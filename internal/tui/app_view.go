package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/billygate/ripjira/internal/tui/gfx"
	"github.com/billygate/ripjira/internal/tui/panes"
)

// View implements tea.Model.
func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	topBar := m.renderTopBar()
	tabBar := m.renderTabBar()
	hintBar := m.renderHintBar()

	listW, detailW, previewW, contentHeight := m.paneDims()
	var body string
	if m.preview.Active {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderDetailPane(detailW, contentHeight),
			m.renderPreviewPane(previewW, contentHeight),
		)
	} else {
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderListPane(listW, contentHeight),
			m.renderDetailPane(detailW, contentHeight),
		)
	}

	parts := []string{topBar, tabBar, body}
	if m.editorInstallPrompt {
		parts = append(parts, m.renderInstallPrompt())
	}
	parts = append(parts, hintBar)
	frame := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Toasts float as a top-centre overlay rather than taking a row in the
	// vertical layout. Inlining them used to grow the frame past m.height
	// when one fired, which made bubbletea drop the topmost row (the
	// program-name + tab strip) until the toast TTL expired.
	if toast := m.toasts.View(m.styles); toast != "" {
		frame = overlayTopCenter(frame, toast, m.chromeHeights.topBar+m.chromeHeights.tabBar)
	}

	if v := m.activeOverlay(); v != "" {
		frame = overlayCenter(frame, v)
	}
	out := m.styles.App.Width(m.width).Height(m.height).Render(frame)
	return seamFrameBg(out, hexToBgSGR(string(m.styles.Palette.Bg())))
}

// seamFrameBg patches "black gaps" in the rendered frame. Lipgloss does not
// re-establish a parent's Background after a child span's `\x1b[0m` reset,
// so plain text concatenated outside a styled span (gaps in detail
// rendering, glamour-rendered markdown, overlay bodies) leaks the host
// terminal's default bg through. This walks the rendered string and
// inserts bgSGR before every printable rune that follows a `\x1b[0m`
// without an interleaving SGR that already sets a bg. The visible output
// is unchanged for cells that were already styled; only the unstyled gaps
// pick up the palette's bg. Empty bgSGR (legacy / unknown palette) leaves
// content unchanged.
func seamFrameBg(s, bgSGR string) string {
	if bgSGR == "" || s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 64)
	resetPending := false
	i := 0
	for i < len(s) {
		// ASCII fast path: byte that isn't ESC or LF.
		if c := s[i]; c < 0x80 && c != 0x1b && c != '\n' {
			if resetPending {
				b.WriteString(bgSGR)
				resetPending = false
			}
			b.WriteByte(c)
			i++
			continue
		}
		if s[i] == '\n' {
			resetPending = false
			b.WriteByte('\n')
			i++
			continue
		}
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) {
				c := s[j]
				if (c >= 0x40 && c <= 0x7e) && c != ';' {
					break
				}
				j++
			}
			if j < len(s) && s[j] == 'm' {
				params := s[i+2 : j]
				if params == "" || params == "0" {
					resetPending = true
				} else if sgrSetsBg(params) {
					resetPending = false
				}
			}
			b.WriteString(s[i : j+1])
			i = j + 1
			continue
		}
		// Multi-byte UTF-8 (or stray ESC without '['): decode, copy.
		r, size := utf8.DecodeRuneInString(s[i:])
		if resetPending {
			b.WriteString(bgSGR)
			resetPending = false
		}
		b.WriteRune(r)
		i += size
	}
	return b.String()
}

// sgrSetsBg reports whether an SGR parameter list explicitly sets a bg color.
// Matches the 256-color (48;5;…) and truecolor (48;2;R;G;B) prefixes plus
// the legacy 8/16-color codes. SGR 49 (default bg) is intentionally NOT
// treated as setting one — it would re-leak the terminal default.
func sgrSetsBg(params string) bool {
	parts := strings.Split(params, ";")
	for _, p := range parts {
		if p == "48" {
			return true
		}
	}
	return false
}

// hexToBgSGR returns the truecolor SGR escape that paints the given hex
// color (#RRGGBB) as the background. Returns "" for malformed input.
func hexToBgSGR(hex string) string {
	if len(hex) != 7 || hex[0] != '#' {
		return ""
	}
	r, err := strconv.ParseInt(hex[1:3], 16, 0)
	if err != nil {
		return ""
	}
	g, err := strconv.ParseInt(hex[3:5], 16, 0)
	if err != nil {
		return ""
	}
	bb, err := strconv.ParseInt(hex[5:7], 16, 0)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, bb)
}

