package service

import (
	"bufio"
	"fmt"
	"strings"
)

// AskYesNo asks q on the controlling terminal and returns the answer, with def
// for an empty reply. It reads the terminal directly rather than stdin, so it
// works under `curl ... | bash`, where stdin is the script. ok is false when
// there is no terminal to ask on.
func AskYesNo(q string, def bool) (yes, ok bool) {
	in, out, err := openTerminal()
	if err != nil {
		return def, false
	}
	defer in.Close()
	defer out.Close()
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	fmt.Fprintf(out, "%s %s ", q, hint)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return def, false
	}
	return parseYesNo(line, def), true
}

// parseYesNo reads y/yes/n/no (any case); anything else gives def.
func parseYesNo(s string, def bool) bool {
	switch a := strings.ToLower(strings.TrimSpace(s)); {
	case strings.HasPrefix(a, "y"):
		return true
	case strings.HasPrefix(a, "n"):
		return false
	}
	return def
}
