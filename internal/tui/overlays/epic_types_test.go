package overlays

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

func newEpicTypes(items []string) EpicTypes {
	closeKey := key.NewBinding(key.WithKeys("esc"))
	o := NewEpicTypes(closeKey).Show(items)
	return o
}

func sendKey(o EpicTypes, k string) (EpicTypes, tea.Cmd) {
	return o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
}

func sendNamed(o EpicTypes, t tea.KeyType) (EpicTypes, tea.Cmd) {
	return o.Update(tea.KeyMsg{Type: t})
}

func TestEpicTypesNavigation(t *testing.T) {
	o := newEpicTypes([]string{"Epic", "Epic Feature"})
	o, _ = sendNamed(o, tea.KeyDown)
	if o.Items()[o.Cursor()] != "Epic Feature" {
		t.Fatalf("cursor did not advance")
	}
	o, _ = sendNamed(o, tea.KeyDown) // clamp
	if o.Cursor() != 1 {
		t.Fatalf("cursor should clamp at last index")
	}
}

func TestEpicTypesAdd(t *testing.T) {
	o := newEpicTypes([]string{"Epic"})
	o, _ = sendKey(o, "a")
	if !o.Editing() {
		t.Fatal("should be in editing mode")
	}
	for _, r := range "Initiative" {
		o, _ = o.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	o, _ = sendNamed(o, tea.KeyEnter)
	if got := o.Items(); !reflect.DeepEqual(got, []string{"Epic", "Initiative"}) {
		t.Fatalf("Items = %v, want [Epic Initiative]", got)
	}
}

func TestEpicTypesAddEmptyRejected(t *testing.T) {
	o := newEpicTypes([]string{"Epic"})
	o, _ = sendKey(o, "a")
	o, _ = sendNamed(o, tea.KeyEnter)
	if got := o.Items(); !reflect.DeepEqual(got, []string{"Epic"}) {
		t.Fatalf("empty add should be rejected, got %v", got)
	}
	if !o.Editing() {
		t.Fatal("should remain in editing mode on empty submit")
	}
}

func TestEpicTypesDelete(t *testing.T) {
	o := newEpicTypes([]string{"Epic", "Epic Feature"})
	o, _ = sendKey(o, "d")
	if got := o.Items(); !reflect.DeepEqual(got, []string{"Epic Feature"}) {
		t.Fatalf("Items after delete = %v", got)
	}
	if o.Cursor() != 0 {
		t.Fatalf("cursor should clamp to 0, got %d", o.Cursor())
	}
}

func TestEpicTypesEdit(t *testing.T) {
	o := newEpicTypes([]string{"Epic"})
	o, _ = sendKey(o, "e")
	if !o.Editing() {
		t.Fatal("should be editing")
	}
	o.SetInputForTest("Theme")
	o, _ = sendNamed(o, tea.KeyEnter)
	if got := o.Items(); !reflect.DeepEqual(got, []string{"Theme"}) {
		t.Fatalf("Items after edit = %v", got)
	}
}

func TestEpicTypesApplyOnEnter(t *testing.T) {
	o := newEpicTypes([]string{"Epic"})
	_, cmd := sendNamed(o, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("Enter must produce a cmd")
	}
	switch m := cmd().(type) {
	case EpicTypesAppliedMsg:
		if !reflect.DeepEqual(m.Items, []string{"Epic"}) {
			t.Fatalf("Applied.Items = %v", m.Items)
		}
	default:
		t.Fatalf("expected EpicTypesAppliedMsg, got %T", m)
	}
}

func TestEpicTypesCancelOnEsc(t *testing.T) {
	o := newEpicTypes([]string{"Epic"})
	_, cmd := sendNamed(o, tea.KeyEsc)
	if cmd == nil {
		t.Fatal("Esc must produce a cmd")
	}
	if _, ok := cmd().(EpicTypesCancelledMsg); !ok {
		t.Fatalf("expected EpicTypesCancelledMsg, got %T", cmd())
	}
}

func TestEpicTypesEscDuringEditCancelsInputOnly(t *testing.T) {
	o := newEpicTypes([]string{"Epic"})
	o, _ = sendKey(o, "a")
	o.SetInputForTest("X")
	o, cmd := sendNamed(o, tea.KeyEsc)
	if cmd != nil {
		t.Fatal("Esc during input mode should not emit a message")
	}
	if o.Editing() {
		t.Fatal("input mode should be exited")
	}
	if !o.Visible() {
		t.Fatal("overlay should remain visible")
	}
	if got := o.Items(); !reflect.DeepEqual(got, []string{"Epic"}) {
		t.Fatalf("Items modified during cancelled input: %v", got)
	}
}
