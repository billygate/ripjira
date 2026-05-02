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
	return Styles{
		Palette: p,

		App:     lipgloss.NewStyle().Foreground(p.Fg()).Background(p.Bg()),
		TopBar:  lipgloss.NewStyle().Foreground(p.Accent()).Bold(true).Padding(0, 1),
		HintBar: lipgloss.NewStyle().Foreground(p.Muted()).Padding(0, 1),

		PaneBorder:        lipgloss.NewStyle().Border(border).BorderForeground(p.Muted()),
		PaneBorderFocused: lipgloss.NewStyle().Border(border).BorderForeground(p.Accent()),
		PaneTitle:         lipgloss.NewStyle().Foreground(p.Accent()).Bold(true),

		ListItem:         lipgloss.NewStyle().Foreground(p.Fg()),
		ListItemSelected: lipgloss.NewStyle().Foreground(p.Bg()).Background(p.Accent()).Bold(true),
		GroupHeader:      lipgloss.NewStyle().Foreground(p.Cyan()).Bold(true),

		SectionHeader: lipgloss.NewStyle().Foreground(p.Accent()).Bold(true).Underline(true),
		Description:   lipgloss.NewStyle().Foreground(p.Fg()),

		Muted:  lipgloss.NewStyle().Foreground(p.Muted()),
		Accent: lipgloss.NewStyle().Foreground(p.Accent()),
		Error:  lipgloss.NewStyle().Foreground(p.Red()).Bold(true),
		Toast:  lipgloss.NewStyle().Foreground(p.Bg()).Background(p.Yellow()).Padding(0, 1),

		OverlayBorder: lipgloss.NewStyle().Border(border).BorderForeground(p.Accent()).Padding(1, 2),
		OverlayTitle:  lipgloss.NewStyle().Foreground(p.Magenta()).Bold(true),

		PreviewBadge: lipgloss.NewStyle().
			Foreground(p.Bg()).
			Background(p.Green()).
			Bold(true).
			Padding(0, 1),

		ActiveTab: lipgloss.NewStyle().
			Foreground(p.Bg()).
			Background(p.Accent()).
			Bold(true).
			Padding(0, 2),
		InactiveTab: lipgloss.NewStyle().
			Foreground(p.Muted()).
			Padding(0, 2),
		TabDivider: lipgloss.NewStyle().
			Foreground(p.Muted()),
	}
}

// Priority returns a style for rendering a Jira priority label, with the
// foreground color sourced from the palette.
func (s Styles) Priority(name string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.Palette.Priority(name)).Bold(true)
}

// Status returns a style for rendering a Jira status category label
// ("new", "indeterminate", "done").
func (s Styles) Status(category string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(s.Palette.Status(category)).Bold(true)
}