// activeOverlay returns the visible overlay's rendered view, or "" when no
// overlay is showing. Order matches the input-routing precedence in
// handleKey so the same modal that captures keypresses is also the one
// rendered on top.
func (m Model) activeOverlay() string {
	if v := m.help.View(m.styles); v != "" {
		return v
	}
	if v := m.transition.View(m.styles); v != "" {
		return v
	}
	if v := m.comment.View(m.styles); v != "" {
		return v
	}
	if v := m.assign.View(m.styles); v != "" {
		return v
	}
	if v := m.create.View(m.styles); v != "" {
		return v
	}
	if v := m.options.View(m.styles); v != "" {
		return v
	}
	if v := m.settings.View(m.styles); v != "" {
		return v
	}
	if v := m.edit.View(m.styles); v != "" {
		return v
	}
	if v := m.favorites.View(m.styles); v != "" {
		return v
	}
	if v := m.link.View(m.styles); v != "" {
		return v
	}
	if v := m.linkRemove.View(m.styles); v != "" {
		return v
	}
	if v := m.worklog.View(m.styles); v != "" {
		return v
	}
	if v := m.worklogRemove.View(m.styles); v != "" {
		return v
	}
	if v := m.description.View(m.styles); v != "" {
		return v
	}
	if v := m.priority.View(m.styles); v != "" {
		return v
	}
	if v := m.epicPicker.View(m.styles); v != "" {
		return v
	}
	if v := m.structPicker.View(m.styles); v != "" {
		return v
	}
	if v := m.scopeEditor.View(m.styles); v != "" {
		return v
	}
	if v := m.topGo.View(m.styles); v != "" {
		return v
	}
	if v := m.created.View(m.styles); v != "" {
		return v
	}
	return ""
}

// overlayCenter places fg over the centre of bg. Both arguments may carry
// ANSI styling; cell positions are computed via lipgloss.Width / ansi helpers
// so wide runes and escape sequences don't shift the splice points.
func overlayCenter(bg, fg string) string {
	bgW := lipgloss.Width(bg)
	bgH := lipgloss.Height(bg)
	fgW := lipgloss.Width(fg)
	fgH := lipgloss.Height(fg)
	x := (bgW - fgW) / 2
	y := (bgH - fgH) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return overlayCompose(bg, fg, x, y)
}

// overlayTopCenter places fg horizontally centred on bg at vertical offset y.
// Used for transient toasts so they don't take a row in the vertical layout
// (which would push the frame past m.height and trigger bubbletea's
// drop-from-top fallback, hiding the program-name + tab strip).
func overlayTopCenter(bg, fg string, y int) string {
	bgW := lipgloss.Width(bg)
	fgW := lipgloss.Width(fg)
	x := (bgW - fgW) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return overlayCompose(bg, fg, x, y)
}

// overlayCompose splices fg onto bg starting at cell coordinate (x, y). For
// each row covered by fg the underlying bg row is cut at startX and endX;
// the gap between is replaced verbatim by the corresponding fg line. Lines
// outside fg's vertical range are left untouched.
func overlayCompose(bg, fg string, x, y int) string {
	if fg == "" {
		return bg
	}
	bgLines := strings.Split(bg, "\n")
	fgLines := strings.Split(fg, "\n")
	for i, fgLine := range fgLines {
		row := y + i
		if row < 0 || row >= len(bgLines) {
			continue
		}
		bgLine := bgLines[row]
		fgW := lipgloss.Width(fgLine)
		left := ansi.Truncate(bgLine, x, "")
		if lw := lipgloss.Width(left); lw < x {
			left += strings.Repeat(" ", x-lw)
		}
		right := ansi.TruncateLeft(bgLine, x+fgW, "")
		bgLines[row] = left + fgLine + right
	}
	return strings.Join(bgLines, "\n")
}

func (m *Model) paneDims() (listW, detailW, previewW, contentHeight int) {
	if !m.chromeHeights.valid || m.chromeHeights.width != m.width {
		m.chromeHeights.topBar = lipgloss.Height(m.renderTopBar())
		m.chromeHeights.tabBar = lipgloss.Height(m.renderTabBar())
		m.chromeHeights.hintBar = lipgloss.Height(m.renderHintBar())
		m.chromeHeights.width = m.width
		m.chromeHeights.valid = true
	}
	overhead := m.chromeHeights.topBar + m.chromeHeights.tabBar + m.chromeHeights.hintBar
	contentHeight = max(m.height-overhead, 3)
	if m.preview.Active {
		listW = 0
		detailW = m.width / 2
		previewW = m.width - detailW
		return
	}
	listW = m.width / 2
	detailW = m.width - listW
	return
}

