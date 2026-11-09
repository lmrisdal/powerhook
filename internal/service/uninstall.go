package service

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

// component is one removable piece of a powerhook install.
type component struct {
	name   string
	remove func() error
}

// cmdUninstall is the interactive uninstaller. It shows what's installed, lets
// the user pick what to remove, and offers a final "everything" option.
func Uninstall() error {
	if err := RequireAdmin("uninstall"); err != nil {
		return err
	}
	comps, err := installedComponents()
	if err != nil {
		return err
	}
	if len(comps) == 0 {
		fmt.Println("Nothing to uninstall.")
		return nil
	}

	fmt.Println("Installed powerhook components:")
	for i, c := range comps {
		fmt.Printf("  %d) %s\n", i+1, c.name)
	}
	everythingIdx := len(comps) + 1
	fmt.Printf("  %d) Everything\n", everythingIdx)

	r := bufio.NewReader(os.Stdin)
	sel, err := prompt(r, "Enter numbers (space/comma separated) or 'q' to cancel: ")
	if err != nil || sel == "" || strings.EqualFold(sel, "q") {
		fmt.Println("Cancelled.")
		return nil
	}
	picks, err := parseSelection(sel, everythingIdx)
	if err != nil {
		return err
	}

	var toRemove []component
	for i, c := range comps {
		if picks[everythingIdx] || picks[i+1] {
			toRemove = append(toRemove, c)
		}
	}

	fmt.Println("\nAbout to remove:")
	for _, c := range toRemove {
		fmt.Printf("  - %s\n", c.name)
	}
	ans, _ := prompt(r, "Proceed? [y/N] ")
	if !strings.HasPrefix(strings.ToLower(ans), "y") {
		fmt.Println("Aborted.")
		return nil
	}

	for _, c := range toRemove {
		log.Printf("removing %s", c.name)
		if err := c.remove(); err != nil {
			return err
		}
	}
	if len(toRemove) == len(comps) {
		fmt.Println("\npowerhook has been fully uninstalled.")
	} else {
		fmt.Printf("\nRemoved %d component(s).\n", len(toRemove))
	}
	return nil
}

func parseSelection(s string, max int) (map[int]bool, error) {
	picks := make(map[int]bool)
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 1 || n > max {
			return nil, fmt.Errorf("invalid selection %q (choose 1-%d)", f, max)
		}
		picks[n] = true
	}
	return picks, nil
}

func prompt(r *bufio.Reader, q string) (string, error) {
	fmt.Print(q)
	line, err := r.ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
