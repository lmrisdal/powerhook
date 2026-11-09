package service

import "testing"

func TestParseYesNo(t *testing.T) {
	cases := []struct {
		in        string
		def, want bool
	}{
		{"", true, true},
		{"\n", false, false},
		{"y\n", false, true},
		{" YES ", false, true},
		{"n", true, false},
		{"No\n", true, false},
		{"maybe", true, true},
	}
	for _, c := range cases {
		if got := parseYesNo(c.in, c.def); got != c.want {
			t.Errorf("parseYesNo(%q, %v) = %v, want %v", c.in, c.def, got, c.want)
		}
	}
}
