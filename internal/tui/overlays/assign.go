package overlays

import (
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// DefaultAssignDebounce is the typing-pause window before a search request
// is dispatched, per the design spec ("250ms debounce").
const DefaultAssignDebounce = 250 * time.Millisecond

// AssignMinQueryLen is the minimum query length before a search fires.
const AssignMinQueryLen = 2

// AssignSearchRequestMsg is published when the debounce window for the
// current query expires. The root model handles it by dispatching a
// SearchUsers call and routing the result back as AssignResultsMsg. The
// model should compare Token against the overlay's current Token() and
// ignore stale requests (the user kept typing after the timer was armed).
type AssignSearchRequestMsg struct {
	Query string
	Token int64
}

// AssignResultsMsg carries SearchUsers results back to the overlay. Token
// matches the AssignSearchRequestMsg that produced the call so stale results
// can be ignored when the user has typed more characters since.
type AssignResultsMsg struct {
	Query string
	Token int64
	Users []jira.User
	Err   error
}

// AssignSelectedMsg is published when the user picks a candidate. The root
// model handles it by applying an optimistic assignee change and dispatching
// the AssignIssue network call; PrevAssignee is held so the change can be
// rolled back on failure.
type AssignSelectedMsg struct {
	IssueKey     string
	User         jira.User
	PrevAssignee *jira.User
}

// Assign is the `a` overlay: a textinput plus an async-loaded list of user
// candidates. Typing debounces a SearchUsers dispatch; results are cached by
// query (and by prefix so superstrings can filter locally).
type Assign struct {
	visible      bool
	issueKey     string
	prevAssignee *jira.User

	input    textinput.Model
	cursor   int
	results  []jira.User
	loading  bool
	err      error
	token    int64
	cache    map[string][]jira.User
	debounce time.Duration

	closeBinding key.Binding
}

// NewAssign constructs a hidden Assign overlay. closeKey is the key that
// hides the overlay (typically `esc`). debounce is the typing-pause window;
// pass 0 to disable debouncing (useful in tests).
func NewAssign(closeKey key.Binding, debounce time.Duration) Assign {
	ti := textinput.New()
	ti.Placeholder = "Type to search…"
	ti.Prompt = "› "
	ti.CharLimit = 64
	ti.Width = 40
	return Assign{
		input:        ti,
		closeBinding: closeKey,
		debounce:     debounce,
		cache:        map[string][]jira.User{},
	}
}

// Visible reports whether the overlay is currently shown.
func (a Assign) Visible() bool { return a.visible }

// IssueKey returns the issue key the overlay was opened for.
func (a Assign) IssueKey() string { return a.issueKey }

// Cursor returns the index of the highlighted candidate.
func (a Assign) Cursor() int { return a.cursor }

// Results returns the current candidate list (mostly for tests).
func (a Assign) Results() []jira.User { return a.results }

// Value returns the textinput's current query.
func (a Assign) Value() string { return a.input.Value() }

// Loading reports whether a search is in flight.
func (a Assign) Loading() bool { return a.loading }

// Err returns the last search error, if any.
func (a Assign) Err() error { return a.err }

// Token returns the current debounce/result generation. Tests use this to
// fabricate AssignResultsMsg / assignDebounceMsg with a matching token.
func (a Assign) Token() int64 { return a.token }

// Show binds a to issueKey + previous assignee with an empty query and
// focuses the textinput. The returned cmd starts the cursor blink.
func (a Assign) Show(issueKey string, prevAssignee *jira.User) (Assign, tea.Cmd) {
	a.visible = true
	a.issueKey = issueKey
	if prevAssignee != nil {
		clone := *prevAssignee
		a.prevAssignee = &clone
	} else {
		a.prevAssignee = nil
	}
	a.input.Reset()
	a.results = nil
	a.cursor = 0
	a.loading = false
	a.err = nil
	a.token++
	cmd := a.input.Focus()
	return a, cmd
}

// Hide returns a copy of a with the overlay closed and per-open state
// cleared. The query cache survives across Hide/Show within a session.
func (a Assign) Hide() Assign {
	a.visible = false
	a.issueKey = ""
	a.prevAssignee = nil
	a.input.Reset()
	a.input.Blur()
	a.results = nil
	a.cursor = 0
	a.loading = false
	a.err = nil
	return a
}

// Update consumes input while the overlay is visible. Up/Down/Ctrl-N/Ctrl-P
// move the cursor through results, Enter publishes a selection, the close
// key hides the overlay, and any other key is forwarded to the textinput
// (with the debounce timer rearmed when the value changes).
func (a Assign) Update(msg tea.Msg) (Assign, tea.Cmd) {
	if !a.visible {
		return a, nil
	}
	switch m := msg.(type) {
	case AssignResultsMsg:
		if m.Token != a.token {
			return a, nil
		}
		a.loading = false
		if m.Err != nil {
			a.err = m.Err
			a.results = nil
			a.cursor = 0
			return a, nil
		}
		a.cache[m.Query] = append([]jira.User(nil), m.Users...)
		a.results = m.Users
		a.cursor = 0
		a.err = nil
		return a, nil
	case tea.KeyMsg:
		if key.Matches(m, a.closeBinding) {
			return a.Hide(), nil
		}
		switch m.String() {
		case "up", "ctrl+p":
			if a.cursor > 0 {
				a.cursor--
			}
			return a, nil
		case "down", "ctrl+n":
			if a.cursor < len(a.results)-1 {
				a.cursor++
			}
			return a, nil
		case "enter":
			if len(a.results) == 0 {
				return a, nil
			}
			sel := a.results[a.cursor]
			issueKey := a.issueKey
			prev := a.prevAssignee
			hidden := a.Hide()
			return hidden, func() tea.Msg {
				return AssignSelectedMsg{
					IssueKey:     issueKey,
					User:         sel,
					PrevAssignee: prev,
				}
			}
		}
		old := a.input.Value()
		var cmd tea.Cmd
		a.input, cmd = a.input.Update(m)
		if a.input.Value() == old {
			return a, cmd
		}
		return a.afterValueChange(cmd)
	}
	var cmd tea.Cmd
	a.input, cmd = a.input.Update(msg)
	return a, cmd
}

// afterValueChange recomputes results state in response to a textinput edit.
// It bumps the token (invalidating any in-flight debounce/result), serves
// from cache when possible, and otherwise schedules a debounced search.
func (a Assign) afterValueChange(extra tea.Cmd) (Assign, tea.Cmd) {
	a.token++
	q := strings.TrimSpace(a.input.Value())
	a.err = nil
	a.cursor = 0
	if len([]rune(q)) < AssignMinQueryLen {
		a.results = nil
		a.loading = false
		return a, extra
	}
	if users, ok := a.cache[q]; ok {
		a.results = users
		a.loading = false
		return a, extra
	}
	if users, ok := a.longestPrefixHit(q); ok {
		a.results = filterUsers(users, q)
		a.loading = false
		return a, extra
	}
	a.results = nil
	a.loading = true
	token := a.token
	debounce := a.debounce
	emit := func() tea.Msg {
		return AssignSearchRequestMsg{Query: q, Token: token}
	}
	var schedule tea.Cmd
	if debounce <= 0 {
		schedule = emit
	} else {
		schedule = tea.Tick(debounce, func(time.Time) tea.Msg { return emit() })
	}
	if extra == nil {
		return a, schedule
	}
	return a, tea.Batch(extra, schedule)
}

// longestPrefixHit returns the cached result set for the longest prefix of
// q present in the cache; ok=false when no prefix is cached.
func (a Assign) longestPrefixHit(q string) ([]jira.User, bool) {
	qr := []rune(q)
	for i := len(qr) - 1; i >= AssignMinQueryLen; i-- {
		prefix := string(qr[:i])
		if users, ok := a.cache[prefix]; ok {
			return users, true
		}
	}
	return nil, false
}

// filterUsers returns the entries of users whose display name or email
// contain q (case-insensitive). Used for client-side narrowing when a
// shorter prefix has already been fetched.
func filterUsers(users []jira.User, q string) []jira.User {
	needle := strings.ToLower(q)
	out := make([]jira.User, 0, len(users))
	for _, u := range users {
		if strings.Contains(strings.ToLower(u.DisplayName), needle) ||
			strings.Contains(strings.ToLower(u.Email), needle) {
			out = append(out, u)
		}
	}
	return out
}

// View renders the overlay. Returns "" when hidden.
func (a Assign) View(s styles.Styles) string {
	if !a.visible {
		return ""
	}
	titleText := "Assign"
	if a.issueKey != "" {
		titleText = "Assign " + a.issueKey
	}
	title := s.OverlayTitle.Render(titleText)

	var body string
	switch {
	case a.err != nil:
		body = s.Error.Render("Error: " + a.err.Error())
	case len([]rune(strings.TrimSpace(a.input.Value()))) < AssignMinQueryLen:
		body = s.Muted.Render("Type at least 2 characters…")
	case a.loading && len(a.results) == 0:
		body = s.Muted.Render("Searching…")
	case len(a.results) == 0:
		body = s.Muted.Render("(no matches)")
	default:
		rows := make([]string, 0, len(a.results))
		for i, u := range a.results {
			label := u.DisplayName
			if u.Email != "" {
				label += "  " + u.Email
			}
			if i == a.cursor {
				label = s.ListItemSelected.Render(label)
			} else {
				label = s.ListItem.Render(label)
			}
			rows = append(rows, label)
		}
		body = lipgloss.JoinVertical(lipgloss.Left, rows...)
	}

	hint := s.Muted.Render(
		"enter select    " + a.closeBinding.Help().Key + " " + a.closeBinding.Help().Desc,
	)
	parts := []string{title, "", a.input.View(), "", body, "", hint}
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	return s.OverlayBorder.Render(inner)
}
