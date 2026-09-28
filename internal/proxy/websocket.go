package proxy

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/traffickit/traffickit/internal/traffic"
)

// WebSocket frames are decoded from a copy of the relayed bytes (RFC 6455).
// The relay itself never waits on or changes anything here.

const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA

	// Compressed payloads are kept whole up to this size so they can be
	// inflated; the decompressed result is then capped at the capture limit.
	maxCompressedPayload = 8 << 20
	maxInflated          = 16 << 20
	deflateWindow        = 32 << 10
)

var opNames = map[byte]string{opText: "text", opBinary: "binary", opClose: "close", opPing: "ping", opPong: "pong"}

// deflateParams is what permessage-deflate negotiated (RFC 7692).
type deflateParams struct {
	enabled                 bool
	clientNoContextTakeover bool
	serverNoContextTakeover bool
}

func parseDeflate(h http.Header) deflateParams {
	for _, ext := range h.Values("Sec-WebSocket-Extensions") {
		for offer := range strings.SplitSeq(ext, ",") {
			parts := strings.Split(offer, ";")
			if strings.TrimSpace(parts[0]) != "permessage-deflate" {
				continue
			}
			p := deflateParams{enabled: true}
			for _, param := range parts[1:] {
				switch strings.TrimSpace(strings.SplitN(param, "=", 2)[0]) {
				case "client_no_context_takeover":
					p.clientNoContextTakeover = true
				case "server_no_context_takeover":
					p.serverNoContextTakeover = true
				}
			}
			return p
		}
	}
	return deflateParams{}
}

// wsDecoder turns one direction's byte stream into messages.
type wsDecoder struct {
	dir        string // send or receive
	limit      int64  // bytes of each payload to keep
	deflate    bool
	noTakeover bool
	emit       func(traffic.WSMessage)

	hdr       []byte // header bytes collected so far
	remaining uint64 // payload bytes left in the current frame
	masked    bool
	mask      [4]byte
	maskPos   int
	opcode    byte
	fin       bool
	rsv1      bool
	control   []byte // payload of the current control frame

	// The data message being assembled, possibly across several frames.
	msgOp         byte
	msgActive     bool
	msgCompressed bool
	msgData       []byte
	msgSize       int64
	msgTruncated  bool

	window   []byte // last 32 KB of decompressed output, for context takeover
	desynced bool   // a compressed message was cut short; later ones can't be inflated
	broken   bool   // the stream stopped looking like WebSocket frames
}

// Write implements io.Writer so the decoder can sit behind an io.TeeReader.
// It never fails: problems are reported as notes, not as relay errors.
func (d *wsDecoder) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 && !d.broken {
		if d.remaining == 0 {
			p = d.readHeader(p)
			continue
		}
		take := uint64(len(p))
		if take > d.remaining {
			take = d.remaining
		}
		d.payload(p[:take])
		p = p[take:]
		d.remaining -= take
		if d.remaining == 0 {
			d.endFrame()
		}
	}
	return n, nil
}

// readHeader consumes header bytes; when a full header is present it sets
// up the frame and, for empty payloads, completes it immediately.
func (d *wsDecoder) readHeader(p []byte) []byte {
	need := 2
	if len(d.hdr) >= 2 {
		need = headerLen(d.hdr)
	}
	for len(d.hdr) < need && len(p) > 0 {
		d.hdr = append(d.hdr, p[0])
		p = p[1:]
		if len(d.hdr) == 2 {
			need = headerLen(d.hdr)
		}
	}
	if len(d.hdr) < need {
		return p
	}

	h := d.hdr
	d.fin = h[0]&0x80 != 0
	d.rsv1 = h[0]&0x40 != 0
	d.opcode = h[0] & 0x0f
	d.masked = h[1]&0x80 != 0
	length := uint64(h[1] & 0x7f)
	off := 2
	switch length {
	case 126:
		length = uint64(binary.BigEndian.Uint16(h[2:4]))
		off = 4
	case 127:
		length = binary.BigEndian.Uint64(h[2:10])
		off = 10
	}
	if d.masked {
		copy(d.mask[:], h[off:off+4])
	}
	d.maskPos = 0
	d.hdr = d.hdr[:0]

	if _, known := opNames[d.opcode]; !known && d.opcode != opContinuation {
		d.broken = true
		d.emit(traffic.WSMessage{Dir: d.dir, Type: "binary", Note: "Unrecognised frame; stopped decoding this direction."})
		return nil
	}
	d.control = d.control[:0]
	if d.opcode < opClose {
		d.startData()
	}
	d.remaining = length
	if length == 0 {
		d.endFrame()
	}
	return p
}

func headerLen(h []byte) int {
	n := 2
	switch h[1] & 0x7f {
	case 126:
		n += 2
	case 127:
		n += 8
	}
	if h[1]&0x80 != 0 {
		n += 4
	}
	return n
}

func (d *wsDecoder) startData() {
	if d.opcode == opContinuation && d.msgActive {
		return
	}
	d.msgOp = d.opcode
	if d.opcode == opContinuation {
		d.msgOp = opBinary // continuation without a start; best effort
	}
	d.msgActive = true
	d.msgCompressed = d.deflate && d.rsv1
	d.msgData = d.msgData[:0]
	d.msgSize = 0
	d.msgTruncated = false
}

