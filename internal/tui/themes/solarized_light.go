package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type solarizedLight struct{}

// SolarizedLight returns the Solarized Light palette by Ethan Schoonover.
// This is the project's first light palette; verify legibility on any
// surface that previously assumed a dark background.
func SolarizedLight() Palette { return solarizedLight{} }

func (solarizedLight) Name() string { return "solarized-light" }

func (solarizedLight) Bg() lipgloss.Color      { return lipgloss.Color("#fdf6e3") }
func (solarizedLight) Fg() lipgloss.Color      { return lipgloss.Color("#657b83") }
func (solarizedLight) Accent() lipgloss.Color  { return lipgloss.Color("#268bd2") }
func (solarizedLight) Muted() lipgloss.Color   { return lipgloss.Color("#93a1a1") }
func (solarizedLight) Red() lipgloss.Color     { return lipgloss.Color("#dc322f") }
func (solarizedLight) Green() lipgloss.Color   { return lipgloss.Color("#859900") }
func (solarizedLight) Yellow() lipgloss.Color  { return lipgloss.Color("#b58900") }
func (solarizedLight) Blue() lipgloss.Color    { return lipgloss.Color("#268bd2") }
func (solarizedLight) Magenta() lipgloss.Color { return lipgloss.Color("#d33682") }
func (solarizedLight) Cyan() lipgloss.Color    { return lipgloss.Color("#2aa198") }

func (t solarizedLight) Priority(name string) lipgloss.Color {
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

func (t solarizedLight) Status(category string) lipgloss.Color {
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
