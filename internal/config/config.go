// Package config loads, validates, and persists ripjira's YAML configuration.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// Config holds user-facing settings persisted to ~/.config/ripjira/config.yaml.
type Config struct {
	BaseURL            string `yaml:"base_url"`
	Email              string `yaml:"email"`
	DefaultProject     string `yaml:"default_project,omitempty"`
	DefaultGrouping    string `yaml:"default_grouping"`
	AutoRefreshSeconds int    `yaml:"auto_refresh_seconds"`
	Theme              string `yaml:"theme"`
	Icons              string `yaml:"icons"`
}

// ErrPermissionsTooWide is returned by Load when the config file mode is wider
// than 0600. The returned config is still valid; callers should surface the
// warning but proceed.
var ErrPermissionsTooWide = errors.New("config file permissions wider than 0600")

// Enum values accepted in config fields.
const (
	GroupingStatus   = "status"
	GroupingPriority = "priority"

	ThemeTokyoNight = "tokyonight"
	ThemeCatppuccin = "catppuccin"
	ThemeGruvbox    = "gruvbox"
	ThemeNord       = "nord"
	ThemeRosePine   = "rosepine"

	IconsUnicode = "unicode"
	IconsASCII   = "ascii"
)

var (
	validGroupings = map[string]bool{GroupingStatus: true, GroupingPriority: true}
	validThemes    = map[string]bool{
		ThemeTokyoNight: true,
		ThemeCatppuccin: true,
		ThemeGruvbox:    true,
		ThemeNord:       true,
		ThemeRosePine:   true,
	}
	validIcons = map[string]bool{IconsUnicode: true, IconsASCII: true}

	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

// Defaults returns a Config populated with default values for optional fields.
func Defaults() *Config {
	return &Config{
		DefaultGrouping:    GroupingStatus,
		AutoRefreshSeconds: 60,
		Theme:              ThemeTokyoNight,
		Icons:              IconsUnicode,
	}
}

// DefaultPath returns the XDG-aware default location of the config file.
// It honours XDG_CONFIG_HOME, falling back to $HOME/.config on all platforms
// (the spec mandates POSIX-style paths regardless of OS).
func DefaultPath() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "ripjira", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ripjira", "config.yaml"), nil
}

// Load reads and validates the config at path. If the file mode is wider than
// 0600, Load returns the parsed config alongside ErrPermissionsTooWide so
// callers may warn while still using the config.
func Load(path string) (*Config, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // path is user-supplied by design
	if err != nil {
		return nil, err
	}

	cfg := Defaults()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	applyDefaults(cfg)
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if info.Mode().Perm()&0o077 != 0 {
		return cfg, ErrPermissionsTooWide
	}
	return cfg, nil
}

// Save writes cfg to path with 0600 permissions, creating the parent directory.
func Save(path string, cfg *Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Validate ensures all required fields are present and enum-valued fields are
// known.
func (c *Config) Validate() error {
	if c.BaseURL == "" {
		return errors.New("base_url is required")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("base_url is not a valid URL: %q", c.BaseURL)
	}
	if c.Email == "" {
		return errors.New("email is required")
	}
	if !emailRe.MatchString(c.Email) {
		return fmt.Errorf("email is not valid: %q", c.Email)
	}
	if !validGroupings[c.DefaultGrouping] {
		return fmt.Errorf("default_grouping %q must be one of status|priority", c.DefaultGrouping)
	}
	if !validThemes[c.Theme] {
		return fmt.Errorf("theme %q is unknown", c.Theme)
	}
	if !validIcons[c.Icons] {
		return fmt.Errorf("icons %q must be one of unicode|ascii", c.Icons)
	}
	if c.AutoRefreshSeconds < 0 {
		return fmt.Errorf("auto_refresh_seconds must be ≥ 0, got %d", c.AutoRefreshSeconds)
	}
	return nil
}

func applyDefaults(c *Config) {
	d := Defaults()
	if c.DefaultGrouping == "" {
		c.DefaultGrouping = d.DefaultGrouping
	}
	if c.Theme == "" {
		c.Theme = d.Theme
	}
	if c.Icons == "" {
		c.Icons = d.Icons
	}
}
