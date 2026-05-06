// Package styles holds all lipgloss.Style instances used by the ripjira TUI.
// Every color in this package is sourced from a themes.Palette — no hex
// literals appear here, so swapping the palette swaps the entire look.
package styles

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/themes"
)

// Styles is the full set of lipgloss styles the TUI consumes. It is a value
// type built once from a Palette via New and copied as needed.
type Styles struct {
	Palette themes.Palette

	App     lipgloss.Style
	TopBar  lipgloss.Style
	HintBar lipgloss.Style

	PaneBorder        lipgloss.Style
	PaneBorderFocused lipgloss.Style
	PaneTitle         lipgloss.Style

	ListItem         lipgloss.Style
	ListItemSelected lipgloss.Style
	GroupHeader      lipgloss.Style

	SectionHeader lipgloss.Style
	Description   lipgloss.Style

	Muted  lipgloss.Style
	Accent lipgloss.Style
	Error  lipgloss.Style
	Toast  lipgloss.Style

	OverlayBorder lipgloss.Style
	OverlayTitle  lipgloss.Style

	PreviewBadge lipgloss.Style

	ActiveTab   lipgloss.Style
	InactiveTab lipgloss.Style
	TabDivider  lipgloss.Style
}

// New constructs a Styles bound to the given palette.
func New(p themes.Palette) Styles {
	border := lipgloss.RoundedBorder()
	bg := p.Bg()
	on := func(s lipgloss.Style) lipgloss.Style { return s.Background(bg) }
	return Styles{
		Palette: p,

		App:     lipgloss.NewStyle().Foreground(p.Fg()).Background(bg),
		TopBar:  on(lipgloss.NewStyle().Foreground(p.Accent()).Bold(true).Padding(0, 1)),
		HintBar: on(lipgloss.NewStyle().Foreground(p.Muted()).Padding(0, 1)),

		PaneBorder:        on(lipgloss.NewStyle().Border(border).BorderForeground(p.Muted()).BorderBackground(bg)),
		PaneBorderFocused: on(lipgloss.NewStyle().Border(border).BorderForeground(p.Accent()).BorderBackground(bg)),
		PaneTitle:         on(lipgloss.NewStyle().Foreground(p.Accent()).Bold(true)),

		ListItem:         on(lipgloss.NewStyle().Foreground(p.Fg())),
		ListItemSelected: lipgloss.NewStyle().Foreground(bg).Background(p.Accent()).Bold(true),
		GroupHeader:      on(lipgloss.NewStyle().Foreground(p.Cyan()).Bold(true)),

		SectionHeader: on(lipgloss.NewStyle().Foreground(p.Accent()).Bold(true).Underline(true)),
		Description:   on(lipgloss.NewStyle().Foreground(p.Fg())),

		Muted:  on(lipgloss.NewStyle().Foreground(p.Muted())),
		Accent: on(lipgloss.NewStyle().Foreground(p.Accent())),
		Error:  on(lipgloss.NewStyle().Foreground(p.Red()).Bold(true)),
		Toast:  lipgloss.NewStyle().Foreground(bg).Background(p.Yellow()).Padding(0, 1),

		OverlayBorder: lipgloss.NewStyle().Border(border).BorderForeground(p.Accent()).BorderBackground(bg).Background(bg).Padding(1, 2),
		OverlayTitle:  on(lipgloss.NewStyle().Foreground(p.Magenta()).Bold(true)),

		PreviewBadge: lipgloss.NewStyle().
			Foreground(bg).
			Background(p.Green()).
			Bold(true).
			Padding(0, 1),

		ActiveTab: on(lipgloss.NewStyle().
			Foreground(p.Accent()).
			Bold(true).
			Underline(true).
			Padding(0, 1)),
		InactiveTab: on(lipgloss.NewStyle().
			Foreground(p.Muted()).
			Padding(0, 1)),
		TabDivider: on(lipgloss.NewStyle().
			Foreground(p.Muted())),
	}
}

// Priority returns a style for rendering a Jira priority label, with the
// foreground color sourced from the palette and the bg pinned to the
// palette's Bg so concatenating it with surrounding text doesn't leave a
// terminal-default-bg gap on dark themes.
func (s Styles) Priority(name string) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.Palette.Priority(name)).
		Background(s.Palette.Bg()).
		Bold(true)
}

// Status returns a style for rendering a Jira status category label
// ("new", "indeterminate", "done"). Bg is pinned for the same reason as
// Priority.
func (s Styles) Status(category string) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(s.Palette.Status(category)).
		Background(s.Palette.Bg()).
		Bold(true)
}
