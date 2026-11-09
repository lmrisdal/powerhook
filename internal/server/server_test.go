package server

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"powerhook/internal/config"
)

const token = "test-token-0123456789"

func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

type fakePower struct{ calls []string }

func (f *fakePower) Suspend() error  { f.calls = append(f.calls, "suspend"); return nil }
func (f *fakePower) Poweroff() error { f.calls = append(f.calls, "poweroff"); return nil }
func (f *fakePower) Restart() error  { f.calls = append(f.calls, "restart"); return nil }

func newTestServer(t *testing.T, allowed ...string) (*Server, *fakePower, *[]func()) {
	t.Helper()
	cfg := &config.Config{Token: token, AllowedIPs: allowed}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	fp := &fakePower{}
	s := New(cfg, fp, "test")
	var pending []func()
	s.after = func(_ time.Duration, fn func()) { pending = append(pending, fn) }
	return s, fp, &pending
}

func do(h http.Handler, method, path, remote, auth string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = remote
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestGuard(t *testing.T) {
	s, _, _ := newTestServer(t, "192.168.1.10", "10.0.0.0/8")
	h := s.Handler()
	good := "Bearer " + token

	tests := []struct {
		name, method, path, remote, auth string
		want                             int
	}{
		{"allowed ip + token", "GET", "/api/status", "192.168.1.10:5555", good, 200},
		{"allowed cidr", "GET", "/api/status", "10.20.30.40:5555", good, 200},
		{"ipv4-mapped ipv6", "GET", "/api/status", "[::ffff:192.168.1.10]:5555", good, 200},
		{"lowercase scheme", "GET", "/api/status", "192.168.1.10:5555", "bearer " + token, 200},
		{"wrong ip", "GET", "/api/status", "192.168.1.11:5555", good, 403},
		{"wrong ip checked before token", "GET", "/api/status", "192.168.1.11:5555", "", 403},
		{"missing token", "GET", "/api/status", "192.168.1.10:5555", "", 401},
		{"wrong token", "GET", "/api/status", "192.168.1.10:5555", "Bearer nope", 401},
		{"token prefix", "GET", "/api/status", "192.168.1.10:5555", "Bearer " + token[:10], 401},
		{"basic scheme", "GET", "/api/status", "192.168.1.10:5555", "Basic " + token, 401},
		{"GET on action", "GET", "/api/poweroff", "192.168.1.10:5555", good, 405},
		{"unknown path", "POST", "/api/explode", "192.168.1.10:5555", good, 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := do(h, tt.method, tt.path, tt.remote, tt.auth).Code; got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestNoToken(t *testing.T) {
	cfg := &config.Config{AllowedIPs: []string{"192.168.1.10"}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	h := New(cfg, &fakePower{}, "test").Handler()
	if c := do(h, "GET", "/api/status", "192.168.1.10:1", "").Code; c != 200 {
		t.Errorf("allowed ip without token: got %d, want 200", c)
	}
	if c := do(h, "GET", "/api/status", "192.168.1.11:1", "").Code; c != 403 {
		t.Errorf("other ip without token: got %d, want 403", c)
	}
}

func TestEmptyAllowlist(t *testing.T) {
	cfg := &config.Config{}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	h := New(cfg, &fakePower{}, "test").Handler()
	for _, addr := range []string{"192.168.1.10:1", "203.0.113.7:1", "[2001:db8::1]:1"} {
		if c := do(h, "GET", "/api/status", addr, "").Code; c != 200 {
			t.Errorf("%s with empty allowlist: got %d, want 200", addr, c)
		}
	}
}

func TestActions(t *testing.T) {
	s, fp, pending := newTestServer(t, "192.168.1.10")
	h := s.Handler()
	for _, action := range []string{"suspend", "poweroff", "restart"} {
		w := do(h, "POST", "/api/"+action, "192.168.1.10:1", "Bearer "+token)
		if w.Code != http.StatusAccepted {
			t.Fatalf("%s: got %d", action, w.Code)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body["action"] != action {
			t.Fatalf("%s: bad body %s", action, w.Body)
		}
		if n := len(fp.calls); n > 0 && fp.calls[n-1] == action {
			t.Fatalf("%s ran before the delay", action)
		}
		// A second request while one is pending is refused.
		if c := do(h, "POST", "/api/suspend", "192.168.1.10:1", "Bearer "+token).Code; c != http.StatusConflict {
			t.Fatalf("expected 409 while pending, got %d", c)
		}
		(*pending)[len(*pending)-1]()
	}
	if got := strings.Join(fp.calls, ","); got != "suspend,poweroff,restart" {
		t.Fatalf("calls = %s", got)
	}
}

func TestStatus(t *testing.T) {
	s, _, _ := newTestServer(t, "127.0.0.1")
	w := do(s.Handler(), "GET", "/api/status", "127.0.0.1:1", "Bearer "+token)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"hostname", "os", "os_id", "os_name", "uptime_seconds", "version"} {
		if _, ok := body[k]; !ok {
			t.Errorf("status missing %q: %s", k, w.Body)
		}
	}
}

func TestCommands(t *testing.T) {
	cfg := &config.Config{
		Token:      token,
		AllowedIPs: []string{"192.168.1.10"},
		Commands: map[string]config.Command{
			"hello": {Exec: []string{"/bin/echo", "hi"}},
			"alpha": {Exec: []string{"/bin/true"}},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	s := New(cfg, &fakePower{}, "test")
	var pending []func()
	s.after = func(_ time.Duration, fn func()) { pending = append(pending, fn) }
	var ran []string
	s.runCmd = func(c config.Command) error { ran = append(ran, strings.Join(c.Exec, " ")); return nil }
	h := s.Handler()
	auth, ip := "Bearer "+token, "192.168.1.10:1"

	w := do(h, "GET", "/api/commands", ip, auth)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `["alpha","hello"]`) {
		t.Fatalf("list: %d %s", w.Code, w.Body)
	}
	if c := do(h, "POST", "/api/commands/nope", ip, auth).Code; c != 404 {
		t.Errorf("unknown command: got %d, want 404", c)
	}
	if c := do(h, "POST", "/api/commands/hello", "192.168.1.11:1", auth).Code; c != 403 {
		t.Errorf("wrong ip: got %d, want 403", c)
	}
	if c := do(h, "POST", "/api/commands/hello", ip, "").Code; c != 401 {
		t.Errorf("no token: got %d, want 401", c)
	}
	if c := do(h, "GET", "/api/commands/hello", ip, auth).Code; c != 405 {
		t.Errorf("GET on command: got %d, want 405", c)
	}
	if len(ran) != 0 {
		t.Fatalf("rejected requests ran commands: %v", ran)
	}
	if c := do(h, "POST", "/api/commands/hello", ip, auth).Code; c != http.StatusAccepted {
		t.Fatalf("run: got %d", c)
	}
	if len(ran) != 0 {
		t.Fatal("ran before the delay")
	}
	if c := do(h, "POST", "/api/commands/alpha", ip, auth).Code; c != http.StatusConflict {
		t.Errorf("while pending: got %d, want 409", c)
	}
	pending[0]()
	if got := strings.Join(ran, ","); got != "/bin/echo hi" {
		t.Errorf("ran = %q", got)
	}
}