func (d *wsDecoder) payload(p []byte) {
	buf := p
	if d.masked {
		buf = make([]byte, len(p))
		for i, b := range p {
			buf[i] = b ^ d.mask[(d.maskPos+i)&3]
		}
		d.maskPos += len(p)
	}
	if d.opcode >= opClose {
		if len(d.control) < 125 {
			d.control = append(d.control, buf[:min(len(buf), 125-len(d.control))]...)
		}
		return
	}
	d.msgSize += int64(len(buf))
	keep := d.limit
	if d.msgCompressed {
		keep = maxCompressedPayload
	}
	if room := keep - int64(len(d.msgData)); room > 0 {
		d.msgData = append(d.msgData, buf[:min(int64(len(buf)), room)]...)
	}
	if int64(len(d.msgData)) < d.msgSize {
		d.msgTruncated = true
	}
}

func (d *wsDecoder) endFrame() {
	now := time.Now()
	if d.opcode >= opClose {
		m := traffic.WSMessage{Time: now, Dir: d.dir, Type: opNames[d.opcode], Size: int64(len(d.control))}
		data := d.control
		if d.opcode == opClose && len(data) >= 2 {
			m.CloseCode = int(binary.BigEndian.Uint16(data[:2]))
			data = data[2:]
		}
		m.Data = append([]byte(nil), data...)
		d.emit(m)
		return
	}
	if !d.fin {
		return // more fragments to come
	}
	m := traffic.WSMessage{Time: now, Dir: d.dir, Type: opNames[d.msgOp], Size: d.msgSize, Compressed: d.msgCompressed}
	data := d.msgData
	if d.msgCompressed {
		out, note := d.inflate(data, d.msgTruncated)
		data, m.Note = out, note
		m.Size = int64(len(out))
	}
	if int64(len(data)) > d.limit {
		data = data[:d.limit]
		m.Truncated = true
	}
	m.Truncated = m.Truncated || (d.msgTruncated && !d.msgCompressed)
	m.Data = append([]byte(nil), data...)
	d.msgActive = false
	d.emit(m)
}

// inflate undoes permessage-deflate. With context takeover (the default)
// each message can refer back to earlier ones, which is what the dictionary
// carries.
func (d *wsDecoder) inflate(data []byte, truncated bool) ([]byte, string) {
	if d.desynced {
		return nil, "Compressed, and an earlier message was too large to follow the compression state. Payload not shown."
	}
	dict := d.window
	if d.noTakeover {
		dict = nil
	}
	src := io.MultiReader(bytes.NewReader(data), bytes.NewReader([]byte{0, 0, 0xff, 0xff}))
	r := flate.NewReaderDict(src, dict)
	out, err := io.ReadAll(io.LimitReader(r, maxInflated))
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		d.desynced = !d.noTakeover
		return out, "Could not decompress: " + err.Error()
	}
	if truncated {
		d.desynced = !d.noTakeover
		return out, "Compressed message was larger than TrafficKit keeps; showing what could be decompressed."
	}
	if !d.noTakeover {
		w := append(d.window, out...)
		if len(w) > deflateWindow {
			w = w[len(w)-deflateWindow:]
		}
		d.window = append([]byte(nil), w...)
	}
	return out, ""
}

// wsRecorder collects both directions' messages for one connection and
// keeps the exchange's counters up to date.
type wsRecorder struct {
	p  *Proxy
	id uint64

	mu      sync.Mutex
	x       *traffic.Exchange
	lastPut time.Time
	pending *time.Timer // trailing update after a burst
	closed  bool
}

const statsEvery = 300 * time.Millisecond

func (r *wsRecorder) record(m traffic.WSMessage) {
	r.p.sink.Message(r.id, m)
	r.mu.Lock()
	defer r.mu.Unlock()
	ws := r.x.WebSocket
	ws.Messages++
	if m.Dir == "send" {
		ws.Sent++
	} else {
		ws.Received++
	}
	if m.Type == "close" && ws.CloseCode == 0 {
		ws.CloseCode = m.CloseCode
		if utf8.Valid(m.Data) {
			ws.CloseReason = string(m.Data)
		}
	}
	// Keep the list's message count moving without a snapshot per message,
	// and make sure the last burst is always reflected.
	now := time.Now()
	if wait := statsEvery - now.Sub(r.lastPut); wait > 0 {
		if r.pending == nil {
			r.pending = time.AfterFunc(wait, r.flush)
		}
		return
	}
	r.lastPut = now
	r.p.sink.Record(r.x.Clone())
}

func (r *wsRecorder) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = nil
	if r.closed {
		return
	}
	r.lastPut = time.Now()
	r.p.sink.Record(r.x.Clone())
}

// close stops background updates; after it returns the exchange belongs to
// the caller again.
func (r *wsRecorder) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.pending != nil {
		r.pending.Stop()
	}
}

func (r *wsRecorder) decoders(params deflateParams, limit int64) (send, receive *wsDecoder) {
	send = &wsDecoder{dir: "send", limit: limit, deflate: params.enabled, noTakeover: params.clientNoContextTakeover, emit: r.record}
	receive = &wsDecoder{dir: "receive", limit: limit, deflate: params.enabled, noTakeover: params.serverNoContextTakeover, emit: r.record}
	return send, receive
}
