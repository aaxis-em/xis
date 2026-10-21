// Package termbg sets the terminal background before bubbletea's package
// init asks the terminal for it. A terminal that does not answer makes that
// query wait 5 seconds in every xis command. Go initializes the packages
// that are ready in import path order, and this path sorts before
// github.com/charmbracelet/bubbletea.
package termbg

import (
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func init() { lipgloss.SetHasDarkBackground(Dark()) }

// Dark reads COLORFGBG ("15;0" is light text on black), which many terminals
// set. Without it, xis assumes a dark background.
func Dark() bool {
	v := os.Getenv("COLORFGBG")
	bg, err := strconv.Atoi(v[strings.LastIndex(v, ";")+1:])
	if v == "" || err != nil {
		return true
	}
	return bg < 7 || bg == 8
}
