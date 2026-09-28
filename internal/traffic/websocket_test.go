package traffic

import (
	"strings"
	"testing"
)

func TestMessageLog(t *testing.T) {
	s := NewStore(10)
	id := s.NextID()
	s.Put(Exchange{ID: id, State: StateStreaming})
	if s.AppendMessage(999, WSMessage{}) {
		t.Fatal("appended to a missing exchange")
	}
	for i := range maxMessages + 5 {
		dir := "send"
		if i%2 == 1 {
			dir = "receive"
		}
		s.AppendMessage(id, WSMessage{Dir: dir, Type: "text", Data: []byte("msg")})
	}
	all, more, total, dropped, ok := s.Messages(id, MessageQuery{Limit: 10})
	if !ok || len(all) != 10 || !more || total != maxMessages || dropped != 5 || all[0].Seq != 6 {
		t.Fatalf("first page: len=%d more=%v total=%d dropped=%d first=%d", len(all), more, total, dropped, all[0].Seq)
	}
	recv, _, _, _, _ := s.Messages(id, MessageQuery{Dir: "receive", After: maxMessages, Limit: 100})
	for _, m := range recv {
		if m.Dir != "receive" || m.Seq <= maxMessages {
			t.Fatalf("filter leaked %+v", m)
		}
	}
	none, _, _, _, _ := s.Messages(id, MessageQuery{Match: func(m WSMessage) bool { return strings.Contains(string(m.Data), "zzz") }})
	if len(none) != 0 {
		t.Fatal("match filter ignored")
	}
	s.Delete(id)
	if _, _, _, _, ok := s.Messages(id, MessageQuery{}); ok {
		t.Fatal("messages outlived their exchange")
	}
}
