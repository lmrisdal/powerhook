package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	DefaultListen  = ":9876"
	minTokenLength = 16

	DefaultCommandTimeout = 30 * time.Second
	maxCommandTimeoutS    = 3600
)

var commandNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

// Command is a named program the API may run. Callers pick it by name only;
// nothing from the request reaches the command line.
type Command struct {
	// Exec is the program and its arguments. It is run directly, without a
	// shell, and Exec[0] must be an absolute path.
	Exec []string `json:"exec"`

	// TimeoutS kills the command after this many seconds. Defaults to 30.
	TimeoutS int `json:"timeout_s,omitempty"`
}

func (c Command) Timeout() time.Duration {
	if c.TimeoutS <= 0 {
		return DefaultCommandTimeout
	}
	return time.Duration(c.TimeoutS) * time.Second
}

type Config struct {
	// Listen is the address to bind, e.g. ":9876". Defaults to ":9876".
	Listen string `json:"listen,omitempty"`

	// Token is the shared secret; clients send "Authorization: Bearer <token>".
	// Empty means no token is required.
	Token string `json:"token"`

	// AllowedIPs lists client addresses or CIDR ranges allowed to call the API.
	// Everything else gets 403. Empty means any address may call it.
	AllowedIPs []string `json:"allowed_ips"`

	// ActionDelayMS is the delay between answering the request and performing
	// the action, so the caller gets its response first. Defaults to 1000.
	ActionDelayMS int `json:"action_delay_ms,omitempty"`

	// DryRun logs actions instead of performing them.
	DryRun bool `json:"dry_run,omitempty"`

	// ForceCloseApps (Windows only): don't let apps block shutdown/restart.
	// Unsaved work in those apps is lost.
	ForceCloseApps bool `json:"force_close_apps,omitempty"`

	// Commands are named programs callable via POST /api/commands/<name>.
	Commands map[string]Command `json:"commands,omitempty"`

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
	return c.Token != ""
}

func (c *Config) ActionDelay() time.Duration {
	if c.ActionDelayMS <= 0 {
		return time.Second
	}
	return time.Duration(c.ActionDelayMS) * time.Millisecond
}

func Load(path string) (*Config, error) {
	if path == "" {
		// A config in a directory only root can open (like /etc/powerhook)
		// fails Stat with a permission error, not "not exist".
		var denied []string
		for _, p := range SearchPaths() {
			_, err := os.Stat(p)
			if err == nil {
				path = p
				break
			}
			if errors.Is(err, fs.ErrPermission) {
				denied = append(denied, p)
			}
		}
		if path == "" {
			if len(denied) > 0 {
				return nil, fmt.Errorf("can't read %s (permission denied); run with sudo", strings.Join(denied, ", "))
			}
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
		return fmt.Errorf("\"token\" must be at least %d characters (e.g. from: openssl rand -base64 32), or empty to not require one", minTokenLength)
	}
	for name, cmd := range c.Commands {
		if !commandNameRE.MatchString(name) {
			return fmt.Errorf("\"commands\": name %q must be 1-64 letters, digits, '-' or '_', starting with a letter or digit", name)
		}
		if len(cmd.Exec) == 0 || cmd.Exec[0] == "" {
			return fmt.Errorf("\"commands\": %q needs a non-empty \"exec\" list", name)
		}
		if !filepath.IsAbs(cmd.Exec[0]) {
			return fmt.Errorf("\"commands\": %q: exec[0] %q must be an absolute path", name, cmd.Exec[0])
		}
		if cmd.TimeoutS < 0 || cmd.TimeoutS > maxCommandTimeoutS {
			return fmt.Errorf("\"commands\": %q: \"timeout_s\" must be between 0 and %d", name, maxCommandTimeoutS)
		}
	}
	c.Allowed = c.Allowed[:0]
	for _, s := range c.AllowedIPs {
		p, err := ParsePrefix(s)
		if err != nil {
			return fmt.Errorf("\"allowed_ips\": %w", err)
		}
		c.Allowed = append(c.Allowed, p)
	}
	if len(c.Allowed) == 0 {
		c.Allowed = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0"), netip.MustParsePrefix("::/0")}
	}
	return nil
}

// CommandNames returns the configured command names, sorted.
func (c *Config) CommandNames() []string {
	names := make([]string, 0, len(c.Commands))
	for n := range c.Commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// CommandWarnings lists commands whose program is missing or not a file. It's
// advisory, since the file may be installed later.
func (c *Config) CommandWarnings() []string {
	var out []string
	for _, n := range c.CommandNames() {
		exe := c.Commands[n].Exec[0]
		if st, err := os.Stat(exe); err != nil || st.IsDir() {
			out = append(out, fmt.Sprintf("command %q: %s not found", n, exe))
		}
	}
	return out
}

// AllowsAnyIP reports whether every source address is allowed, either because
// allowed_ips is empty or because it contains 0.0.0.0/0 and ::/0.
func (c *Config) AllowsAnyIP() bool {
	var v4, v6 bool
	for _, p := range c.Allowed {
		if p.Bits() == 0 {
			if p.Addr().Is4() {
				v4 = true
			} else {
				v6 = true
			}
		}
	}
	return v4 && v6
}

// AllowedList describes allowed_ips for logs and status output.
func (c *Config) AllowedList() string {
	if len(c.AllowedIPs) == 0 {
		return "any address"
	}
	return strings.Join(c.AllowedIPs, ", ")
}

// Unprotected reports whether anyone who can reach the port can call the API:
// no token and no IP restriction.
func (c *Config) Unprotected() bool {
	return !c.TokenRequired() && c.AllowsAnyIP()
}

// UnprotectedWarning is shown wherever an unprotected config is used.
const UnprotectedWarning = `WARNING: no token and no IP restriction. Anyone who can reach this port can
suspend, power off or restart this machine. Set a "token" or list addresses
in "allowed_ips" unless the network itself is trusted.`

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
