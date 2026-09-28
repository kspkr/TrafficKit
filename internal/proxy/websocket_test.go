package proxy

import (
	"bufio"
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/traffickit/traffickit/internal/traffic"
)

// frame builds one WebSocket frame. Client frames are masked.
func frame(fin bool, op byte, payload []byte, masked, rsv1 bool) []byte {
	var b bytes.Buffer
	first := op
	if fin {
		first |= 0x80
	}
	if rsv1 {
		first |= 0x40
	}
	b.WriteByte(first)
	maskBit := byte(0)
	if masked {
		maskBit = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		b.WriteByte(maskBit | byte(n))
	case n <= 0xffff:
		b.WriteByte(maskBit | 126)
		binary.Write(&b, binary.BigEndian, uint16(n))
	default:
		b.WriteByte(maskBit | 127)
		binary.Write(&b, binary.BigEndian, uint64(n))
	}
	if !masked {
		b.Write(payload)
		return b.Bytes()
	}
	key := [4]byte{0x12, 0x34, 0x56, 0x78}
	b.Write(key[:])
	for i, c := range payload {
		b.WriteByte(c ^ key[i%4])
	}
	return b.Bytes()
}

func collect(dir string, limit int64, deflate, noTakeover bool) (*wsDecoder, *[]traffic.WSMessage) {
	var got []traffic.WSMessage
	d := &wsDecoder{dir: dir, limit: limit, deflate: deflate, noTakeover: noTakeover, emit: func(m traffic.WSMessage) {
		got = append(got, m)
	}}
	return d, &got
}

func TestDecodeLengthsAndMasking(t *testing.T) {
	d, got := collect("send", 1<<20, false, false)
	sizes := []int{0, 5, 125, 126, 200, 70000}
	var stream []byte
	for _, n := range sizes {
		stream = append(stream, frame(true, opText, bytes.Repeat([]byte("x"), n), true, false)...)
	}
	// Deliver one byte at a time: frames and headers split everywhere.
	for i := range stream {
		d.Write(stream[i : i+1])
	}
	if len(*got) != len(sizes) {
		t.Fatalf("got %d messages, want %d", len(*got), len(sizes))
	}
	for i, m := range *got {
		if m.Type != "text" || m.Dir != "send" || int(m.Size) != sizes[i] || len(m.Data) != sizes[i] || strings.Trim(string(m.Data), "x") != "" {
			t.Errorf("message %d: %+v (len %d)", i, m.Type, len(m.Data))
		}
	}
}

func TestDecodeFragmentsAndControlFrames(t *testing.T) {
	d, got := collect("receive", 1<<20, false, false)
	var s []byte
	s = append(s, frame(false, opText, []byte("hel"), false, false)...)
	s = append(s, frame(true, opPing, []byte("p"), false, false)...) // control frames may interleave
	s = append(s, frame(false, opContinuation, []byte("lo "), false, false)...)
	s = append(s, frame(true, opContinuation, []byte("world"), false, false)...)
	closePayload := append([]byte{0x03, 0xe8}, "bye"...)
	s = append(s, frame(true, opClose, closePayload, false, false)...)
	d.Write(s)

	want := []struct{ typ, data string }{{"ping", "p"}, {"text", "hello world"}, {"close", "bye"}}
	if len(*got) != len(want) {
		t.Fatalf("got %d messages", len(*got))
	}
	for i, w := range want {
		if m := (*got)[i]; m.Type != w.typ || string(m.Data) != w.data {
			t.Errorf("message %d = %s %q, want %s %q", i, m.Type, m.Data, w.typ, w.data)
		}
	}
	if (*got)[2].CloseCode != 1000 {
		t.Errorf("close code = %d", (*got)[2].CloseCode)
	}
}

// deflateMessages compresses messages the way permessage-deflate does:
// sync-flushed, with the trailing 00 00 ff ff removed.
func deflateMessages(t *testing.T, msgs []string, takeover bool) [][]byte {
	var out [][]byte
	var buf bytes.Buffer
	w, _ := flate.NewWriter(&buf, flate.BestCompression)
	for _, m := range msgs {
		if !takeover {
			buf.Reset()
			w, _ = flate.NewWriter(&buf, flate.BestCompression)
		}
		start := buf.Len()
		w.Write([]byte(m))
		w.Flush()
		chunk := append([]byte(nil), buf.Bytes()[start:]...)
		if !bytes.HasSuffix(chunk, []byte{0, 0, 0xff, 0xff}) {
			t.Fatal("flush did not end in a sync marker")
		}
		out = append(out, chunk[:len(chunk)-4])
	}
	return out
}

