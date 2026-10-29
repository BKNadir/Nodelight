//go:build !windows

package main

import "fmt"

func attachConsole() bool { return true }

func setConsoleTitle(string) {}

func showMessage(title, text string, isError bool) { fmt.Printf("%s: %s\n", title, text) }

func singleInstance() (release func(), ok bool) { return func() {}, true }
