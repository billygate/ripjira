//go:build !windows

package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// reExec replaces the current process with a fresh ripjira invocation,
// preserving the original argv and environment. Called when the user
// changes the active theme — live re-styling leaves rendering
// artifacts on some terminals, so a clean re-launch is the safest fix.
//
// On success syscall.Exec does not return; if it does, we treat that
// as a soft failure and instruct the user to restart manually rather
// than continue in an inconsistent state.
func reExec(errw io.Writer) error {
	exe, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(errw, "ripjira: theme saved; restart ripjira to apply")
		return nil
	}
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		_, _ = fmt.Fprintf(errw, "ripjira: theme saved; restart manually (exec: %v)\n", err)
	}
	return nil
}
