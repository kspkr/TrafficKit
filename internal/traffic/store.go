package traffic

import (
	"sort"
	"sync"
)

// Store keeps exchanges in memory, ordered by ID, and evicts the oldest once
// it holds more than its limit. Safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	limit  int
	nextID uint64
	byID   map[uint64]*Exchange
	order  []uint64 // ascending

	// The proxy keeps writing an exchange until it finishes. If the user
	// deletes it (or it's evicted, or everything is cleared) before then,
	// those later writes must not bring it back. IDs at or below floor are
	// gone unless present; dropped holds in-flight exchanges deleted
	// individually, and is emptied as they finish.
	floor   uint64
	dropped map[uint64]struct{}

	msgs map[uint64]*messageLog // WebSocket messages by exchange
}

func NewStore(limit int) *Store {
	if limit <= 0 {
		limit = 1
	}
	return &Store{
		limit:   limit,
		byID:    make(map[uint64]*Exchange),
		dropped: make(map[uint64]struct{}),
	}
}

func (s *Store) NextID() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return s.nextID
}

// Put inserts or replaces x, bumping its revision. It returns the stored
// summary, the IDs evicted to make room, and false if x had been deleted.
func (s *Store) Put(x Exchange) (sum Summary, evicted []uint64, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cur, exists := s.byID[x.ID]; exists {
		x.Rev = cur.Rev + 1
		*cur = x
		return cur.Summary(), nil, true
	}
	if _, gone := s.dropped[x.ID]; gone {
		if finished(x.State) {
			delete(s.dropped, x.ID)
		}
		return Summary{}, nil, false
	}
	if x.ID <= s.floor {
		return Summary{}, nil, false
	}

	x.Rev = 1
	s.byID[x.ID] = &x
	// IDs are allocated in order but Put calls race, so insert in place.
	i := sort.Search(len(s.order), func(i int) bool { return s.order[i] >= x.ID })
	s.order = append(s.order, 0)
	copy(s.order[i+1:], s.order[i:])
	s.order[i] = x.ID

	for len(s.order) > s.limit {
		id := s.order[0]
		s.order = s.order[1:]
		delete(s.byID, id)
		delete(s.msgs, id)
		s.floor = max(s.floor, id)
		evicted = append(evicted, id)
	}
	return x.Summary(), evicted, true
}

func finished(st State) bool {
	return st == StateComplete || st == StateFailed
}

func (s *Store) Get(id uint64) (Exchange, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	x, ok := s.byID[id]
	if !ok {
		return Exchange{}, false
	}
	return x.Clone(), true
}

// List returns up to limit summaries with ID greater than after, ascending,
// and whether more remain.
func (s *Store) List(after uint64, limit int) ([]Summary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := sort.Search(len(s.order), func(i int) bool { return s.order[i] > after })
	end := min(i+limit, len(s.order))
	out := make([]Summary, 0, end-i)
	for _, id := range s.order[i:end] {
		out = append(out, s.byID[id].Summary())
	}
	return out, end < len(s.order)
}

func (s *Store) Delete(id uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, ok := s.byID[id]
	if !ok {
		return false
	}
	if !finished(x.State) {
		s.dropped[id] = struct{}{}
	}
	delete(s.byID, id)
	delete(s.msgs, id)
	i := sort.Search(len(s.order), func(i int) bool { return s.order[i] >= id })
	s.order = append(s.order[:i], s.order[i+1:]...)
	return true
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[uint64]*Exchange)
	s.msgs = nil
	s.order = nil
	s.floor = s.nextID
	clear(s.dropped)
}

func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.order)
}

// LastID is the most recently allocated ID, whether or not it's still stored.
func (s *Store) LastID() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.nextID
}
