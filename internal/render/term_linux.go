//go:build linux

package render

import (
	"os"
	"syscall"
	"unsafe"
)

// IsTerminal reports whether f is a terminal.
func IsTerminal(f *os.File) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TCGETS, uintptr(unsafe.Pointer(&t)))
	return errno == 0
}

// TerminalWidth returns the number of columns of the terminal f, or 0 when it
// cannot be determined.
func TerminalWidth(f *os.File) int {
	var ws struct{ Row, Col, X, Y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCGWINSZ, uintptr(unsafe.Pointer(&ws)))
	if errno != 0 {
		return 0
	}
	return int(ws.Col)
}

// PrepareTerminal makes f ready for ANSI escape sequences. Linux terminals
// need no preparation.
func PrepareTerminal(f *os.File) {}
