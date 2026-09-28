package proxy

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/traffickit/traffickit/internal/traffic"
)

// timer collects httptrace callbacks. The transport calls them from its own
// goroutines, hence the lock.
type timer struct {
	mu sync.Mutex

	start               time.Time
	dnsStart, dnsDone   time.Time
	connStart, connDone time.Time
	tlsStart, tlsDone   time.Time
	gotConn, wroteReq   time.Time
	firstByte           time.Time
}

func newTimer(start time.Time) *timer { return &timer{start: start} }

func (t *timer) mark(dst *time.Time) {
	t.mu.Lock()
	// With happy-eyeballs dialing ConnectStart can fire more than once; keep
	// the first start and the last finish.
	if dst.IsZero() || dst == &t.connDone {
		*dst = time.Now()
	}
	t.mu.Unlock()
}

func (t *timer) attach(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart:             func(httptrace.DNSStartInfo) { t.mark(&t.dnsStart) },
		DNSDone:              func(httptrace.DNSDoneInfo) { t.mark(&t.dnsDone) },
		ConnectStart:         func(string, string) { t.mark(&t.connStart) },
		ConnectDone:          func(string, string, error) { t.mark(&t.connDone) },
		TLSHandshakeStart:    func() { t.mark(&t.tlsStart) },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { t.mark(&t.tlsDone) },
		GotConn:              func(httptrace.GotConnInfo) { t.mark(&t.gotConn) },
		WroteRequest:         func(httptrace.WroteRequestInfo) { t.mark(&t.wroteReq) },
		GotFirstResponseByte: func() { t.mark(&t.firstByte) },
	})
}

func span(a, b time.Time) float64 {
	if a.IsZero() || b.IsZero() || b.Before(a) {
		return -1
	}
	return float64(b.Sub(a).Microseconds()) / 1000
}

// result turns the collected marks into Timings, with end as the moment the
// last response byte was relayed (or the exchange failed).
func (t *timer) result(end time.Time) traffic.Timings {
	t.mu.Lock()
	defer t.mu.Unlock()
	sendFrom := t.gotConn
	if sendFrom.IsZero() {
		sendFrom = t.start
	}
	return traffic.Timings{
		DNS:     span(t.dnsStart, t.dnsDone),
		Connect: span(t.connStart, t.connDone),
		TLS:     span(t.tlsStart, t.tlsDone),
		Send:    span(sendFrom, t.wroteReq),
		Wait:    span(t.wroteReq, t.firstByte),
		Receive: span(t.firstByte, end),
		Total:   span(t.start, end),
	}
}
