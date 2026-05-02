package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type nord struct{}

// Nord returns the Nord palette.
func Nord() Palette { return nord{} }

func (nord) Name() string { return "nord" }

func (nord) Bg() lipgloss.Color      { return lipgloss.Color("#2e3440") }
func (nord) Fg() lipgloss.Color      { return lipgloss.Color("#d8dee9") }
func (nord) Accent() lipgloss.Color  { return lipgloss.Color("#88c0d0") }
func (nord) Muted() lipgloss.Color   { return lipgloss.Color("#4c566a") }
func (nord) Red() lipgloss.Color     { return lipgloss.Color("#bf616a") }
func (nord) Green() lipgloss.Color   { return lipgloss.Color("#a3be8c") }
func (nord) Yellow() lipgloss.Color  { return lipgloss.Color("#ebcb8b") }
func (nord) Blue() lipgloss.Color    { return lipgloss.Color("#81a1c1") }
func (nord) Magenta() lipgloss.Color { return lipgloss.Color("#b48ead") }
func (nord) Cyan() lipgloss.Color    { return lipgloss.Color("#8fbcbb") }

func (t nord) Priority(name string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "highest":
		return t.Red()
	case "high":
		return t.Magenta()
	case "medium":
		return t.Yellow()
	case "low":
		return t.Blue()
	case "lowest":
		return t.Cyan()
	default:
		return t.Muted()
	}
}

func (t nord) Status(category string) lipgloss.Color {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "new":
		return t.Blue()
	case "indeterminate":
		return t.Yellow()
	case "done":
		return t.Green()
	default:
		return t.Muted()
	}
}
