package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTranslateLayout_Russian(t *testing.T) {
	cases := map[rune]rune{
		'х': '[', 'ъ': ']', 'й': 'q', 'т': 'n',
		'с': 'c', 'ф': 'a', 'р': 'h',
		'Ы': 'S', 'П': 'G',
	}
	for in, want := range cases {
		got := translateLayout(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{in}})
		if len(got.Runes) != 1 || got.Runes[0] != want {
			t.Errorf("translate(%q) = %v, want %q", string(in), got.Runes, string(want))
		}
	}
}

func TestTranslateLayout_Greek(t *testing.T) {
	cases := map[rune]rune{
		'η': 'h', 'ν': 'n', 'φ': 'f', 'ψ': 'c',
	}
	for in, want := range cases {
		got := translateLayout(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{in}})
		if len(got.Runes) != 1 || got.Runes[0] != want {
			t.Errorf("translate(%q) = %v, want %q", string(in), got.Runes, string(want))
		}
	}
}

func TestTranslateLayout_LatinUnchanged(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
	got := translateLayout(msg)
	if got.Runes[0] != 'a' {
		t.Errorf("Latin rune mutated: got %q", string(got.Runes[0]))
	}
}

func TestTranslateLayout_NonRuneTypeIgnored(t *testing.T) {
	msg := tea.KeyMsg{Type: tea.KeyEsc}
	got := translateLayout(msg)
	if got.Type != tea.KeyEsc {
		t.Errorf("non-rune type changed: got %v", got.Type)
	}
}

func TestTranslateLayout_DoesNotMutateInput(t *testing.T) {
	original := []rune{'х'}
	msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: original}
	_ = translateLayout(msg)
	if original[0] != 'х' {
		t.Errorf("input mutated: %q", string(original[0]))
	}
}
