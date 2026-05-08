package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type kanagawa struct{}

// Kanagawa returns the Kanagawa Wave palette (rebelot/kanagawa.nvim).
func Kanagawa() Palette { return kanagawa{} }

func (kanagawa) Name() string { return "kanagawa" }

func (kanagawa) Bg() lipgloss.Color      { return lipgloss.Color("#1f1f28") }
func (kanagawa) Fg() lipgloss.Color      { return lipgloss.Color("#dcd7ba") }
func (kanagawa) Accent() lipgloss.Color  { return lipgloss.Color("#7e9cd8") }
func (kanagawa) Muted() lipgloss.Color   { return lipgloss.Color("#727169") }
func (kanagawa) Red() lipgloss.Color     { return lipgloss.Color("#e82424") }
func (kanagawa) Green() lipgloss.Color   { return lipgloss.Color("#76946a") }
func (kanagawa) Yellow() lipgloss.Color  { return lipgloss.Color("#dca561") }
func (kanagawa) Blue() lipgloss.Color    { return lipgloss.Color("#7e9cd8") }
func (kanagawa) Magenta() lipgloss.Color { return lipgloss.Color("#957fb8") }
func (kanagawa) Cyan() lipgloss.Color    { return lipgloss.Color("#7fb4ca") }

func (t kanagawa) Priority(name string) lipgloss.Color {
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

func (t kanagawa) Status(category string) lipgloss.Color {
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
