package traffic

import "testing"

func seedQuery(t *testing.T) *Store {
	s := NewStore(100)
	add := func(method, host, path string, status int, errCode string) {
		x := Exchange{ID: s.NextID(), State: StateComplete, Request: Request{Method: method, Host: host, Path: path}}
		if status != 0 {
			x.Response = &Response{Status: status}
		}
		if errCode != "" {
			x.State = StateFailed
			x.Error = &Failure{Code: errCode}
		}
		if _, _, ok := s.Put(x); !ok {
			t.Fatal("put failed")
		}
	}
	add("GET", "api.example.com", "/users", 200, "")      // 1
	add("POST", "api.example.com", "/login", 401, "")     // 2
	add("GET", "cdn.example.com", "/app.js", 200, "")     // 3
	add("GET", "down.example.com", "/", 0, "refused")     // 4
	add("DELETE", "api.example.com", "/users/7", 500, "") // 5
	return s
}

func ids(items []Summary) []uint64 {
	var out []uint64
	for _, s := range items {
		out = append(out, s.ID)
	}
	return out
}

func TestQuery(t *testing.T) {
	s := seedQuery(t)
	cases := []struct {
		name string
		q    Query
		want []uint64
	}{
		{"all newest first", Query{}, []uint64{5, 4, 3, 2, 1}},
		{"host", Query{Host: "API."}, []uint64{5, 2, 1}},
		{"method", Query{Method: "get"}, []uint64{4, 3, 1}},
		{"failed", Query{FailedOnly: true}, []uint64{5, 4, 2}},
		{"status range", Query{StatusMin: 400, StatusMax: 499}, []uint64{2}},
		{"filter terms", Query{Filter: "api -login"}, []uint64{5, 1}},
		{"after", Query{After: 3}, []uint64{5, 4}},
		{"before", Query{Before: 3}, []uint64{2, 1}},
	}
	for _, tc := range cases {
		got, _ := s.Query(tc.q)
		if len(got) != len(tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, ids(got), tc.want)
			continue
		}
		for i := range got {
			if got[i].ID != tc.want[i] {
				t.Errorf("%s: got %v, want %v", tc.name, ids(got), tc.want)
				break
			}
		}
	}
	got, more := s.Query(Query{Limit: 2})
	if len(got) != 2 || !more {
		t.Errorf("limit: %v more=%v", ids(got), more)
	}
}

func TestRecent(t *testing.T) {
	s := seedQuery(t)
	r := s.Recent(2)
	if len(r) != 2 || r[0].ID != 5 || r[1].ID != 4 {
		t.Fatalf("recent = %v, %v", r[0].ID, r[1].ID)
	}
}
