package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/tui/styles"
)

// ToastTTL is how long a toast remains visible after being pushed.
const ToastTTL = 4 * time.Second

// ToastLevel selects the visual style applied to a toast.
type ToastLevel int

// Toast levels.
const (
	ToastInfo ToastLevel = iota
	ToastError
)

// ToastMsg is published by any component that wants to surface a transient
// notification in the status bar. The root model captures it, pushes it onto
// the toast queue, and schedules expiry.
type ToastMsg struct {
	Text  string
	Level ToastLevel
}

// toastExpireMsg is an internal tick that prompts the toast queue to drop
// entries whose TTL has passed. It is not exported because only the toast
// machinery itself should care about it.
type toastExpireMsg struct{}

type toastEntry struct {
	text    string
	level   ToastLevel
	expires time.Time
}

// Toasts is a tiny, time-bounded queue of notifications rendered above the
// hint bar. The clock is injectable so tests can advance simulated time
// without sleeping.
type Toasts struct {
	now     func() time.Time
	entries []toastEntry
}

// NewToasts returns an empty queue using the real wall clock.
func NewToasts() Toasts {
	return Toasts{now: time.Now}
}

// WithClock returns a copy of t that consults the given clock function.
func (t Toasts) WithClock(now func() time.Time) Toasts {
	t.now = now
	return t
}

// Push appends a new entry and returns a tick command that fires once the
// entry's TTL elapses. Multiple pushes schedule multiple ticks; each tick
// drops only entries that have actually expired, so duplicates are harmless.
func (t Toasts) Push(text string, level ToastLevel) (Toasts, tea.Cmd) {
	t.entries = append(t.entries, toastEntry{
		text:    text,
		level:   level,
		expires: t.now().Add(ToastTTL),
	})
	cmd := tea.Tick(ToastTTL, func(time.Time) tea.Msg { return toastExpireMsg{} })
	return t, cmd
}

// Tick removes entries whose deadline is at or before the current clock
// reading. Safe to call when empty.
func (t Toasts) Tick() Toasts {
	if len(t.entries) == 0 {
		return t
	}
	now := t.now()
	kept := make([]toastEntry, 0, len(t.entries))
	for _, e := range t.entries {
		if e.expires.After(now) {
			kept = append(kept, e)
		}
	}
	t.entries = kept
	return t
}

// Empty reports whether any toasts are currently visible.
func (t Toasts) Empty() bool { return len(t.entries) == 0 }

// Len returns the number of visible toasts.
func (t Toasts) Len() int { return len(t.entries) }

// View renders the queue as a stack of styled lines. Returns "" when empty.
func (t Toasts) View(s styles.Styles) string {
	if len(t.entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(t.entries))
	for _, e := range t.entries {
		st := s.Toast
		if e.level == ToastError {
			st = s.Error
		}
		lines = append(lines, st.Render(e.text))
	}
	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// Spinner couples a bubbles/spinner.Model with a counter of in-flight
// background operations. The spinner only animates and only renders when
// the counter is positive.
type Spinner struct {
	model   spinner.Model
	counter int
}

// NewSpinner constructs a Spinner using the dot variant.
func NewSpinner() Spinner {
	m := spinner.New()
	m.Spinner = spinner.Dot
	return Spinner{model: m}
}

// Active reports whether at least one background operation is in flight.
func (s Spinner) Active() bool { return s.counter > 0 }

// Counter returns the current in-flight count (mostly for tests).
func (s Spinner) Counter() int { return s.counter }

// Adjust applies a delta to the in-flight counter. The counter is clamped at
// zero so a stray decrement cannot leave the spinner stuck. When the spinner
// transitions from idle to active, the returned cmd starts the animation.
func (s Spinner) Adjust(delta int) (Spinner, tea.Cmd) {
	wasActive := s.Active()
	s.counter += delta
	if s.counter < 0 {
		s.counter = 0
	}
	if !wasActive && s.Active() {
		return s, s.model.Tick
	}
	return s, nil
}

// Update forwards spinner.TickMsg to the underlying model only while active,
// so ticks that arrive after the counter drops to zero quietly stop the
// animation chain.
func (s Spinner) Update(msg tea.Msg) (Spinner, tea.Cmd) {
	if _, ok := msg.(spinner.TickMsg); !ok {
		return s, nil
	}
	if !s.Active() {
		return s, nil
	}
	var cmd tea.Cmd
	s.model, cmd = s.model.Update(msg)
	return s, cmd
}

// View returns the current spinner frame, or "" when inactive.
func (s Spinner) View() string {
	if !s.Active() {
		return ""
	}
	return s.model.View()
}
