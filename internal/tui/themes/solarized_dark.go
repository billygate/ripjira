package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type solarizedDark struct{}

// SolarizedDark returns the Solarized Dark palette by Ethan Schoonover.
func SolarizedDark() Palette { return solarizedDark{} }

func (solarizedDark) Name() string { return "solarized-dark" }

func (solarizedDark) Bg() lipgloss.Color      { return lipgloss.Color("#002b36") }
func (solarizedDark) Fg() lipgloss.Color      { return lipgloss.Color("#839496") }
func (solarizedDark) Accent() lipgloss.Color  { return lipgloss.Color("#268bd2") }
func (solarizedDark) Muted() lipgloss.Color   { return lipgloss.Color("#586e75") }
func (solarizedDark) Red() lipgloss.Color     { return lipgloss.Color("#dc322f") }
func (solarizedDark) Green() lipgloss.Color   { return lipgloss.Color("#859900") }
func (solarizedDark) Yellow() lipgloss.Color  { return lipgloss.Color("#b58900") }
func (solarizedDark) Blue() lipgloss.Color    { return lipgloss.Color("#268bd2") }
func (solarizedDark) Magenta() lipgloss.Color { return lipgloss.Color("#d33682") }
func (solarizedDark) Cyan() lipgloss.Color    { return lipgloss.Color("#2aa198") }

func (t solarizedDark) Priority(name string) lipgloss.Color {
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

func (t solarizedDark) Status(category string) lipgloss.Color {
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
