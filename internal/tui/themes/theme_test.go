package themes

import (
	"testing"
)

var allPalettes = []string{"tokyonight", "catppuccin-mocha", "gruvbox", "nord", "rosepine"}

func TestByName_All(t *testing.T) {
	for _, name := range allPalettes {
		p, err := ByName(name)
		if err != nil {
			t.Fatalf("ByName(%q) returned error: %v", name, err)
		}
		if p == nil {
			t.Fatalf("ByName(%q) returned nil palette", name)
		}
		if got := p.Name(); got != name {
			t.Errorf("Name() = %q, want %q", got, name)
		}
	}
}

func TestByName_CatppuccinAliasResolvesToMocha(t *testing.T) {
	p, err := ByName("catppuccin")
	if err != nil {
		t.Fatalf("ByName(\"catppuccin\") error: %v", err)
	}
	if p.Name() != "catppuccin-mocha" {
		t.Errorf("alias resolved to %q, want catppuccin-mocha", p.Name())
	}
}

func TestByName_CaseInsensitive(t *testing.T) {
	for _, name := range []string{"TokyoNight", "TOKYONIGHT", "  tokyonight  ", "Catppuccin-Mocha", "Catppuccin", "GRUVBOX", "Nord", "RosePine"} {
		if _, err := ByName(name); err != nil {
			t.Errorf("ByName(%q) error: %v", name, err)
		}
	}
}

func TestByName_Unknown(t *testing.T) {
	if _, err := ByName("nope"); err == nil {
		t.Error("ByName(\"nope\") expected error, got nil")
	}
}

func TestNames_ContainsAll(t *testing.T) {
	got := make(map[string]bool)
	for _, n := range Names() {
		got[n] = true
	}
	for _, want := range allPalettes {
		if !got[want] {
			t.Errorf("Names() = %v, expected to include %q", Names(), want)
		}
	}
}

func TestPalette_NamedColorsNonEmpty(t *testing.T) {
	for _, name := range allPalettes {
		p, err := ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		checks := map[string]string{
			"Bg":      string(p.Bg()),
			"Fg":      string(p.Fg()),
			"Accent":  string(p.Accent()),
			"Muted":   string(p.Muted()),
			"Red":     string(p.Red()),
			"Green":   string(p.Green()),
			"Yellow":  string(p.Yellow()),
			"Blue":    string(p.Blue()),
			"Magenta": string(p.Magenta()),
			"Cyan":    string(p.Cyan()),
		}
		for label, val := range checks {
			if val == "" {
				t.Errorf("%s: %s() returned empty color", name, label)
			}
		}
	}
}

func TestPalette_Priority(t *testing.T) {
	for _, name := range allPalettes {
		p, err := ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		cases := []struct{ in, label string }{
			{"Highest", "Highest"},
			{"high", "high"},
			{"Medium", "Medium"},
			{"LOW", "LOW"},
			{"Lowest", "Lowest"},
			{"", "empty"},
			{"weird", "unknown"},
		}
		muted := string(p.Muted())
		for _, c := range cases {
			got := string(p.Priority(c.in))
			if got == "" {
				t.Errorf("%s: Priority(%q) returned empty color", name, c.in)
			}
			switch c.label {
			case "Highest", "high", "Medium", "LOW", "Lowest":
				if got == muted {
					t.Errorf("%s: Priority(%q) = Muted, expected a distinct color", name, c.in)
				}
			case "empty", "unknown":
				if got != muted {
					t.Errorf("%s: Priority(%q) = %q, expected Muted (%q)", name, c.in, got, muted)
				}
			}
		}
	}
}

func TestPalette_Status(t *testing.T) {
	for _, name := range allPalettes {
		p, err := ByName(name)
		if err != nil {
			t.Fatal(err)
		}
		muted := string(p.Muted())
		knownCases := []struct {
			category string
			want     string
		}{
			{"new", string(p.Blue())},
			{"NEW", string(p.Blue())},
			{"indeterminate", string(p.Yellow())},
			{"done", string(p.Green())},
		}
		for _, c := range knownCases {
			if got := string(p.Status(c.category)); got != c.want {
				t.Errorf("%s: Status(%q) = %q, want %q", name, c.category, got, c.want)
			}
		}
		for _, in := range []string{"", "garbage"} {
			if got := string(p.Status(in)); got != muted {
				t.Errorf("%s: Status(%q) = %q, want Muted (%q)", name, in, got, muted)
			}
		}
	}
}
