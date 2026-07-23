package cli

import (
	"golang.org/x/term"
)

// fdHolder is satisfied by *os.File and lets tests use plain writers.
type fdHolder interface {
	Fd() uintptr
}

// isTerminal reports whether v is backed by an interactive terminal.
func isTerminal(v any) bool {
	f, ok := v.(fdHolder)
	if !ok {
		return false
	}

	return term.IsTerminal(int(f.Fd()))
}

// useTUI decides whether the full-screen dashboard can run: not disabled by
// flag, and both the render target (stderr) and key input (stdin) are
// terminals.
func useTUI(noTUI bool, stderr, stdin any) bool {
	return !noTUI && isTerminal(stderr) && isTerminal(stdin)
}
