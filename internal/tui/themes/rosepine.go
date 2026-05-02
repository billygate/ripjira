package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type rosePine struct{}

// RosePine returns the Rosé Pine palette.
func RosePine() Palette { return rosePine{} }

func (rosePine) Name() string { return "rosepine" }

func (rosePine) Bg() lipgloss.Color      { return lipgloss.Color("#191724") }
func (rosePine) Fg() lipgloss.Color      { return lipgloss.Color("#e0def4") }
func (rosePine) Accent() lipgloss.Color  { return lipgloss.Color("#c4a7e7") }
func (rosePine) Muted() lipgloss.Color   { return lipgloss.Color("#6e6a86") }
func (rosePine) Red() lipgloss.Color     { return lipgloss.Color("#eb6f92") }
func (rosePine) Green() lipgloss.Color   { return lipgloss.Color("#56949f") }
func (rosePine) Yellow() lipgloss.Color  { return lipgloss.Color("#f6c177") }
func (rosePine) Blue() lipgloss.Color    { return lipgloss.Color("#31748f") }
func (rosePine) Magenta() lipgloss.Color { return lipgloss.Color("#c4a7e7") }
func (rosePine) Cyan() lipgloss.Color    { return lipgloss.Color("#9ccfd8") }

func (t rosePine) Priority(name string) lipgloss.Color {
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

func (t rosePine) Status(category string) lipgloss.Color {
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
