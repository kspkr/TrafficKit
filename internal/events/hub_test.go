package events

import (
	"strings"
	"testing"
)

func TestPublishDelivers(t *testing.T) {
	h := NewHub()
	s := h.Subscribe(4)
	h.Publish("ping", nil)
	got := string(<-s.C)
	if got != `{"type":"ping","data":{}}`+"\n" {
		t.Fatalf("line = %q", got)
	}
}

func TestSlowSubscriberIsMarkedLagged(t *testing.T) {
	h := NewHub()
	slow := h.Subscribe(1)
	fast := h.Subscribe(8)
	for i := range 5 {
		h.Publish("n", i)
	}
	if !slow.Lagged() {
		t.Fatal("slow subscriber not marked lagged")
	}
	if slow.Lagged() {
		t.Fatal("Lagged should reset after being read")
	}
	if fast.Lagged() || len(fast.C) != 5 {
		t.Fatalf("fast subscriber lost events: len=%d", len(fast.C))
	}
}

func TestUnsubscribe(t *testing.T) {
	h := NewHub()
	s := h.Subscribe(1)
	h.Unsubscribe(s)
	h.Publish("x", nil)
	if len(s.C) != 0 {
		t.Fatal("event delivered after Unsubscribe")
	}
}

func TestEncodeIsOneLine(t *testing.T) {
	line := string(Encode("exchange", map[string]string{"path": "/a\nb"}))
	if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") {
		t.Fatalf("not a single line: %q", line)
	}
}
