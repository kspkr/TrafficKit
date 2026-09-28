package proxy

import (
	"io"
	"sync"
)

// capture keeps the first limit bytes written to it and counts the rest.
// Writes never fail, so it can sit in a tee without affecting the stream.
type capture struct {
	mu    sync.Mutex
	limit int64
	buf   []byte
	n     int64
}

func newCapture(limit int64) *capture {
	return &capture{limit: limit}
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n += int64(len(p))
	if room := c.limit - int64(len(c.buf)); room > 0 {
		c.buf = append(c.buf, p[:min(int64(len(p)), room)]...)
	}
	return len(p), nil
}

// snapshot returns a private copy of what has been captured so far.
func (c *capture) snapshot() (data []byte, size int64, truncated bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf...), c.n, c.n > int64(len(c.buf))
}

// teeBody copies everything read from the client's request body into a
// capture on its way upstream.
type teeBody struct {
	rc io.ReadCloser
	c  *capture
}

func (t *teeBody) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		t.c.Write(p[:n])
	}
	return n, err
}

func (t *teeBody) Close() error { return t.rc.Close() }
