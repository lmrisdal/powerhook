package service

import (
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"powerhook/internal/config"
)

// Helpers for reading powerhook's current firewall rules back and comparing
// them with the config. They're plain functions over command output so they
// can be tested on any OS.

var (
	anyV4 = netip.MustParsePrefix("0.0.0.0/0")
	anyV6 = netip.MustParsePrefix("::/0")
)

// rulesMatch reports whether the sources found in the firewall are what cfg
// asks for. When cfg allows any address, a lone IPv4 catch-all is accepted too,
// since a firewall with IPv6 disabled only creates that one.
func rulesMatch(cfg *config.Config, found []netip.Prefix) bool {
	if cfg.AllowsAnyIP() {
		has4, other := false, false
		for _, p := range found {
			switch p {
			case anyV4:
				has4 = true
			case anyV6:
			default:
				other = true
			}
		}
		return has4 && !other
	}
	return samePrefixes(cfg.Allowed, found)
}

func samePrefixes(a, b []netip.Prefix) bool {
	set := func(ps []netip.Prefix) []string {
		out := make([]string, 0, len(ps))
		seen := map[string]bool{}
		for _, p := range ps {
			if s := p.Masked().String(); !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
		sort.Strings(out)
		return out
	}
	x, y := set(a), set(b)
	if len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

var ufwColumns = regexp.MustCompile(`\s{2,}`)

// parseUfwStatus reads the powerhook rules from `ufw status` output, e.g.
//
//	9876/tcp                   ALLOW       10.0.20.0/24               # powerhook
//	9876/tcp (v6)              ALLOW       Anywhere (v6)              # powerhook
//
// It returns the source of each rule and whether every rule is for port.
func parseUfwStatus(out, port string) (sources []netip.Prefix, portsOK bool) {
	portsOK = true
	for _, line := range strings.Split(out, "\n") {
		rule, comment, ok := strings.Cut(line, "#")
		if !ok || strings.TrimSpace(comment) != Name {
			continue
		}
		cols := ufwColumns.Split(strings.TrimSpace(rule), -1)
		if len(cols) < 3 {
			portsOK = false
			continue
		}
		if strings.TrimSuffix(cols[0], " (v6)") != port+"/tcp" {
			portsOK = false
		}
		switch from := cols[len(cols)-1]; from {
		case "Anywhere":
			sources = append(sources, anyV4)
		case "Anywhere (v6)":
			sources = append(sources, anyV6)
		default:
			p, err := config.ParsePrefix(from)
			if err != nil {
				portsOK = false
				continue
			}
			sources = append(sources, p)
		}
	}
	return sources, portsOK
}

var richSource = regexp.MustCompile(`source address="([^"]+)"`)

// parseFirewalldRichRules returns the source addresses of powerhook's rich rules.
func parseFirewalldRichRules(out string) []netip.Prefix {
	var sources []netip.Prefix
	for _, rule := range strings.Split(out, "\n") {
		if !strings.Contains(rule, `service name="`+Name+`"`) {
			continue
		}
		if m := richSource.FindStringSubmatch(rule); m != nil {
			if p, err := config.ParsePrefix(m[1]); err == nil {
				sources = append(sources, p)
			}
		}
	}
	return sources
}

// parseWindowsRemote converts a Get-NetFirewallAddressFilter RemoteAddress
// entry ("Any", "10.0.20.0/255.255.255.0", "192.168.1.5", "2001:db8::/32").
func parseWindowsRemote(s string) (netip.Prefix, bool) {
	s = strings.TrimSpace(s)
	if strings.EqualFold(s, "any") {
		return anyV4, true
	}
	if addr, mask, ok := strings.Cut(s, "/"); ok && strings.Contains(mask, ".") {
		m, err := netip.ParseAddr(mask)
		if err != nil || !m.Is4() {
			return netip.Prefix{}, false
		}
		bits := 0
		for _, octet := range m.As4() {
			bits += popcount(octet)
		}
		a, err := netip.ParseAddr(addr)
		if err != nil {
			return netip.Prefix{}, false
		}
		return netip.PrefixFrom(a, bits).Masked(), true
	}
	p, err := config.ParsePrefix(s)
	return p, err == nil
}

func popcount(b byte) int {
	n := 0
	for ; b != 0; b &= b - 1 {
		n++
	}
	return n
}
