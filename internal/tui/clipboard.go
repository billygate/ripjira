package tui

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
)

// copyToClipboard writes text to the system clipboard via the OSC 52
// terminal escape sequence. This avoids a hard dependency on xclip/xsel
// and works in all terminals listed in the design spec (Kitty, iTerm2,
// WezTerm, Ghostty) plus tmux when allow-passthrough is enabled. Output
// goes directly to /dev/tty so the sequence reaches the terminal even
// when stdout is captured by Bubble Tea's renderer.
//
// w is exposed for tests; production callers pass nil to default to the
// real TTY.
func copyToClipboard(w io.Writer, text string) error {
	if w == nil {
		f, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	_, err := fmt.Fprintf(w, "\x1b]52;c;%s\x07", enc)
	return err
}
