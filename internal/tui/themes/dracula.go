package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type dracula struct{}

// Dracula returns the Dracula palette (https://draculatheme.com).
func Dracula() Palette { return dracula{} }

func (dracula) Name() string { return "dracula" }

func (dracula) Bg() lipgloss.Color      { return lipgloss.Color("#282a36") }
func (dracula) Fg() lipgloss.Color      { return lipgloss.Color("#f8f8f2") }
func (dracula) Accent() lipgloss.Color  { return lipgloss.Color("#ff79c6") }
func (dracula) Muted() lipgloss.Color   { return lipgloss.Color("#6272a4") }
func (dracula) Red() lipgloss.Color     { return lipgloss.Color("#ff5555") }
func (dracula) Green() lipgloss.Color   { return lipgloss.Color("#50fa7b") }
func (dracula) Yellow() lipgloss.Color  { return lipgloss.Color("#f1fa8c") }
func (dracula) Blue() lipgloss.Color    { return lipgloss.Color("#bd93f9") }
func (dracula) Magenta() lipgloss.Color { return lipgloss.Color("#ff79c6") }
func (dracula) Cyan() lipgloss.Color    { return lipgloss.Color("#8be9fd") }

func (t dracula) Priority(name string) lipgloss.Color {
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

func (t dracula) Status(category string) lipgloss.Color {
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
