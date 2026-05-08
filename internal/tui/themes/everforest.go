package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type everforest struct{}

// Everforest returns the Everforest Dark Medium palette
// (sainnhe/everforest).
func Everforest() Palette { return everforest{} }

func (everforest) Name() string { return "everforest" }

func (everforest) Bg() lipgloss.Color      { return lipgloss.Color("#2d353b") }
func (everforest) Fg() lipgloss.Color      { return lipgloss.Color("#d3c6aa") }
func (everforest) Accent() lipgloss.Color  { return lipgloss.Color("#a7c080") }
func (everforest) Muted() lipgloss.Color   { return lipgloss.Color("#859289") }
func (everforest) Red() lipgloss.Color     { return lipgloss.Color("#e67e80") }
func (everforest) Green() lipgloss.Color   { return lipgloss.Color("#a7c080") }
func (everforest) Yellow() lipgloss.Color  { return lipgloss.Color("#dbbc7f") }
func (everforest) Blue() lipgloss.Color    { return lipgloss.Color("#7fbbb3") }
func (everforest) Magenta() lipgloss.Color { return lipgloss.Color("#d699b6") }
func (everforest) Cyan() lipgloss.Color    { return lipgloss.Color("#83c092") }

func (t everforest) Priority(name string) lipgloss.Color {
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

func (t everforest) Status(category string) lipgloss.Color {
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
