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

// seamBgAcrossResets walks s and inserts bgSGR after every \x1b[0m that is
// not immediately followed by another SGR sequence. This patches the
// "black gap" lipgloss leaves when a child span ends mid-line and plain
// text follows — the next printable rune starts at terminal-default bg
// otherwise. Glamour's markdown output is the typical source: each styled
// word ends with \x1b[0m before a space or the next styled word.
func seamBgAcrossResets(s, bgSGR string) string {
	if bgSGR == "" || s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 32)
	runes := []rune(s)
	resetPending := false
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r == 0x1b && i+1 < len(runes) && runes[i+1] == '[' {
			j := i + 2
			for j < len(runes) {
				c := runes[j]
				if c >= 0x40 && c <= 0x7e && c != ';' {
					break
				}
				j++
			}
			if j < len(runes) && runes[j] == 'm' {
				params := string(runes[i+2 : j])
				if params == "" || params == "0" {
					resetPending = true
				} else if sgrSetsBg(params) {
					resetPending = false
				}
			}
			b.WriteString(string(runes[i : j+1]))
			i = j + 1
			continue
		}
		if r == '\n' {
			resetPending = false
			b.WriteRune(r)
			i++
			continue
		}
		if resetPending {
			b.WriteString(bgSGR)
			resetPending = false
		}
		b.WriteRune(r)
		i++
	}
	return b.String()
}

// sgrSetsBg reports whether an SGR parameter list explicitly sets a bg color.
// 49 (default bg) is intentionally NOT treated as setting one — it would
// re-leak the terminal default.
func sgrSetsBg(params string) bool {
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		if parts[i] == "48" {
			return true
		}
	}
	return false
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
