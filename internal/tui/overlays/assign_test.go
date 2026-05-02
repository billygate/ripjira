package overlays

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
)

func sampleUsers() []jira.User {
	return []jira.User{
		{AccountID: "u1", DisplayName: "Anna Andersson", Email: "anna@example.com"},
		{AccountID: "u2", DisplayName: "Sandra Wright", Email: "sandra@example.com"},
		{AccountID: "u3", DisplayName: "Ben Carter", Email: "ben@example.com"},
	}
}

func TestAssign_HiddenByDefault(t *testing.T) {
	a := NewAssign(closeBinding(), 0)
	if a.Visible() {
		t.Error("Assign should start hidden")
	}
	if got := a.View(newStyles(t)); got != "" {
		t.Errorf("hidden View should be empty, got %q", got)
	}
}

func TestAssign_ShowAndHide(t *testing.T) {
	prev := jira.User{AccountID: "u0", DisplayName: "Old"}
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", &prev)
	if !a.Visible() {
		t.Fatal("Show did not flip visible")
	}
	if a.IssueKey() != "PROJ-1" {
		t.Errorf("IssueKey = %q, want PROJ-1", a.IssueKey())
	}
	a = a.Hide()
	if a.Visible() {
		t.Fatal("Hide did not flip visible")
	}
	if a.IssueKey() != "" {
		t.Errorf("Hide should clear IssueKey, got %q", a.IssueKey())
	}
}

func TestAssign_ShortQueryShowsNoSearch(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if _, ok := findSearchRequestMsg(execCmd(cmd)); ok {
		t.Errorf("short query scheduled a search request")
	}
	if got := len(a.Results()); got != 0 {
		t.Errorf("results for short query = %d, want 0", got)
	}
	if a.Loading() {
		t.Error("short query should not show loading")
	}
}

func TestAssign_TypingSchedulesSearchRequest(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	a, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if cmd == nil {
		t.Fatal("typing 2nd char should schedule a debounced search")
	}
	if !a.Loading() {
		t.Error("Loading should be true while debounce is in flight")
	}
	req, ok := findSearchRequestMsg(execCmd(cmd))
	if !ok {
		t.Fatalf("scheduled cmd did not contain AssignSearchRequestMsg")
	}
	if req.Query != "an" {
		t.Errorf("search query = %q, want 'an'", req.Query)
	}
	if req.Token != a.Token() {
		t.Errorf("search token = %d, want %d", req.Token, a.Token())
	}
}

// execCmd safely calls a tea.Cmd and returns the resulting message.
func execCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// findSearchRequestMsg unwraps tea.BatchMsg and reports whether any leaf
// produces an AssignSearchRequestMsg. Lets cache/debounce tests stay
// agnostic to whether textinput.Update added a blink cmd to the batch.
func findSearchRequestMsg(msg tea.Msg) (AssignSearchRequestMsg, bool) {
	switch v := msg.(type) {
	case AssignSearchRequestMsg:
		return v, true
	case tea.BatchMsg:
		for _, c := range v {
			if c == nil {
				continue
			}
			if r, ok := findSearchRequestMsg(c()); ok {
				return r, true
			}
		}
	}
	return AssignSearchRequestMsg{}, false
}

func TestAssign_ResultsPopulateAndRender(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})

	a, _ = a.Update(AssignResultsMsg{
		Query: "an", Token: a.Token(), Users: sampleUsers()[:2],
	})
	if a.Loading() {
		t.Error("Loading should clear when results arrive")
	}
	if got := len(a.Results()); got != 2 {
		t.Errorf("results len = %d, want 2", got)
	}

	view := stripANSI(a.View(newStyles(t)))
	for _, want := range []string{"Anna Andersson", "Sandra Wright"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q\n%s", want, view)
		}
	}
}

func TestAssign_StaleResultsIgnored(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})
	staleToken := a.Token() - 1

	a, _ = a.Update(AssignResultsMsg{Query: "stale", Token: staleToken, Users: sampleUsers()})
	if got := len(a.Results()); got != 0 {
		t.Errorf("stale results applied: %+v", a.Results())
	}
}

func TestAssign_ResultsErrorSurfaces(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})

	netErr := errors.New("boom")
	a, _ = a.Update(AssignResultsMsg{Query: "an", Token: a.Token(), Err: netErr})
	if a.Err() == nil || !strings.Contains(a.Err().Error(), "boom") {
		t.Errorf("err = %v, want to contain 'boom'", a.Err())
	}
	view := stripANSI(a.View(newStyles(t)))
	if !strings.Contains(view, "boom") {
		t.Errorf("view missing error message\n%s", view)
	}
}

