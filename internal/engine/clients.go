package engine

import (
	"sort"
	"sync"
	"time"
)

// Client is something other than the UI using the engine API, such as the
// MCP server on behalf of an AI assistant. The UI shows these so it's never
// a secret that an assistant can read the traffic.
type Client struct {
	Name     string    `json:"name"`
	LastSeen time.Time `json:"lastSeen"`
}

type clientTracker struct {
	mu        sync.Mutex
	seen      map[string]time.Time
	published time.Time
}

// publishEvery limits "clients" events; a busy assistant makes many calls.
const publishEvery = 20 * time.Second

func (e *Engine) NoteClient(name string) {
	now := time.Now()
	t := &e.clients
	t.mu.Lock()
	if t.seen == nil {
		t.seen = make(map[string]time.Time)
	}
	_, known := t.seen[name]
	t.seen[name] = now
	publish := !known || now.Sub(t.published) > publishEvery
	if publish {
		t.published = now
	}
	t.mu.Unlock()
	if publish {
		e.hub.Publish("clients", e.Clients())
	}
}

func (e *Engine) Clients() []Client {
	t := &e.clients
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Client, 0, len(t.seen))
	for name, at := range t.seen {
		out = append(out, Client{Name: name, LastSeen: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}
