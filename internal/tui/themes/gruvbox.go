package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type gruvbox struct{}

// Gruvbox returns the Gruvbox Dark palette.
func Gruvbox() Palette { return gruvbox{} }

func (gruvbox) Name() string { return "gruvbox" }

func (gruvbox) Bg() lipgloss.Color      { return lipgloss.Color("#282828") }
func (gruvbox) Fg() lipgloss.Color      { return lipgloss.Color("#ebdbb2") }
func (gruvbox) Accent() lipgloss.Color  { return lipgloss.Color("#fabd2f") }
func (gruvbox) Muted() lipgloss.Color   { return lipgloss.Color("#928374") }
func (gruvbox) Red() lipgloss.Color     { return lipgloss.Color("#fb4934") }
func (gruvbox) Green() lipgloss.Color   { return lipgloss.Color("#b8bb26") }
func (gruvbox) Yellow() lipgloss.Color  { return lipgloss.Color("#fabd2f") }
func (gruvbox) Blue() lipgloss.Color    { return lipgloss.Color("#83a598") }
func (gruvbox) Magenta() lipgloss.Color { return lipgloss.Color("#d3869b") }
func (gruvbox) Cyan() lipgloss.Color    { return lipgloss.Color("#8ec07c") }

func (t gruvbox) Priority(name string) lipgloss.Color {
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

func (t gruvbox) Status(category string) lipgloss.Color {
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
