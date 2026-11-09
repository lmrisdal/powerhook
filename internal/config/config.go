package config

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strings"
	"time"
)

const (
	DefaultListen  = ":9876"
	minTokenLength = 16
)

type Config struct {
	// Listen is the address to bind, e.g. ":9876". Defaults to ":9876".
	Listen string `json:"listen,omitempty"`

	// Token is the shared secret; clients send "Authorization: Bearer <token>".
	Token string `json:"token,omitempty"`

	// RequireToken: set to false to skip the token check and rely on the IP
	// allowlist alone. Defaults to true.
	RequireToken *bool `json:"require_token,omitempty"`

	// AllowedIPs lists client addresses or CIDR ranges allowed to call the API.
	// Everything else gets 403.
	AllowedIPs []string `json:"allowed_ips"`

	// ActionDelayMS is the delay between answering the request and performing
	// the action, so the caller gets its response first. Defaults to 1000.
	ActionDelayMS int `json:"action_delay_ms,omitempty"`

	// DryRun logs actions instead of performing them.
	DryRun bool `json:"dry_run,omitempty"`

	// ForceCloseApps (Windows only): don't let apps block shutdown/restart.
	// Unsaved work in those apps is lost.
	ForceCloseApps bool `json:"force_close_apps,omitempty"`

	Path    string         `json:"-"` // source file
	Allowed []netip.Prefix `json:"-"` // parsed AllowedIPs
}

func (c *Config) ListenAddr() string {
	if c.Listen == "" {
		return DefaultListen
	}
	return c.Listen
}

func (c *Config) Port() string {
	_, port, _ := net.SplitHostPort(c.ListenAddr())
	return port
}

// TokenRequired reports whether requests must carry the token.
func (c *Config) TokenRequired() bool {
	return c.RequireToken == nil || *c.RequireToken
}

func (c *Config) ActionDelay() time.Duration {
	if c.ActionDelayMS <= 0 {
		return time.Second
	}
	return time.Duration(c.ActionDelayMS) * time.Millisecond
}

func Load(path string) (*Config, error) {
	if path == "" {
		for _, p := range SearchPaths() {
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
		if path == "" {
			return nil, fmt.Errorf("no config file found; checked %s", strings.Join(SearchPaths(), ", "))
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}
	// Tolerate a UTF-8 BOM (Windows editors like to add one).
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // a typo like "allowed_ip" should fail loudly
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	c.Path = path
	return &c, nil
}

func (c *Config) Validate() error {
	if _, _, err := net.SplitHostPort(c.ListenAddr()); err != nil {
		return fmt.Errorf("\"listen\" %q: %w", c.Listen, err)
	}
	if c.TokenRequired() && (len(c.Token) < minTokenLength || c.Token == "CHANGE_ME") {
		return fmt.Errorf("\"token\" must be at least %d characters (generate one with: powerhook token), or set \"require_token\": false", minTokenLength)
	}
	if len(c.AllowedIPs) == 0 {
		return fmt.Errorf("\"allowed_ips\" is empty; add the addresses allowed to call the API")
	}
	c.Allowed = c.Allowed[:0]
	for _, s := range c.AllowedIPs {
		p, err := ParsePrefix(s)
		if err != nil {
			return fmt.Errorf("\"allowed_ips\": %w", err)
		}
		// Without a token, the allowlist is the only protection; refuse to
		// leave the API open to everyone.
		if !c.TokenRequired() && p.Bits() == 0 {
			return fmt.Errorf("\"allowed_ips\" contains %s, which allows everyone; that needs \"require_token\": true", p)
		}
		c.Allowed = append(c.Allowed, p)
	}
	return nil
}

// parsePrefix accepts a single address ("192.168.1.10") or a CIDR range.
func ParsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "/") {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("%q is not a valid address or CIDR range", s)
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q is not a valid address or CIDR range", s)
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
