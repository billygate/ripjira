//go:build windows

package main

import (
	"fmt"
	"io"
)

// reExec is a no-op on Windows: ripjira targets unix terminals, and
// spawning a detached child to replace the current process here is not
// worth the complexity. We tell the user to restart and exit cleanly.
func reExec(errw io.Writer) error {
	_, _ = fmt.Fprintln(errw, "ripjira: theme saved; restart ripjira to apply")
	return nil
}
