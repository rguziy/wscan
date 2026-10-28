//go:build !linux && !windows

package render

import "os"

// IsTerminal reports whether f is a character device (a terminal or
// /dev/null; good enough until native console support is added).
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// TerminalWidth returns 0: the width is not detected on this platform yet.
func TerminalWidth(f *os.File) int { return 0 }

// PrepareTerminal makes f ready for ANSI escape sequences. Nothing to do on
// this platform.
func PrepareTerminal(f *os.File) {}
