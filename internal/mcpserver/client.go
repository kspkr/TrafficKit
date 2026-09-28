package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/traffickit/traffickit/internal/discovery"
)

// client talks to the engine's control API. In the normal case it finds the
// desktop app's engine through the discovery file on every call, so it keeps
// working if TrafficKit is started, restarted or closed while the assistant
// is connected.
type client struct {
	dataDir string
	fixed   *discovery.Info // standalone mode: our own in-process engine
	http    *http.Client
	name    atomic.Value // string: assistant name, sent as X-TK-Client
}

func newClient(dataDir string, fixed *discovery.Info) *client {
	c := &client{dataDir: dataDir, fixed: fixed, http: &http.Client{Timeout: 30 * time.Second}}
	c.name.Store("AI assistant")
	return c
}

var errNotRunning = errors.New("TrafficKit isn't running. Ask the user to open the TrafficKit app, then try again")

func (c *client) endpoint() (discovery.Info, error) {
	if c.fixed != nil {
		return *c.fixed, nil
	}
	info, err := discovery.Read(c.dataDir)
	if errors.Is(err, discovery.ErrNotRunning) {
		return info, errNotRunning
	}
	return info, err
}

type apiError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
}

func (c *client) send(ctx context.Context, method, path string, body any) (*http.Response, error) {
	info, err := c.endpoint()
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, info.API+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+info.Token)
	req.Header.Set("X-TK-Client", c.name.Load().(string))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// The discovery file outlived the engine (e.g. after a crash).
		return nil, errNotRunning
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var e apiError
		json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&e)
		if e.Error.Message == "" {
			return nil, fmt.Errorf("TrafficKit answered %s", resp.Status)
		}
		msg := e.Error.Message
		if e.Error.Hint != "" {
			msg += " " + e.Error.Hint
		}
		return nil, errors.New(msg)
	}
	return resp, nil
}

func (c *client) json(ctx context.Context, method, path string, body, out any) error {
	resp, err := c.send(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type bodyData struct {
	bytes       []byte
	truncated   bool
	decoded     string
	decodeError string
}

func (c *client) body(ctx context.Context, id uint64, side string) (bodyData, error) {
	resp, err := c.send(ctx, "GET", fmt.Sprintf("/v1/exchanges/%d/body/%s?decode=1", id, url.PathEscape(side)), nil)
	if err != nil {
		return bodyData{}, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	return bodyData{
		bytes:       b,
		truncated:   resp.Header.Get("X-TK-Truncated") == "1",
		decoded:     resp.Header.Get("X-TK-Decoded"),
		decodeError: resp.Header.Get("X-TK-Decode-Error"),
	}, err
}
