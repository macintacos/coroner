package graphics_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/macintacos/coroner/internal/graphics"
)

func env(pairs map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		value, ok := pairs[key]
		return value, ok
	}
}

var _ = DescribeTable("Detect",
	func(pairs map[string]string, want graphics.Protocol) {
		Expect(graphics.Detect(env(pairs))).To(Equal(want))
	},
	Entry("an empty environment", map[string]string{}, graphics.None),
	Entry("a dumb terminal", map[string]string{"TERM": "dumb"}, graphics.None),
	Entry("Terminal.app", map[string]string{"TERM": "xterm-256color", "TERM_PROGRAM": "Apple_Terminal"}, graphics.None),
	Entry("WezTerm, which has no Unicode placeholders", map[string]string{"TERM_PROGRAM": "WezTerm", "TERM": "xterm-256color"}, graphics.None),

	Entry("kitty by its window id", map[string]string{"KITTY_WINDOW_ID": "1"}, graphics.Kitty),
	Entry("kitty by its TERM", map[string]string{"TERM": "xterm-kitty"}, graphics.Kitty),
	Entry("Ghostty by TERM_PROGRAM", map[string]string{"TERM_PROGRAM": "ghostty", "TERM": "xterm-256color"}, graphics.Kitty),
	Entry("Ghostty by its TERM", map[string]string{"TERM": "xterm-ghostty"}, graphics.Kitty),

	Entry("iTerm2 3.7.0", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.0"}, graphics.Kitty),
	Entry("iTerm2 3.7.1", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.1"}, graphics.Kitty),
	Entry("iTerm2 3.10", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.10"}, graphics.Kitty),
	Entry("iTerm2 4.0", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "4.0"}, graphics.Kitty),
	Entry("a 3.7 beta of iTerm2", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.7.0beta6"}, graphics.Kitty),
	Entry("iTerm2 3.6.9, before Kitty images", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "3.6.9"}, graphics.None),
	Entry("iTerm2 2.9", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "2.9"}, graphics.None),
	Entry("iTerm2 with no version", map[string]string{"TERM_PROGRAM": "iTerm.app"}, graphics.None),
	Entry("iTerm2 with a version that is not a number", map[string]string{"TERM_PROGRAM": "iTerm.app", "TERM_PROGRAM_VERSION": "nightly"}, graphics.None),
	Entry("iTerm2 by LC_TERMINAL, as it reaches ssh", map[string]string{"LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.7.1"}, graphics.Kitty),
	Entry("iTerm2 by LC_TERMINAL at 3.6", map[string]string{"LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.6.0"}, graphics.None),

	Entry("tmux, even inside Ghostty", map[string]string{"TMUX": "/tmp/tmux-501/default,1,0", "TERM_PROGRAM": "ghostty", "TERM": "tmux-256color"}, graphics.None),
	Entry("tmux by TMUX alone, inside kitty", map[string]string{"TMUX": "/tmp/tmux-501/default,1,0", "KITTY_WINDOW_ID": "1"}, graphics.None),
	Entry("a tmux TERM", map[string]string{"TERM": "tmux-256color", "KITTY_WINDOW_ID": "1"}, graphics.None),
	Entry("GNU screen by STY, inside iTerm2", map[string]string{"STY": "1234.pts-0", "LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.7.1"}, graphics.None),
	Entry("a screen TERM", map[string]string{"TERM": "screen-256color", "TERM_PROGRAM": "ghostty"}, graphics.None),
	Entry("Zellij, inside Ghostty", map[string]string{"ZELLIJ": "0", "TERM_PROGRAM": "ghostty"}, graphics.None),
	Entry("an empty TMUX, as a shell that left tmux keeps it", map[string]string{"TMUX": "", "TERM": "xterm-kitty"}, graphics.Kitty),

	Entry("VS Code launched from kitty, which keeps KITTY_WINDOW_ID", map[string]string{"TERM_PROGRAM": "vscode", "KITTY_WINDOW_ID": "1"}, graphics.None),
	Entry("another program under a kitty TERM", map[string]string{"TERM_PROGRAM": "vscode", "TERM": "xterm-kitty"}, graphics.None),
	Entry("another program under a Ghostty TERM", map[string]string{"TERM_PROGRAM": "vscode", "TERM": "xterm-ghostty"}, graphics.None),
	Entry("another program over ssh from iTerm2", map[string]string{"TERM_PROGRAM": "vscode", "LC_TERMINAL": "iTerm2", "LC_TERMINAL_VERSION": "3.7.1"}, graphics.None),
	Entry("Ghostty with a KITTY_WINDOW_ID left over", map[string]string{"TERM_PROGRAM": "ghostty", "KITTY_WINDOW_ID": "1"}, graphics.Kitty),
)
