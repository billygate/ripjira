package themes

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type tokyoNight struct{}

// TokyoNight returns the Tokyo Night palette.
func TokyoNight() Palette { return tokyoNight{} }

func (tokyoNight) Name() string { return "tokyonight" }

func (tokyoNight) Bg() lipgloss.Color      { return lipgloss.Color("#1a1b26") }
func (tokyoNight) Fg() lipgloss.Color      { return lipgloss.Color("#c0caf5") }
func (tokyoNight) Accent() lipgloss.Color  { return lipgloss.Color("#7aa2f7") }
func (tokyoNight) Muted() lipgloss.Color   { return lipgloss.Color("#565f89") }
func (tokyoNight) Red() lipgloss.Color     { return lipgloss.Color("#f7768e") }
func (tokyoNight) Green() lipgloss.Color   { return lipgloss.Color("#9ece6a") }
func (tokyoNight) Yellow() lipgloss.Color  { return lipgloss.Color("#e0af68") }
func (tokyoNight) Blue() lipgloss.Color    { return lipgloss.Color("#7aa2f7") }
func (tokyoNight) Magenta() lipgloss.Color { return lipgloss.Color("#bb9af7") }
func (tokyoNight) Cyan() lipgloss.Color    { return lipgloss.Color("#7dcfff") }

func (t tokyoNight) Priority(name string) lipgloss.Color {
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

func (t tokyoNight) Status(category string) lipgloss.Color {
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
