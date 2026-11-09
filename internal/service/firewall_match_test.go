package service

import (
	"net/netip"
	"testing"

	"powerhook/internal/config"
)

func cfgWith(t *testing.T, ips ...string) *config.Config {
	t.Helper()
	cfg := &config.Config{AllowedIPs: ips}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

const ufwStatus = `Status: active

To                         Action      From
--                         ------      ----
22/tcp                     ALLOW       Anywhere
9876/tcp                   ALLOW       10.0.20.0/24               # powerhook
9876/tcp                   ALLOW       192.168.1.5                # powerhook
22/tcp (v6)                ALLOW       Anywhere (v6)
`

const ufwStatusAny = `Status: active

To                         Action      From
--                         ------      ----
9876/tcp                   ALLOW       Anywhere                   # powerhook
9876/tcp (v6)              ALLOW       Anywhere (v6)              # powerhook
`

func TestUfwMatch(t *testing.T) {
	sources, portsOK := parseUfwStatus(ufwStatus, "9876")
	if !portsOK || len(sources) != 2 {
		t.Fatalf("parsed %v, portsOK=%v", sources, portsOK)
	}
	if !rulesMatch(cfgWith(t, "192.168.1.5", "10.0.20.0/24"), sources) {
		t.Error("same ranges in another order should match")
	}
	if rulesMatch(cfgWith(t, "10.0.20.0/24"), sources) {
		t.Error("extra rule should not match")
	}
	if _, portsOK := parseUfwStatus(ufwStatus, "9877"); portsOK {
		t.Error("rules for another port should not match")
	}

	sources, portsOK = parseUfwStatus(ufwStatusAny, "9876")
	if !portsOK || !rulesMatch(cfgWith(t), sources) {
		t.Errorf("Anywhere rules should match an empty allowlist: %v", sources)
	}
	// ufw with IPv6 disabled only has the v4 rule.
	if !rulesMatch(cfgWith(t), []netip.Prefix{anyV4}) {
		t.Error("lone IPv4 catch-all should match an empty allowlist")
	}
	if rulesMatch(cfgWith(t, "10.0.20.0/24"), sources) {
		t.Error("Anywhere should not match a restricted allowlist")
	}
}

func TestFirewalldRichRules(t *testing.T) {
	out := `rule family="ipv4" source address="10.0.20.0/24" service name="powerhook" accept
rule family="ipv4" source address="10.0.0.9" service name="ssh" accept
rule family="ipv6" source address="2001:db8::/32" service name="powerhook" accept
`
	got := parseFirewalldRichRules(out)
	if !samePrefixes(got, cfgWith(t, "2001:db8::/32", "10.0.20.0/24").Allowed) {
		t.Errorf("got %v", got)
	}
}

func TestWindowsRemote(t *testing.T) {
	cases := map[string]string{
		"Any":                     "0.0.0.0/0",
		"10.0.20.0/255.255.255.0": "10.0.20.0/24",
		"192.168.1.5":             "192.168.1.5/32",
		"2001:db8::/32":           "2001:db8::/32",
	}
	for in, want := range cases {
		p, ok := parseWindowsRemote(in)
		if !ok || p.String() != want {
			t.Errorf("parseWindowsRemote(%q) = %v, %v; want %s", in, p, ok, want)
		}
	}
	if _, ok := parseWindowsRemote("10.0.0.1/255.x"); ok {
		t.Error("bad mask accepted")
	}
}
