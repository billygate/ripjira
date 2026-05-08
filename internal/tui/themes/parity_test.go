package themes_test

import (
	"testing"

	"github.com/billygate/ripjira/internal/config"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// configThemeConstants is the canonical list of theme names exposed to
// users via config. The "catppuccin" alias is intentionally excluded —
// it resolves to catppuccin-mocha but is not its own palette.
var configThemeConstants = []string{
	config.ThemeTokyoNight,
	config.ThemeCatppuccinMocha,
	config.ThemeGruvbox,
	config.ThemeNord,
	config.ThemeRosePine,
	config.ThemeDracula,
	config.ThemeSolarizedDark,
	config.ThemeSolarizedLight,
	config.ThemeEverforest,
	config.ThemeKanagawa,
	config.ThemeMonokai,
	config.ThemeOneDark,
}

// TestConfigConstantsAreRegistered guards against drift: every theme
// constant exposed in package config must resolve to a registered
// palette.
func TestConfigConstantsAreRegistered(t *testing.T) {
	for _, name := range configThemeConstants {
		if _, err := themes.ByName(name); err != nil {
			t.Errorf("config exposes %q but themes.ByName fails: %v", name, err)
		}
	}
}

// TestRegistryIsCovered guards the other direction: every registered
// theme (excluding the catppuccin alias) must have a matching
// config.Theme* constant, so users can actually select it via YAML or
// the Settings overlay.
func TestRegistryIsCovered(t *testing.T) {
	covered := make(map[string]bool, len(configThemeConstants))
	for _, n := range configThemeConstants {
		covered[n] = true
	}
	for _, name := range themes.Names() {
		if name == "catppuccin" {
			continue // documented alias
		}
		if !covered[name] {
			t.Errorf("themes registry has %q but no matching config.Theme* constant", name)
		}
	}
}
