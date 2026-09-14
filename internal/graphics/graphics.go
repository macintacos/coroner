// Package graphics decides whether a terminal can show images inline and builds what it sends
// one: the Kitty graphics escape sequences, the placeholder cells an image shows through, and
// the text that stands in for an image where none can show. It does no I/O.
package graphics

import (
	"strconv"
	"strings"
)

// Protocol is how a terminal can show an image.
type Protocol int

const (
	// None shows no image: a placeholder text stands in for it.
	None Protocol = iota
	// Kitty shows an image through Kitty graphics Unicode placeholders, text cells a program
	// redraws like any other.
	Kitty
)

// Detect returns the protocol the terminal lookupEnv describes can show images through, from
// environment hints alone. A multiplexer shows none, since it swallows graphics escapes unless
// configured to pass them through.
func Detect(lookupEnv func(string) (string, bool)) Protocol {
	term := env(lookupEnv, "TERM")
	switch {
	case env(lookupEnv, "TMUX") != "", env(lookupEnv, "STY") != "", env(lookupEnv, "ZELLIJ") != "",
		strings.HasPrefix(term, "tmux"), strings.HasPrefix(term, "screen"):
		return None
	case env(lookupEnv, "KITTY_WINDOW_ID") != "", term == "xterm-kitty",
		env(lookupEnv, "TERM_PROGRAM") == "ghostty", term == "xterm-ghostty":
		return Kitty
	case env(lookupEnv, "TERM_PROGRAM") == "iTerm.app" && atLeast37(env(lookupEnv, "TERM_PROGRAM_VERSION")),
		env(lookupEnv, "LC_TERMINAL") == "iTerm2" && atLeast37(env(lookupEnv, "LC_TERMINAL_VERSION")):
		return Kitty
	}

	return None
}

func env(lookupEnv func(string) (string, bool), key string) string {
	value, _ := lookupEnv(key)
	return value
}

// atLeast37 reports whether version, such as 3.7.1 or 3.7.0beta6, is iTerm2 3.7 or later, the
// first with Kitty graphics.
func atLeast37(version string) bool {
	majorText, rest, _ := strings.Cut(version, ".")
	minorText, _, _ := strings.Cut(rest, ".")
	major, err := strconv.Atoi(majorText)
	if err != nil {
		return false
	}
	minor, err := strconv.Atoi(minorText)
	if err != nil {
		return major > 3
	}

	return major > 3 || major == 3 && minor >= 7
}
