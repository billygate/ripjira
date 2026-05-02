package panes

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// renderMarkdown styles markdown source for the terminal via glamour, sized
// to width. The renderer is cached per-width so paint loops don't pay the
// glamour init cost on every refresh. Empty/whitespace input returns "".
// On any glamour error the raw input is returned so the user still sees
// something rather than a silent gap.
func renderMarkdown(src string, width int) string {
	if strings.TrimSpace(src) == "" {
		return ""
	}
	if width <= 0 {
		width = 80
	}
	r := mdRendererFor(width)
	if r == nil {
		return src
	}
	out, err := r.Render(src)
	if err != nil {
		return src
	}
	return strings.Trim(out, "\n")
}

var (
	mdRendererMu    sync.Mutex
	mdRendererCache = map[int]*glamour.TermRenderer{}
)

func mdRendererFor(width int) *glamour.TermRenderer {
	mdRendererMu.Lock()
	defer mdRendererMu.Unlock()
	if r, ok := mdRendererCache[width]; ok {
		return r
	}
	// Glamour's WithAutoStyle calls termenv.HasDarkBackground, which
	// sends an OSC 11 query and blocks waiting for the terminal's
	// reply. Inside an already-running Bubble Tea program tea's input
	// reader consumes the response and the query stalls for its full
	// timeout (~3s). Pinning to the dark preset keeps the renderer
	// off the TTY query path; ripjira's themes are all dark-on-dark
	// anyway, so the visual result matches.
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
		glamour.WithEmoji(),
	)
	if err != nil {
		return nil
	}
	mdRendererCache[width] = r
	return r
}
