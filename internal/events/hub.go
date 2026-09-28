// Package events fans engine events out to API subscribers.
//
// Publishing never blocks. A subscriber that falls behind loses events and is
// told so through Lagged, after which it should refetch whatever it mirrors.
package events

import (
	"encoding/json"
	"sync"
	"sync/atomic"
)

type Sub struct {
	C      chan []byte
	lagged atomic.Bool
}

// Lagged reports, and resets, whether events were dropped for s.
func (s *Sub) Lagged() bool { return s.lagged.Swap(false) }

type Hub struct {
	mu   sync.RWMutex
	subs map[*Sub]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[*Sub]struct{})}
}

func (h *Hub) Subscribe(buffer int) *Sub {
	s := &Sub{C: make(chan []byte, buffer)}
	h.mu.Lock()
	h.subs[s] = struct{}{}
	h.mu.Unlock()
	return s
}

func (h *Hub) Unsubscribe(s *Sub) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

type envelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// Encode renders an event as one NDJSON line.
func Encode(typ string, data any) []byte {
	if data == nil {
		data = struct{}{}
	}
	b, err := json.Marshal(envelope{typ, data})
	if err != nil {
		// Only our own types are published, so this is a programming error.
		panic("events: " + err.Error())
	}
	return append(b, '\n')
}

func (h *Hub) Publish(typ string, data any) {
	line := Encode(typ, data)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for s := range h.subs {
		select {
		case s.C <- line:
		default:
			s.lagged.Store(true)
		}
	}
}
