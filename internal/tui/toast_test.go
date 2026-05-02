package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/tui/styles"
	"github.com/billygate/ripjira/internal/tui/themes"
)

// fixedClock returns a clock function whose reading is whatever the caller
// most recently stored in *now. Tests advance simulated time by reassigning
// the variable.
func fixedClock(now *time.Time) func() time.Time {
	return func() time.Time { return *now }
}

func newStyles(t *testing.T) styles.Styles {
	t.Helper()
	p, err := themes.ByName("tokyonight")
	if err != nil {
		t.Fatalf("load tokyonight: %v", err)
	}
	return styles.New(p)
}

func TestToasts_PushSchedulesExpiry(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	tt := NewToasts().WithClock(fixedClock(&now))

	tt, cmd := tt.Push("hello", ToastInfo)
	if tt.Len() != 1 {
		t.Fatalf("Len after push = %d, want 1", tt.Len())
	}
	if cmd == nil {
		t.Fatal("Push returned nil cmd; want a tick")
	}
}

func TestToasts_ExpireAfterTTL(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	tt := NewToasts().WithClock(fixedClock(&now))

	tt, _ = tt.Push("hello", ToastInfo)

	now = now.Add(ToastTTL - time.Millisecond)
	tt = tt.Tick()
	if tt.Empty() {
		t.Fatal("toast expired before TTL elapsed")
	}

	now = now.Add(2 * time.Millisecond)
	tt = tt.Tick()
	if !tt.Empty() {
		t.Fatalf("toast did not expire after %v simulated time", ToastTTL)
	}
}

func TestToasts_MultipleExpireIndependently(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	tt := NewToasts().WithClock(fixedClock(&now))

	tt, _ = tt.Push("first", ToastInfo)
	now = now.Add(2 * time.Second)
	tt, _ = tt.Push("second", ToastError)

	now = now.Add(3 * time.Second)
	tt = tt.Tick()
	if tt.Len() != 1 {
		t.Fatalf("Len = %d after partial expiry, want 1", tt.Len())
	}
}

func TestToasts_EmptyView(t *testing.T) {
	tt := NewToasts()
	if v := tt.View(newStyles(t)); v != "" {
		t.Errorf("empty queue rendered %q, want empty string", v)
	}
}

func TestToasts_ViewIncludesText(t *testing.T) {
	tt := NewToasts()
	tt, _ = tt.Push("Comment added", ToastInfo)
	v := tt.View(newStyles(t))
	if !strings.Contains(stripANSI(v), "Comment added") {
		t.Errorf("View missing toast text; got %q", v)
	}
}

func TestSpinner_HiddenWhenIdle(t *testing.T) {
	sp := NewSpinner()
	if sp.Active() {
		t.Error("freshly constructed spinner should not be active")
	}
	if v := sp.View(); v != "" {
		t.Errorf("idle spinner rendered %q, want empty string", v)
	}
}

func TestSpinner_ShownWhenCounterPositive(t *testing.T) {
	sp := NewSpinner()
	sp, cmd := sp.Adjust(1)
	if !sp.Active() {
		t.Fatal("Adjust(+1) did not activate spinner")
	}
	if cmd == nil {
		t.Fatal("Adjust transition idle→active did not return a tick cmd")
	}
	if v := sp.View(); v == "" {
		t.Errorf("active spinner rendered empty string")
	}
}

func TestSpinner_DecrementHidesAndClampsAtZero(t *testing.T) {
	sp := NewSpinner()
	sp, _ = sp.Adjust(2)
	if sp.Counter() != 2 {
		t.Fatalf("Counter = %d, want 2", sp.Counter())
	}

	sp, cmd := sp.Adjust(-1)
	if !sp.Active() || cmd != nil {
		t.Errorf("after -1 active=%v cmd=%v; want active=true cmd=nil", sp.Active(), cmd)
	}

	sp, _ = sp.Adjust(-1)
	if sp.Active() {
		t.Error("spinner still active after counter dropped to zero")
	}
	if v := sp.View(); v != "" {
		t.Errorf("inactive spinner rendered %q", v)
	}

	sp, _ = sp.Adjust(-5)
	if sp.Counter() != 0 {
		t.Errorf("Counter = %d after stray decrement, want 0", sp.Counter())
	}
}

func TestSpinner_UpdateOnlyTicksWhileActive(t *testing.T) {
	sp := NewSpinner()
	sp, cmd := sp.Update(spinner.TickMsg{})
	if cmd != nil {
		t.Error("idle spinner produced a tick cmd from TickMsg")
	}

	sp, _ = sp.Adjust(1)
	sp, _ = sp.Update(spinner.TickMsg{})
	if !sp.Active() {
		t.Error("spinner deactivated after TickMsg")
	}

	_, cmd = sp.Update(tea.WindowSizeMsg{})
	if cmd != nil {
		t.Error("spinner.Update returned cmd for non-TickMsg")
	}
}

func TestModel_ToastMsgPushesAndRenders(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)

	updated, cmd := m.Update(ToastMsg{Text: "Saved", Level: ToastInfo})
	m = updated.(Model)
	if cmd == nil {
		t.Error("ToastMsg did not produce expiry cmd")
	}
	if m.Toasts().Empty() {
		t.Fatal("ToastMsg did not enqueue a toast")
	}
	if !strings.Contains(stripANSI(m.View()), "Saved") {
		t.Errorf("View did not render toast text\n%s", stripANSI(m.View()))
	}
}

func TestModel_ToastExpiresOnSimulatedTime(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	m := newTestModel(t).WithToastClock(fixedClock(&now))
	m, _ = sendSize(m, 80, 24)

	updated, _ := m.Update(ToastMsg{Text: "hello", Level: ToastInfo})
	m = updated.(Model)
	if m.Toasts().Empty() {
		t.Fatal("toast not enqueued")
	}

	now = now.Add(ToastTTL + time.Second)
	updated, _ = m.Update(toastExpireMsg{})
	m = updated.(Model)
	if !m.Toasts().Empty() {
		t.Error("expected toast to expire after simulated TTL")
	}
}

func TestModel_BackgroundActivityTogglesSpinner(t *testing.T) {
	m := newTestModel(t)
	m, _ = sendSize(m, 80, 24)

	if m.Spinner().Active() {
		t.Fatal("spinner active before any background activity")
	}

	updated, cmd := m.Update(BackgroundActivityMsg{Delta: 1})
	m = updated.(Model)
	if !m.Spinner().Active() {
		t.Error("spinner not active after Delta=+1")
	}
	if cmd == nil {
		t.Error("idle→active transition did not return spinner tick cmd")
	}

	withSpinner := stripANSI(m.View())
	updated, _ = m.Update(BackgroundActivityMsg{Delta: -1})
	withoutSpinner := stripANSI(updated.(Model).View())
	if withSpinner == withoutSpinner {
		t.Error("View identical with and without active spinner")
	}
}