func (m Model) renderTopBar() string {
	// Left: home label + tabs. The tab strip stays visually pinned so it
	// doesn't shift when transient indicators come and go.
	left := []string{m.styles.TopBar.Render("~/RJ>"), m.renderTabs()}
	// Right: spinner / prefetch / status — anchored to the right edge so
	// they don't push the tab strip around.
	right := make([]string, 0, 3)
	if sp := m.spinner.View(); sp != "" {
		right = append(right, m.styles.Accent.Render(sp))
	}
	if pi := m.renderPrefetchIndicator(); pi != "" {
		right = append(right, pi)
	}
	if m.statusText != "" {
		right = append(right, m.styles.Muted.Render(m.statusText))
	}
	sep := m.styles.App.Render(" ")
	leftStr := strings.Join(left, sep)
	rightStr := strings.Join(right, sep)
	if m.width <= 0 {
		return m.styles.App.Render(leftStr + sep + rightStr)
	}
	leftW, rightW := lipgloss.Width(leftStr), lipgloss.Width(rightStr)
	gap := m.width - leftW - rightW
	if gap < 1 {
		// Not enough room for both — clip the tab strip and keep the
		// indicators visible. Single space between left and right.
		clipTo := m.width - rightW - 1
		if clipTo < 0 {
			clipTo = 0
		}
		leftStr = ansi.Truncate(leftStr, clipTo, "")
		gap = 1
	}
	pad := m.styles.App.Render(strings.Repeat(" ", gap))
	return m.styles.App.Width(m.width).Render(leftStr + pad + rightStr)
}

// renderTabs returns just the horizontal tab cells, with the active view
// highlighted. Search is a transient mode (entered via `/`) rather than a
// tab; when active it appends a SEARCH cell so the user has a visual cue,
// but it is not part of the `[`/`]` cycle.
// renderTabs draws a single-row drill-down strip: the active top group's
// label, then a `›` separator, then the sub-views of that group as pills.
// Other top groups are reachable via `}`/`{`. When the active top has only
// one sub-view (Sprint / Structures / Search), only the top label is shown.
func (m Model) renderTabs() string {
	active := panes.TopGroup(m.view)
	topCell := m.styles.ActiveTab.Render(active.String()) +
		m.styles.Muted.Render("[g]")
	subs := panes.SubViews(active)
	if len(subs) <= 1 {
		return topCell
	}
	subCells := make([]string, 0, len(subs))
	for _, v := range subs {
		label := panes.SubLabel(v)
		if v == m.view {
			subCells = append(subCells, m.styles.ActiveTab.Render(label))
		} else {
			subCells = append(subCells, m.styles.InactiveTab.Render(label))
		}
	}
	sep := m.styles.Muted.Render(" › ")
	return topCell + sep + lipgloss.JoinHorizontal(lipgloss.Top, subCells...)
}

// renderPrefetchIndicator returns a static "▒ caching N/M" chip when the
// detail cache is warming up, otherwise "". Static glyph (no animation) is
// the deliberate distinction from the spinner — the spinner means "the user
// is waiting on a network call", this indicator means "the user is free to
// keep working while we warm the cache in the background".
func (m Model) renderPrefetchIndicator() string {
	pf, ok := m.loader.(prefetchProgressReporter)
	if !ok {
		return ""
	}
	done, total, active := pf.PrefetchProgress()
	if !active || total == 0 {
		return ""
	}
	return m.styles.Muted.Render(fmt.Sprintf("▒ %d/%d", done, total))
}

func (m Model) renderHintBar() string {
	parts := []string{
		"↑/↓ nav",
		"}/{ top tab",
		"]/[ sub-tab",
		"? help",
	}
	return m.styles.HintBar.Width(m.width).Render(strings.Join(parts, "  "))
}

