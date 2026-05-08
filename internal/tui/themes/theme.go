// Package themes defines the Palette interface and registers concrete
// palettes used by the ripjira TUI. UI styles consume colors exclusively
// from a Palette — no hex literals live outside this package.
package themes

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Palette is a named color palette with semantic getters. Implementations
// must return a non-empty color for every method, including unknown
// priority/status inputs (typically falling back to Muted).
type Palette interface {
	Name() string

	Bg() lipgloss.Color
	Fg() lipgloss.Color
	Accent() lipgloss.Color
	Muted() lipgloss.Color
	Red() lipgloss.Color
	Green() lipgloss.Color
	Yellow() lipgloss.Color
	Blue() lipgloss.Color
	Magenta() lipgloss.Color
	Cyan() lipgloss.Color

	// Priority returns a color for the given Jira priority name
	// (e.g. "Highest", "High", "Medium", "Low", "Lowest"). Matching is
	// case-insensitive. Unknown names fall back to Muted.
	Priority(name string) lipgloss.Color

	// Status returns a color for a Jira status category: "new",
	// "indeterminate", or "done". Matching is case-insensitive. Unknown
	// categories fall back to Muted.
	Status(category string) lipgloss.Color
}

var registry = map[string]func() Palette{
	"tokyonight":       func() Palette { return TokyoNight() },
	"catppuccin-mocha": func() Palette { return Catppuccin() },
	"catppuccin":       func() Palette { return Catppuccin() }, // alias, kept for older configs
	"gruvbox":          func() Palette { return Gruvbox() },
	"nord":             func() Palette { return Nord() },
	"rosepine":         func() Palette { return RosePine() },
	"dracula":          func() Palette { return Dracula() },
	"solarized-dark":   func() Palette { return SolarizedDark() },
	"solarized-light":  func() Palette { return SolarizedLight() },
	"everforest":       func() Palette { return Everforest() },
	"kanagawa":         func() Palette { return Kanagawa() },
	"monokai":          func() Palette { return Monokai() },
	"onedark":          func() Palette { return OneDark() },
}

// ByName returns the registered palette with the given name. Lookup is
// case-insensitive.
func ByName(name string) (Palette, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	factory, ok := registry[key]
	if !ok {
		return nil, fmt.Errorf("themes: unknown palette %q", name)
	}
	return factory(), nil
}

// Names returns the registered palette names in no particular order.
func Names() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
