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

func TestKeymap_WatchAndUnwatchDocumented(t *testing.T) {
	km := DefaultKeymap()
	if keys := km.Watch.Keys(); len(keys) != 1 || keys[0] != "w" {
		t.Fatalf("Watch keys = %v, want [w]", keys)
	}
	if keys := km.Unwatch.Keys(); len(keys) != 1 || keys[0] != "W" {
		t.Fatalf("Unwatch keys = %v, want [W]", keys)
	}
	var sawWatch, sawUnwatch bool
	for _, b := range km.All() {
		switch b.Help().Key {
		case "w":
			sawWatch = true
		case "W":
			sawUnwatch = true
		}
	}
	if !sawWatch {
		t.Fatal("Watch binding missing from All()")
	}
	if !sawUnwatch {
		t.Fatal("Unwatch binding missing from All()")
	}
	sawWatch, sawUnwatch = false, false
	for _, col := range km.FullHelp() {
		for _, b := range col {
			switch b.Help().Key {
			case "w":
				sawWatch = true
			case "W":
				sawUnwatch = true
			}
		}
	}
	if !sawWatch {
		t.Fatal("Watch binding missing from FullHelp() (help overlay would hide it)")
	}
	if !sawUnwatch {
		t.Fatal("Unwatch binding missing from FullHelp() (help overlay would hide it)")
	}
}

func TestKeymap_EditExternalBindsCtrlE(t *testing.T) {
	km := DefaultKeymap()
	if !km.EditExternal.Enabled() {
		t.Fatal("EditExternal should be enabled by default")
	}
	keys := km.EditExternal.Keys()
	if len(keys) != 1 || keys[0] != "ctrl+e" {
		t.Fatalf("EditExternal keys: got %v want [ctrl+e]", keys)
	}
}
