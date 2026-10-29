//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	user32           = syscall.NewLazyDLL("user32.dll")
	procAttach       = kernel32.NewProc("AttachConsole")
	procSetTitle     = kernel32.NewProc("SetConsoleTitleW")
	procCreateMutex  = kernel32.NewProc("CreateMutexW")
	procReleaseMutex = kernel32.NewProc("ReleaseMutex")
	procMessageBox   = user32.NewProc("MessageBoxW")
)

// attachConsole is needed because Nodelight.exe is a windowless (GUI
// subsystem) program: double-clicking it must not open a console. When it is
// started from a terminal, this attaches to that terminal so --help, --json
// and the first-run questions keep working. It reports whether standard output
// is usable (a terminal or a redirection).
func attachConsole() bool {
	if _, err := os.Stdout.Stat(); err == nil {
		return true // output is redirected to a file or a pipe
	}
	const attachParentProcess = 0xFFFFFFFF
	if r, _, _ := procAttach.Call(attachParentProcess); r == 0 {
		return false // started from Explorer: no console to use
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stdout = f
	}
	if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
		os.Stderr = f
	}
	if f, err := os.OpenFile("CONIN$", os.O_RDONLY, 0); err == nil {
		os.Stdin = f
	}
	return true
}

// setConsoleTitle names the console window (best effort).
func setConsoleTitle(title string) {
	p, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	procSetTitle.Call(uintptr(unsafe.Pointer(p)))
}

// showMessage displays a modal message box (used when there is no console).
func showMessage(title, text string, isError bool) {
	t, err1 := syscall.UTF16PtrFromString(title)
	m, err2 := syscall.UTF16PtrFromString(text)
	if err1 != nil || err2 != nil {
		return
	}
	flags := uintptr(0x40) // MB_ICONINFORMATION
	if isError {
		flags = 0x10 // MB_ICONERROR
	}
	procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}

// singleInstance returns false when another Nodelight is already running (two
// instances would fight over the same loopback ports).
func singleInstance() (release func(), ok bool) {
	name, err := syscall.UTF16PtrFromString(`Local\Nodelight-single-instance`)
	if err != nil {
		return func() {}, true
	}
	h, _, e := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return func() {}, true // cannot tell: do not block the user
	}
	if e == syscall.ERROR_ALREADY_EXISTS {
		return func() {}, false
	}
	return func() { procReleaseMutex.Call(h) }, true
}
