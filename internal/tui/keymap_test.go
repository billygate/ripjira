package tui

import "testing"

func TestKeymapSettingsBinding(t *testing.T) {
	km := DefaultKeymap()
	keys := km.Settings.Keys()
	if len(keys) != 1 || keys[0] != "P" {
		t.Fatalf("Settings keys = %v, want [P]", keys)
	}
	if km.Settings.Help().Key != "P" {
		t.Fatalf("Settings help key = %q, want P", km.Settings.Help().Key)
	}
	all := km.All()
	for _, b := range all {
		if b.Help().Key == "P" {
			return
		}
	}
	t.Fatal("Settings binding not in All()")
}