func TestAssign_CursorMovement(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})
	a, _ = a.Update(AssignResultsMsg{Query: "an", Token: a.Token(), Users: sampleUsers()[:2]})

	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if a.Cursor() != 1 {
		t.Errorf("cursor after down = %d, want 1", a.Cursor())
	}
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyDown})
	if a.Cursor() != 1 {
		t.Errorf("cursor clamped at end = %d, want 1", a.Cursor())
	}
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyUp})
	if a.Cursor() != 0 {
		t.Errorf("cursor after up = %d, want 0", a.Cursor())
	}
}

func TestAssign_EnterPublishesSelection(t *testing.T) {
	prev := jira.User{AccountID: "u0", DisplayName: "Old"}
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", &prev)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})
	a, _ = a.Update(AssignResultsMsg{Query: "an", Token: a.Token(), Users: sampleUsers()[:2]})
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyDown})

	hidden, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if hidden.Visible() {
		t.Error("enter should hide overlay")
	}
	if cmd == nil {
		t.Fatal("enter should return a cmd")
	}
	sel, ok := cmd().(AssignSelectedMsg)
	if !ok {
		t.Fatalf("cmd produced %T, want AssignSelectedMsg", cmd())
	}
	if sel.IssueKey != "PROJ-1" {
		t.Errorf("IssueKey = %q, want PROJ-1", sel.IssueKey)
	}
	if sel.User.AccountID != "u2" {
		t.Errorf("selected user AccountID = %q, want u2", sel.User.AccountID)
	}
	if sel.PrevAssignee == nil || sel.PrevAssignee.AccountID != "u0" {
		t.Errorf("PrevAssignee = %+v, want {u0, Old}", sel.PrevAssignee)
	}
}

func TestAssign_EnterEmptyResultsIsNoop(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})
	updated, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !updated.Visible() {
		t.Error("enter on empty results should not hide")
	}
	if cmd != nil {
		t.Errorf("enter on empty results returned cmd: %v", cmd)
	}
}

func TestAssign_EscClosesOverlay(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if updated.Visible() {
		t.Error("esc should hide overlay")
	}
}

func TestAssign_CacheServesRepeatedQueries(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})
	a, _ = a.Update(AssignResultsMsg{
		Query: "an", Token: a.Token(), Users: sampleUsers()[:2],
	})

	// backspace + retype same prefix → result should come from cache without
	// scheduling a new debounced search
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := a.Value(); got != "a" {
		t.Fatalf("after backspace value = %q, want 'a'", got)
	}
	a, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if _, ok := findSearchRequestMsg(execCmd(cmd)); ok {
		t.Errorf("repeated query should hit cache, not schedule a search request")
	}
	if a.Loading() {
		t.Error("cache hit should not flip Loading on")
	}
	if got := len(a.Results()); got != 2 {
		t.Errorf("cache hit results len = %d, want 2", got)
	}
}

func TestAssign_PrefixCacheNarrowsLocally(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})
	a, _ = a.Update(AssignResultsMsg{
		Query: "an", Token: a.Token(), Users: sampleUsers()[:2],
	})

	// type "dra" — narrowing the cached "an" set without a new dispatch.
	for _, r := range "dra" {
		a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	a, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'.'}})
	if _, ok := findSearchRequestMsg(execCmd(cmd)); ok {
		t.Errorf("prefix-cache hit should not schedule a search request")
	}
	// Only "Sandra Wright" matches "andra." (no candidate's name/email
	// contains the literal punctuation, so we backspace it back out).
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := len(a.Results()); got != 1 {
		t.Fatalf("filtered results len = %d, want 1: %+v", got, a.Results())
	}
	if got := a.Results()[0].DisplayName; got != "Sandra Wright" {
		t.Errorf("filtered result = %q, want 'Sandra Wright'", got)
	}
}

func TestAssign_ShortQueryClearsResults(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-1", nil)
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a', 'n'}})
	a, _ = a.Update(AssignResultsMsg{
		Query: "an", Token: a.Token(), Users: sampleUsers()[:2],
	})
	if len(a.Results()) == 0 {
		t.Fatal("setup: results should be populated")
	}
	a, _ = a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := len(a.Results()); got != 0 {
		t.Errorf("after backspacing below min len, results = %d, want 0", got)
	}
}

func TestAssign_HiddenUpdateNoop(t *testing.T) {
	a := NewAssign(closeBinding(), 0)
	updated, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if updated.Visible() {
		t.Error("hidden overlay should stay hidden")
	}
	if cmd != nil {
		t.Errorf("hidden update returned cmd: %v", cmd)
	}
}

func TestAssign_RendersIssueKey(t *testing.T) {
	a, _ := NewAssign(closeBinding(), 0).Show("PROJ-42", nil)
	view := stripANSI(a.View(newStyles(t)))
	for _, want := range []string{"PROJ-42", "enter", "select", "Type at least"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q\n%s", want, view)
		}
	}
}
