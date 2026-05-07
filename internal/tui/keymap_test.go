package tui

import "testing"

func TestKeymapSettingsBinding(t *testing.T) {
	km := DefaultKeymap()
	keys := km.Settings.Keys()
	if len(keys) != 1 || keys[0] != "ctrl+," {
		t.Fatalf("Settings keys = %v, want [ctrl+,]", keys)
	}
	if km.Settings.Help().Key != "ctrl+," {
		t.Fatalf("Settings help key = %q, want ctrl+,", km.Settings.Help().Key)
	}
	all := km.All()
	for _, b := range all {
		if b.Help().Key == "ctrl+," {
			return
		}
	}
	t.Fatal("Settings binding not in All()")
}
