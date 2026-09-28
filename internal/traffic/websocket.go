package traffic

import "time"

// WSStats summarises a WebSocket connection on its exchange.
type WSStats struct {
	Messages    int    `json:"messages"`
	Sent        int    `json:"sent"`
	Received    int    `json:"received"`
	Compressed  bool   `json:"compressed,omitempty"`
	CloseCode   int    `json:"closeCode,omitempty"`
	CloseReason string `json:"closeReason,omitempty"`
}

// WSMessage is one WebSocket message: a data message (possibly assembled
// from several frames) or a control frame.
type WSMessage struct {
	Seq        int       `json:"seq"`
	Time       time.Time `json:"time"`
	Dir        string    `json:"dir"`  // send (client to server) or receive
	Type       string    `json:"type"` // text, binary, close, ping, pong
	Size       int64     `json:"size"` // payload bytes after decompression
	Compressed bool      `json:"compressed,omitempty"`
	Truncated  bool      `json:"truncated,omitempty"`
	Note       string    `json:"note,omitempty"`
	CloseCode  int       `json:"closeCode,omitempty"`

	Data []byte `json:"-"`
}

// maxMessages bounds the log kept per connection; older messages are
// dropped first, since the recent ones are what people debug.
const maxMessages = 5000

type messageLog struct {
	items   []WSMessage
	seq     int
	dropped int
}

// AppendMessage adds m to exchange id's log, assigning its sequence number.
// It returns false if the exchange is gone.
func (s *Store) AppendMessage(id uint64, m WSMessage) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return false
	}
	if s.msgs == nil {
		s.msgs = make(map[uint64]*messageLog)
	}
	log := s.msgs[id]
	if log == nil {
		log = &messageLog{}
		s.msgs[id] = log
	}
	log.seq++
	m.Seq = log.seq
	log.items = append(log.items, m)
	if len(log.items) > maxMessages {
		n := len(log.items) - maxMessages
		log.items = append(log.items[:0:0], log.items[n:]...)
		log.dropped += n
	}
	return true
}

// MessageQuery selects messages; zero fields don't filter.
type MessageQuery struct {
	After int    // only seq > After
	Dir   string // send or receive
	Limit int
	Match func(WSMessage) bool
}

// Messages returns matching messages in order, whether more match beyond
// the limit, how many exist in total and how many were dropped.
func (s *Store) Messages(id uint64, q MessageQuery) (items []WSMessage, more bool, total, dropped int, ok bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.byID[id]; !ok {
		return nil, false, 0, 0, false
	}
	log := s.msgs[id]
	if log == nil {
		return []WSMessage{}, false, 0, 0, true
	}
	if q.Limit <= 0 {
		q.Limit = 500
	}
	items = []WSMessage{}
	for _, m := range log.items {
		if m.Seq <= q.After || (q.Dir != "" && m.Dir != q.Dir) || (q.Match != nil && !q.Match(m)) {
			continue
		}
		if len(items) == q.Limit {
			more = true
			break
		}
		items = append(items, m)
	}
	return items, more, len(log.items), log.dropped, true
}
