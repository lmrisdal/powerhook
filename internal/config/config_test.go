package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExampleConfig(t *testing.T) {
	data, err := os.ReadFile("../../packaging/config.example.json")
	if err != nil {
		t.Fatal(err)
	}
	// The example ships with a placeholder token, which must be rejected...
	if _, err := Load(writeConfig(t, string(data))); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("placeholder token accepted: %v", err)
	}
	// ...and loads once a real one is filled in (as the installers do).
	body := strings.Replace(string(data), "CHANGE_ME", NewToken(), 1)
	c, err := Load(writeConfig(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if c.Port() != "9876" || c.ActionDelay() != time.Second || len(c.Allowed) == 0 {
		t.Fatalf("unexpected config: %+v", c)
	}
}

func TestConfigErrors(t *testing.T) {
	tok := NewToken()
	cases := []struct{ name, body, want string }{
		{"short token", `{"token":"short","allowed_ips":["1.2.3.4"]}`, "token"},
		{"no allowed ips", `{"token":"` + tok + `","allowed_ips":[]}`, "allowed_ips"},
		{"bad ip", `{"token":"` + tok + `","allowed_ips":["192.168.1"]}`, "not a valid"},
		{"bad listen", `{"listen":"9876","token":"` + tok + `","allowed_ips":["1.2.3.4"]}`, "listen"},
		{"unknown key", `{"token":"` + tok + `","allowed_ip":["1.2.3.4"]}`, "unknown field"},
		{"no token, allow all v4", `{"require_token":false,"allowed_ips":["192.168.1.5","0.0.0.0/0"]}`, "allows everyone"},
		{"no token, allow all v6", `{"require_token":false,"allowed_ips":["::/0"]}`, "allows everyone"},
	}
	for _, c := range cases {
		_, err := Load(writeConfig(t, c.body))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: got %v, want error containing %q", c.name, err, c.want)
		}
	}
}

func TestRequireToken(t *testing.T) {
	c, err := Load(writeConfig(t, `{"require_token":false,"allowed_ips":["192.168.1.5"]}`))
	if err != nil {
		t.Fatalf("token should be optional with require_token false: %v", err)
	}
	if c.TokenRequired() {
		t.Error("TokenRequired() = true, want false")
	}
	c, err = Load(writeConfig(t, `{"token":"`+NewToken()+`","allowed_ips":["192.168.1.5"]}`))
	if err != nil || !c.TokenRequired() {
		t.Errorf("token should be required by default: %v", err)
	}
}

func TestBOMAndSingleIP(t *testing.T) {
	c, err := Load(writeConfig(t, "\xef\xbb\xbf"+`{"token":"`+NewToken()+`","allowed_ips":["192.168.1.10"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Allowed[0].String(); got != "192.168.1.10/32" {
		t.Errorf("single IP parsed as %s", got)
	}
}