func TestDecodeCompressed(t *testing.T) {
	msgs := []string{
		`{"type":"subscribe","channel":"prices","symbols":["BTC","ETH"]}`,
		`{"type":"subscribe","channel":"prices","symbols":["BTC","ETH"]}`, // back-references the first
		`{"type":"update","channel":"prices","symbol":"BTC","price":64000}`,
	}
	for _, takeover := range []bool{true, false} {
		t.Run(fmt.Sprintf("takeover=%v", takeover), func(t *testing.T) {
			d, got := collect("receive", 1<<20, true, !takeover)
			for _, c := range deflateMessages(t, msgs, takeover) {
				d.Write(frame(true, opText, c, false, true))
			}
			if len(*got) != len(msgs) {
				t.Fatalf("got %d messages", len(*got))
			}
			for i, m := range *got {
				if string(m.Data) != msgs[i] || !m.Compressed || m.Note != "" {
					t.Errorf("message %d = %q note=%q", i, m.Data, m.Note)
				}
			}
		})
	}
}

func TestDecodeRespectsLimit(t *testing.T) {
	d, got := collect("receive", 10, false, false)
	d.Write(frame(true, opBinary, bytes.Repeat([]byte{7}, 100), false, false))
	m := (*got)[0]
	if m.Size != 100 || len(m.Data) != 10 || !m.Truncated || m.Type != "binary" {
		t.Fatalf("message = size %d, kept %d, truncated %v", m.Size, len(m.Data), m.Truncated)
	}
}

func TestParseDeflate(t *testing.T) {
	h := http.Header{"Sec-Websocket-Extensions": {"permessage-deflate; client_no_context_takeover; server_max_window_bits=15"}}
	p := parseDeflate(h)
	if !p.enabled || !p.clientNoContextTakeover || p.serverNoContextTakeover {
		t.Fatalf("params = %+v", p)
	}
	if parseDeflate(http.Header{}).enabled {
		t.Fatal("deflate without the header")
	}
}

// TestWebSocketThroughProxy runs a small echo server behind the proxy and
// checks that messages are relayed unchanged and recorded.
func TestWebSocketThroughProxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, brw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: x\r\n\r\n")
		// Read the client's masked frame and echo it back unmasked.
		hdr := make([]byte, 6)
		io.ReadFull(brw, hdr)
		n := int(hdr[1] & 0x7f)
		payload := make([]byte, n)
		io.ReadFull(brw, payload)
		for i := range payload {
			payload[i] ^= hdr[2+i%4]
		}
		conn.Write(frame(true, opText, append([]byte("echo:"), payload...), false, false))
		conn.Write(frame(true, opClose, []byte{0x03, 0xe8}, false, false))
		io.Copy(io.Discard, brw)
	}))
	defer origin.Close()
	h := startProxy(t, 1<<20)

	conn, err := net.Dial("tcp", h.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	host := strings.TrimPrefix(origin.URL, "http://")
	fmt.Fprintf(conn, "GET %s/socket HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n", origin.URL, host)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake: %v %v", resp, err)
	}
	conn.Write(frame(true, opText, []byte("hello"), true, false))
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	echo := make([]byte, 2+len("echo:hello"))
	if _, err := io.ReadFull(br, echo); err != nil || string(echo[2:]) != "echo:hello" {
		t.Fatalf("echo = %q, %v", echo, err)
	}
	io.ReadFull(br, make([]byte, 4)) // close frame
	conn.Close()

	x := h.sink.wait(t)
	if x.WebSocket == nil || x.WebSocket.Messages != 3 || x.WebSocket.Sent != 1 || x.WebSocket.CloseCode != 1000 {
		t.Fatalf("stats = %+v", x.WebSocket)
	}
	h.sink.mu.Lock()
	defer h.sink.mu.Unlock()
	got := h.sink.messages
	if len(got) != 3 || string(got[0].Data) != "hello" || got[0].Dir != "send" || string(got[1].Data) != "echo:hello" || got[2].Type != "close" {
		t.Fatalf("messages = %+v", got)
	}
}

// A burst of messages must end with the exchange showing the full count,
// even though snapshots are throttled.
func TestRecorderTrailingUpdate(t *testing.T) {
	s := newSink()
	p := New(Options{Sink: s})
	x := &traffic.Exchange{ID: 1, WebSocket: &traffic.WSStats{}}
	rec := &wsRecorder{p: p, id: 1, x: x}
	var last traffic.Exchange
	var mu sync.Mutex
	p.sink = recordFunc{s, func(e traffic.Exchange) { mu.Lock(); last = e; mu.Unlock() }}
	for range 10 {
		rec.record(traffic.WSMessage{Dir: "receive", Type: "text"})
	}
	time.Sleep(statsEvery + 200*time.Millisecond)
	rec.close()
	mu.Lock()
	defer mu.Unlock()
	if last.WebSocket == nil || last.WebSocket.Messages != 10 {
		t.Fatalf("last snapshot = %+v", last.WebSocket)
	}
}

type recordFunc struct {
	*sink
	fn func(traffic.Exchange)
}

func (r recordFunc) Record(x traffic.Exchange) { r.fn(x) }
