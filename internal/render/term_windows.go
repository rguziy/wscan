//go:build windows

package render

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32                       = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode             = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode             = kernel32.NewProc("SetConsoleMode")
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

const enableVirtualTerminalProcessing = 0x0004

// IsTerminal reports whether f is a console.
func IsTerminal(f *os.File) bool {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

// PrepareTerminal turns on ANSI escape sequence processing for the console
// behind f (Windows 10 and later). Failure is ignored: the output is then
// only less pretty.
func PrepareTerminal(f *os.File) {
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return
	}
	procSetConsoleMode.Call(f.Fd(), uintptr(mode|enableVirtualTerminalProcessing))
}

// consoleScreenBufferInfo is CONSOLE_SCREEN_BUFFER_INFO.
type consoleScreenBufferInfo struct {
	sizeX, sizeY       int16
	cursorX, cursorY   int16
	attributes         uint16
	left, top          int16
	right, bottom      int16
	maxSizeX, maxSizeY int16
}

// TerminalWidth returns the visible console width in cells, or 0.
func TerminalWidth(f *os.File) int {
	var info consoleScreenBufferInfo
	r, _, _ := procGetConsoleScreenBufferInfo.Call(f.Fd(), uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		return 0
	}
	return int(info.right-info.left) + 1
}