func (m Model) renderListPane(w, h int) string {
	border := m.styles.PaneBorder
	if m.focus == FocusList {
		border = m.styles.PaneBorderFocused
	}
	title := m.styles.PaneTitle.Render("Issues")
	body := m.list.View()
	if body == "" {
		body = m.styles.Muted.Render("Loading…")
	}
	content := title + "\n" + body
	// MaxHeight clamps overflow: bubbles/list's pagination feedback loop
	// can yield a View() one row taller than its configured height when
	// items > PerPage. Without the clamp the pane grows past h rows, the
	// frame exceeds m.height, and bubbletea drops the topmost row of
	// chrome (program-name + tab strip).
	return border.Width(max(w-2, 1)).Height(max(h-2, 1)).MaxHeight(max(h, 1)).Render(content)
}

func (m Model) renderDetailPane(w, h int) string {
	border := m.styles.PaneBorder
	if m.focus == FocusDetail {
		border = m.styles.PaneBorderFocused
	}
	title := m.styles.PaneTitle.Render("Details")
	body := m.detail.View()
	content := title + "\n" + body
	return border.Width(max(w-2, 1)).Height(max(h-2, 1)).MaxHeight(max(h, 1)).Render(content)
}

// renderPreviewPane draws the third pane that holds the inline image
// preview. The pane intentionally skips lipgloss border rendering around
// the body — the Kitty / iTerm2 graphics escape contains APC sequences
// lipgloss cannot measure, so wrapping it in a styled border distorts
// neighbouring content. We emit a minimal title row, the escape (or a
// placeholder while it loads), and a "←  back" hint.
func (m Model) renderPreviewPane(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	att := m.preview.Attachment
	title := m.styles.PaneTitle.Render("Preview: " + truncatePane(att.Filename, max(w-12, 4)))
	hint := m.styles.Muted.Render("←  back")
	innerW := max(w-2, 1)
	innerH := max(h-3, 1)
	preview := m.detail.AttachmentPreview(att.ID)
	var bodyBlock string
	if preview.Escape == "" {
		bodyBlock = lipgloss.NewStyle().
			Width(innerW).
			Height(innerH).
			Align(lipgloss.Center, lipgloss.Center).
			Render(m.styles.Muted.Render("Loading preview…"))
	} else {
		// Centre the image within the preview pane. The escape is a
		// zero-cell glyph from lipgloss's perspective, so we have to
		// pad with real spaces and newlines around it. Use the cell
		// footprint reported by gfx.Render to compute the slack on
		// each axis.
		padX := max((innerW-preview.Cols)/2, 0)
		padY := max((innerH-preview.Rows)/2, 0)
		var b strings.Builder
		for range padY {
			b.WriteString("\n")
		}
		b.WriteString(strings.Repeat(" ", padX))
		b.WriteString(preview.Escape)
		bodyBlock = lipgloss.NewStyle().Width(innerW).Height(innerH).Render(b.String())
	}
	return lipgloss.JoinVertical(lipgloss.Left, title, bodyBlock, hint)
}

// clearGraphicsCmd returns a tea.Cmd that writes the host terminal's
// "delete all images" escape directly to stdout. We deliberately do this
// outside View() because lipgloss/bubbletea's ANSI parser has no concept
// of APC sequences — embedding the escape in the rendered frame caused
// the renderer to misalign rows and stutter on every keypress. A direct
// stdout write fires once at the moment the user leaves the preview pane
// and is invisible to bubbletea's diff renderer.
func clearGraphicsCmd() tea.Cmd {
	clr := gfx.ClearAll()
	if clr == "" {
		return nil
	}
	return func() tea.Msg {
		_, _ = os.Stdout.WriteString(clr)
		return nil
	}
}

// truncatePane abbreviates s with an ellipsis to fit n cells. Mirrors the
// pane-internal helper without coupling app.go to internal/tui/panes.
func truncatePane(s string, n int) string {
	if n <= 1 || len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// renderTabBar returns the horizontal divider that sits below the combined
// program-name + tabs row. The tab cells themselves are rendered inline by
// renderTabs as part of renderTopBar.
func (m Model) renderTabBar() string {
	return m.styles.TabDivider.Render(strings.Repeat("─", m.width))
}

// renderInstallPrompt renders the one-line Y/N install offer shown when the
// first-launch advice detects macOS+brew without nvim. Styled as a hint bar
// so it sits naturally between the toast area and the regular hint bar.
func (m Model) renderInstallPrompt() string {
	text := "Neovim isn't installed. Install via 'brew install neovim'? [y/N]"
	return m.styles.HintBar.Width(m.width).Render(text)
}
