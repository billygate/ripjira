package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type monokai struct{}

// Monokai returns the classic Sublime Text Monokai palette.
func Monokai() Palette { return monokai{} }

func (monokai) Name() string { return "monokai" }

func (monokai) Bg() lipgloss.Color      { return lipgloss.Color("#272822") }
func (monokai) Fg() lipgloss.Color      { return lipgloss.Color("#f8f8f2") }
func (monokai) Accent() lipgloss.Color  { return lipgloss.Color("#f92672") }
func (monokai) Muted() lipgloss.Color   { return lipgloss.Color("#75715e") }
func (monokai) Red() lipgloss.Color     { return lipgloss.Color("#f92672") }
func (monokai) Green() lipgloss.Color   { return lipgloss.Color("#a6e22e") }
func (monokai) Yellow() lipgloss.Color  { return lipgloss.Color("#e6db74") }
func (monokai) Blue() lipgloss.Color    { return lipgloss.Color("#66d9ef") }
func (monokai) Magenta() lipgloss.Color { return lipgloss.Color("#ae81ff") }
func (monokai) Cyan() lipgloss.Color    { return lipgloss.Color("#66d9ef") }

func (t monokai) Priority(name string) lipgloss.Color {
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

func (t monokai) Status(category string) lipgloss.Color {
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
