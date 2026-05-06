package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type catppuccin struct{}

// Catppuccin returns the Catppuccin Mocha palette.
func Catppuccin() Palette { return catppuccin{} }

func (catppuccin) Name() string { return "catppuccin-mocha" }

func (catppuccin) Bg() lipgloss.Color      { return lipgloss.Color("#1e1e2e") }
func (catppuccin) Fg() lipgloss.Color      { return lipgloss.Color("#a6adc8") }
func (catppuccin) Accent() lipgloss.Color  { return lipgloss.Color("#b4befe") }
func (catppuccin) Muted() lipgloss.Color   { return lipgloss.Color("#6c7086") }
func (catppuccin) Red() lipgloss.Color     { return lipgloss.Color("#f38ba8") }
func (catppuccin) Green() lipgloss.Color   { return lipgloss.Color("#a6e3a1") }
func (catppuccin) Yellow() lipgloss.Color  { return lipgloss.Color("#f9e2af") }
func (catppuccin) Blue() lipgloss.Color    { return lipgloss.Color("#89b4fa") }
func (catppuccin) Magenta() lipgloss.Color { return lipgloss.Color("#f5c2e7") }
func (catppuccin) Cyan() lipgloss.Color    { return lipgloss.Color("#94e2d5") }

func (t catppuccin) Priority(name string) lipgloss.Color {
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

func (t catppuccin) Status(category string) lipgloss.Color {
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
