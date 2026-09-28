package traffic

import (
	"net/http"
	"testing"
)

func put(t *testing.T, s *Store, id uint64, st State) Summary {
	t.Helper()
	sum, _, ok := s.Put(Exchange{ID: id, State: st, Request: Request{Method: "GET"}})
	if !ok {
		t.Fatalf("Put(%d) rejected", id)
	}
	return sum
}

func TestStorePutBumpsRevision(t *testing.T) {
	s := NewStore(10)
	id := s.NextID()
	if got := put(t, s, id, StatePending).Rev; got != 1 {
		t.Fatalf("first rev = %d, want 1", got)
	}
	if got := put(t, s, id, StateComplete).Rev; got != 2 {
		t.Fatalf("second rev = %d, want 2", got)
	}
	if s.Len() != 1 {
		t.Fatalf("Len = %d, want 1", s.Len())
	}
}

func TestStoreEvictsOldest(t *testing.T) {
	s := NewStore(3)
	var evicted []uint64
	for range 5 {
		_, ev, _ := s.Put(Exchange{ID: s.NextID(), State: StateComplete})
		evicted = append(evicted, ev...)
	}
	if len(evicted) != 2 || evicted[0] != 1 || evicted[1] != 2 {
		t.Fatalf("evicted = %v, want [1 2]", evicted)
	}
	items, more := s.List(0, 10)
	if more || len(items) != 3 || items[0].ID != 3 {
		t.Fatalf("List = %+v more=%v", items, more)
	}
	// A late write for an evicted exchange must not bring it back.
	if _, _, ok := s.Put(Exchange{ID: 1, State: StateComplete}); ok {
		t.Fatal("evicted exchange was re-inserted")
	}
}

func TestStoreOutOfOrderPut(t *testing.T) {
	s := NewStore(10)
	a, b, c := s.NextID(), s.NextID(), s.NextID()
	put(t, s, c, StatePending)
	put(t, s, a, StatePending)
	put(t, s, b, StatePending)
	items, _ := s.List(0, 10)
	for i, want := range []uint64{a, b, c} {
		if items[i].ID != want {
			t.Fatalf("order = %v", items)
		}
	}
}

func TestStoreListPaging(t *testing.T) {
	s := NewStore(100)
	for range 7 {
		put(t, s, s.NextID(), StateComplete)
	}
	page, more := s.List(0, 3)
	if len(page) != 3 || !more {
		t.Fatalf("page1 len=%d more=%v", len(page), more)
	}
	page, more = s.List(page[2].ID, 3)
	if len(page) != 3 || !more || page[0].ID != 4 {
		t.Fatalf("page2 = %+v more=%v", page, more)
	}
	page, more = s.List(page[2].ID, 3)
	if len(page) != 1 || more {
		t.Fatalf("page3 len=%d more=%v", len(page), more)
	}
}

func TestStoreDeleteInFlight(t *testing.T) {
	s := NewStore(10)
	id := s.NextID()
	put(t, s, id, StatePending)
	if !s.Delete(id) {
		t.Fatal("Delete returned false")
	}
	if _, _, ok := s.Put(Exchange{ID: id, State: StateStreaming}); ok {
		t.Fatal("deleted exchange came back on update")
	}
	if _, _, ok := s.Put(Exchange{ID: id, State: StateComplete}); ok {
		t.Fatal("deleted exchange came back on completion")
	}
	if len(s.dropped) != 0 {
		t.Fatalf("dropped set not cleaned up: %v", s.dropped)
	}
}

func TestStoreClear(t *testing.T) {
	s := NewStore(10)
	inflight := s.NextID()
	put(t, s, inflight, StatePending)
	put(t, s, s.NextID(), StateComplete)
	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("Len after Clear = %d", s.Len())
	}
	if _, _, ok := s.Put(Exchange{ID: inflight, State: StateComplete}); ok {
		t.Fatal("cleared exchange came back")
	}
	put(t, s, s.NextID(), StatePending)
}

func TestSummary(t *testing.T) {
	x := Exchange{
		ID:      1,
		State:   StateComplete,
		Request: Request{Method: "POST", Host: "api.test", Path: "/v1?a=1"},
		Response: &Response{
			Status:  201,
			Headers: HeaderList(http.Header{"Content-Type": {"application/json; charset=utf-8"}}),
		},
		Timings: Timings{Total: 12.5},
	}
	x.Response.Body.SetData([]byte(`{}`), 2, false)
	s := x.Summary()
	if s.ContentType != "application/json" || s.Status != 201 || s.RespSize != 2 || s.Duration != 12.5 {
		t.Fatalf("summary = %+v", s)
	}
	x.State = StateStreaming
	if d := x.Summary().Duration; d != -1 {
		t.Fatalf("in-flight duration = %v, want -1", d)
	}
}

func TestHeaderListSorted(t *testing.T) {
	got := HeaderList(http.Header{"X-B": {"2"}, "Accept": {"a", "b"}, "X-A": {"1"}})
	want := []Header{{"Accept", "a"}, {"Accept", "b"}, {"X-A", "1"}, {"X-B", "2"}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
