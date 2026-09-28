package mcpserver

import (
	"strings"
	"testing"

	"github.com/traffickit/traffickit/internal/traffic"
)

const jwt = "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"

func TestRedactHeaders(t *testing.T) {
	r := newRedactor(true)
	cases := map[traffic.Header]string{
		{Name: "Authorization", Value: "Bearer abc123secretvalue"}: "Bearer [redacted, 17 chars]",
		{Name: "Cookie", Value: "sid=abc; theme=dark"}:             "sid=[redacted]; theme=[redacted]",
		{Name: "Set-Cookie", Value: "sid=abc; Path=/; HttpOnly"}:   "sid=[redacted]; Path=/; HttpOnly",
		{Name: "X-Api-Key", Value: "k-123"}:                        "[redacted, 5 chars]",
		{Name: "X-Custom", Value: "token " + jwt}:                  "token [redacted JWT]",
		{Name: "Content-Type", Value: "application/json"}:          "application/json",
	}
	for in, want := range cases {
		if got := r.Header(in).Value; got != want {
			t.Errorf("%s: got %q, want %q", in.Name, got, want)
		}
	}
	if len(r.Hidden()) == 0 {
		t.Error("nothing noted as hidden")
	}
}

func TestRedactURL(t *testing.T) {
	r := newRedactor(true)
	got := r.URL("https://api.test/cb?code=xyz&state=ok&access_token=abc&q=hello%20world")
	want := "https://api.test/cb?code=[redacted]&state=ok&access_token=[redacted]&q=hello%20world"
	if got != want {
		t.Errorf("got %q", got)
	}
	if r.URL("https://api.test/plain") != "https://api.test/plain" {
		t.Error("URL without query changed")
	}
}

func TestRedactJSONBody(t *testing.T) {
	r := newRedactor(true)
	in := `{"user":{"email":"a@b.c","password":"hunter2"},"items":[{"apiKey":"k1","n":1}],"note":"` + jwt + `"}`
	out := r.Body("application/json", in)
	for _, secret := range []string{"hunter2", `"k1"`, jwt} {
		if strings.Contains(out, secret) {
			t.Errorf("%s leaked: %s", secret, out)
		}
	}
	if !strings.Contains(out, `"email":"a@b.c"`) || !strings.Contains(out, `"n":1`) {
		t.Errorf("non-secret data lost: %s", out)
	}
}

func TestUnchangedJSONKeepsFormatting(t *testing.T) {
	r := newRedactor(true)
	in := "{\n  \"b\": 1,\n  \"a\": 2\n}"
	if out := r.Body("application/json", in); out != in {
		t.Errorf("reformatted: %q", out)
	}
}

func TestRedactFormBody(t *testing.T) {
	r := newRedactor(true)
	out := r.Body("application/x-www-form-urlencoded", "username=ada&password=hunter2")
	if out != "username=ada&password=[redacted]" {
		t.Errorf("got %q", out)
	}
}

func TestRedactionOff(t *testing.T) {
	r := newRedactor(false)
	h := traffic.Header{Name: "Authorization", Value: "Bearer secret"}
	if r.Header(h) != h || r.URL("/x?token=1") != "/x?token=1" || r.Body("application/json", `{"password":"p"}`) != `{"password":"p"}` {
		t.Error("redaction applied while disabled")
	}
}

func TestRedactJSONInsideString(t *testing.T) {
	r := newRedactor(true)
	out := r.Body("application/json", `{"query":"mutation Login","variables":"{\"email\":\"a@b.c\",\"password\":\"hunter2\"}"}`)
	if strings.Contains(out, "hunter2") || !strings.Contains(out, "a@b.c") {
		t.Fatalf("got %s", out)
	}
}
