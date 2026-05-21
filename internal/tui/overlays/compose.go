package overlays

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// composeCenter splices fg over the centre of bg. Mirrors the helper in
// internal/tui but lives here so overlays can layer popups (e.g. the
// option-picker on top of the create wizard) without exporting the
// internal one. Cell positions are computed via lipgloss.Width / ansi
// helpers so wide runes and escape sequences don't shift splice points.
func composeCenter(bg, fg string) string {
	if fg == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	bgW := lipgloss.Width(bg)
	fgW := lipgloss.Width(fg)
	x := (bgW - fgW) / 2
	y := (len(bgLines) - len(fgLines)) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	for i, fgLine := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]
		w := lipgloss.Width(fgLine)
		left := ansi.Truncate(bgLine, x, "")
		if lw := lipgloss.Width(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}
		right := ansi.TruncateLeft(bgLine, x+w, "")
		bgLines[row] = left + fgLine + right
	}
	return strings.Join(bgLines, "\n")
}
