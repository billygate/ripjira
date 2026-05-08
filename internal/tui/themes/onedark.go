package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type oneDark struct{}

// OneDark returns the Atom / nvim One Dark palette.
func OneDark() Palette { return oneDark{} }

func (oneDark) Name() string { return "onedark" }

func (oneDark) Bg() lipgloss.Color      { return lipgloss.Color("#282c34") }
func (oneDark) Fg() lipgloss.Color      { return lipgloss.Color("#abb2bf") }
func (oneDark) Accent() lipgloss.Color  { return lipgloss.Color("#61afef") }
func (oneDark) Muted() lipgloss.Color   { return lipgloss.Color("#5c6370") }
func (oneDark) Red() lipgloss.Color     { return lipgloss.Color("#e06c75") }
func (oneDark) Green() lipgloss.Color   { return lipgloss.Color("#98c379") }
func (oneDark) Yellow() lipgloss.Color  { return lipgloss.Color("#e5c07b") }
func (oneDark) Blue() lipgloss.Color    { return lipgloss.Color("#61afef") }
func (oneDark) Magenta() lipgloss.Color { return lipgloss.Color("#c678dd") }
func (oneDark) Cyan() lipgloss.Color    { return lipgloss.Color("#56b6c2") }

func (t oneDark) Priority(name string) lipgloss.Color {
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

func (t oneDark) Status(category string) lipgloss.Color {
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
