package panes

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/gfx"
	"github.com/billygate/ripjira/internal/tui/styles"
)

// renderImage is the function used to encode raw image bytes as an inline
// graphics escape sequence. It is a variable so tests can stub it without
// pulling in the real rasterm output (which depends on terminal env vars).
var renderImage = func(data []byte, cols, rows int) (string, int, int, bool) {
	return gfx.Render(data, cols, rows)
}

// maxAttachmentPreviewBytes caps how many bytes per attachment we will
// download for inline preview. Larger files keep their textual placeholder
// only — the goal is "nice to have", not a full file viewer.
const maxAttachmentPreviewBytes = 4 << 20

// Loader is the data source for the Detail pane. Implementations are typically
// backed by *jira.Client; tests use a stub. Each method takes a context so
// in-flight requests can be cancelled when the user moves to another issue.
type Loader interface {
	LoadIssue(ctx context.Context, key string) (jira.Issue, error)
	LoadComments(ctx context.Context, key string) ([]jira.Comment, error)
	LoadTransitions(ctx context.Context, key string) ([]jira.Transition, error)
	// LoadAttachment fetches the bytes of a single attachment by its absolute
	// `content` URL. Implementations cap the body at a reasonable size and
	// return the server-reported MIME type alongside.
	LoadAttachment(ctx context.Context, contentURL string) ([]byte, string, error)
}

// IssueLoadedMsg is the result of a LoadIssue call started by the Detail pane.
// Token is the load generation at dispatch time; the receiving pane discards
// the message when its current generation has moved on (stale load).
type IssueLoadedMsg struct {
	Key   string
	Token int64
	Issue jira.Issue
	Err   error
}

// CommentsLoadedMsg is the result of a LoadComments call.
type CommentsLoadedMsg struct {
	Key      string
	Token    int64
	Comments []jira.Comment
	Err      error
}

// TransitionsLoadedMsg is the result of a LoadTransitions call.
type TransitionsLoadedMsg struct {
	Key         string
	Token       int64
	Transitions []jira.Transition
	Err         error
}

// AttachmentPreviewMsg carries the rendered inline-graphics escape for a
// single image attachment. Escape is empty when rendering failed or the
// terminal lacks graphics support — the pane then keeps its textual
// placeholder. Cols / Rows are the cell footprint the renderer asked the
// terminal to use, so the host layout can centre the image without
// having to inspect the escape.
type AttachmentPreviewMsg struct {
	Key          string
	Token        int64
	AttachmentID string
	Escape       string
	Cols         int
	Rows         int
	Err          error
}

// AttachmentPreview is the cached render of a single attachment, indexed
// by Attachment.ID inside Detail. Empty Escape means the load completed
// but the host terminal could not render the image (e.g. unsupported
// MIME type) — callers should keep the textual placeholder.
type AttachmentPreview struct {
	Escape string
	Cols   int
	Rows   int
}

// sectionState tracks the lifecycle of one async section of the detail view.
type sectionState int

const (
	sectionLoading sectionState = iota
	sectionLoaded
	sectionError
)

// Detail is the right-hand pane that renders the currently selected issue's
// description and comments. Transitions are still loaded in the background
// so the status-change overlay (`s`) can open instantly with the list ready,
// but they are not rendered in this pane — the overlay owns that UI. Each
// rendered section loads asynchronously: a skeleton placeholder is shown
// until the result arrives. Stale results from a previous selection are
// filtered out via a monotonic load token, and the previous load's context
// is cancelled when the selection changes.
type Detail struct {
	styles styles.Styles
	loader Loader
	vp     viewport.Model
	width  int
	height int

	issue       *jira.Issue
	description string
	comments    []jira.Comment
	transitions []jira.Transition

	detailState     sectionState
	commentState    sectionState
	transitionState sectionState
	detailErr       error
	commentErr      error
	transitionErr   error

	token  int64
	ctx    context.Context
	cancel context.CancelFunc

	// commentRowOffsets stores the row offset (0-based) of each rendered
	// comment's first line in the viewport content, populated during
	// refreshContent. JumpToComment reads this to scroll the viewport.
	commentRowOffsets []int

	// attachmentPreviews maps Attachment.ID → rendered inline-graphics
	// escape sequence and its declared cell footprint. Populated as
	// AttachmentPreviewMsg messages arrive from the loader. Empty entries
	// mean the load failed or the terminal does not support graphics —
	// refreshContent then keeps the textual placeholder.
	attachmentPreviews map[string]AttachmentPreview
}

// NewDetail constructs a Detail pane bound to styles + loader. width/height
// size the inner viewport.
func NewDetail(s styles.Styles, l Loader, width, height int) Detail {
	d := Detail{
		styles: s,
		loader: l,
		vp:     viewport.New(width, height),
		width:  width,
		height: height,
	}
	d.refreshContent()
	return d
}

// SetSize forwards width/height to the inner viewport.
func (d *Detail) SetSize(w, h int) {
	d.width = w
	d.height = h
	d.vp.Width = w
	d.vp.Height = h
}

// Issue returns the issue currently displayed, or nil if none.
func (d Detail) Issue() *jira.Issue { return d.issue }

// Transitions returns the workflow transitions loaded for the current issue.
// Returns nil before they have arrived. Mutating actions like the transition
// overlay read this.
func (d Detail) Transitions() []jira.Transition { return d.transitions }

// AppendComment adds c to the comment list of the displayed issue when its
// key matches. Returns true when applied. The comment section is forced to
// the loaded state so the new entry shows even if the original load is
// still in flight or errored. Used by the comment overlay's success path.
func (d *Detail) AppendComment(key string, c jira.Comment) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.comments = append(d.comments, c)
	d.commentState = sectionLoaded
	d.commentErr = nil
	d.refreshContent()
	return true
}

// UpdateStatus rewrites the status of the displayed issue when it matches
// key, then re-renders. Returns true when applied. Used by optimistic
// updates so the detail pane reflects a transition before the network call
// resolves.
func (d *Detail) UpdateStatus(key string, status jira.Status) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.issue.Status = status
	d.refreshContent()
	return true
}

// UpdateSummary rewrites the summary of the displayed issue when it
// matches key, then re-renders. Used by optimistic edits.
func (d *Detail) UpdateSummary(key, summary string) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.issue.Summary = summary
	d.refreshContent()
	return true
}

// UpdatePriority rewrites the priority of the displayed issue when it
// matches key, then re-renders. Used by optimistic edits.
func (d *Detail) UpdatePriority(key string, priority jira.Priority) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.issue.Priority = priority
	d.refreshContent()
	return true
}

// UpdateDescription rewrites the description body of the displayed issue
// when it matches key, then re-renders. Used by optimistic edits via the
// description overlay.
func (d *Detail) UpdateDescription(key, body string) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.issue.Description = body
	d.description = body
	d.refreshContent()
	return true
}

// UpdateParent rewrites the parent epic key/summary of the displayed issue
// when it matches key, then re-renders. Used by optimistic epic-link edits.
func (d *Detail) UpdateParent(key, parentKey, parentSummary string) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.issue.ParentKey = parentKey
	d.issue.ParentSummary = parentSummary
	d.refreshContent()
	return true
}

// RemoveWorklogByID drops the worklog with the given ID from the
// displayed issue. Used by optimistic delete-worklog.
func (d *Detail) RemoveWorklogByID(owningKey, worklogID string) bool {
	if d.issue == nil || d.issue.Key != owningKey {
		return false
	}
	for i, w := range d.issue.Worklogs {
		if w.ID == worklogID {
			d.issue.Worklogs = append(d.issue.Worklogs[:i], d.issue.Worklogs[i+1:]...)
			d.refreshContent()
			return true
		}
	}
	return false
}

// AppendWorklog adds a Worklog to the displayed issue. Used so the
// add-worklog flow can show the freshly-logged entry without waiting on
// a refetch.
func (d *Detail) AppendWorklog(owningKey string, w jira.Worklog) bool {
	if d.issue == nil || d.issue.Key != owningKey {
		return false
	}
	d.issue.Worklogs = append(d.issue.Worklogs, w)
	d.refreshContent()
	return true
}

// AppendLink adds an IssueLink to the displayed issue when its key matches
// owningKey. Returns true on apply. Used by optimistic add-link.
func (d *Detail) AppendLink(owningKey string, link jira.IssueLink) bool {
	if d.issue == nil || d.issue.Key != owningKey {
		return false
	}
	d.issue.Links = append(d.issue.Links, link)
	d.refreshContent()
	return true
}

// RemoveLink drops the first link to otherKey from the displayed issue
// when its key matches owningKey. Used to revert a failed optimistic add.
func (d *Detail) RemoveLink(owningKey, otherKey string) bool {
	if d.issue == nil || d.issue.Key != owningKey {
		return false
	}
	for i, l := range d.issue.Links {
		if l.OtherKey == otherKey {
			d.issue.Links = append(d.issue.Links[:i], d.issue.Links[i+1:]...)
			d.refreshContent()
			return true
		}
	}
	return false
}

// UpdateLabels rewrites the labels of the displayed issue when it matches
// key, then re-renders. Used by optimistic edits.
func (d *Detail) UpdateLabels(key string, labels []string) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.issue.Labels = append([]string(nil), labels...)
	d.refreshContent()
	return true
}

// UpdateDueDate rewrites the due date of the displayed issue when it
// matches key, then re-renders. Used by optimistic edits.
func (d *Detail) UpdateDueDate(key, dueDate string) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	d.issue.DueDate = dueDate
	d.refreshContent()
	return true
}

// AppendSubtask adds a SubtaskRef to the displayed issue when its key
// matches parentKey. Returns true on apply. Used by optimistic updates
// from the create wizard's subtask-mode submit path.
func (d *Detail) AppendSubtask(parentKey string, sub jira.SubtaskRef) bool {
	if d.issue == nil || d.issue.Key != parentKey {
		return false
	}
	d.issue.Subtasks = append(d.issue.Subtasks, sub)
	d.refreshContent()
	return true
}

// UpdateAssignee rewrites the assignee of the displayed issue when it
// matches key, then re-renders. Returns true when applied. Used by
// optimistic updates from the assign overlay; a nil assignee renders as
// "unassigned".
func (d *Detail) UpdateAssignee(key string, assignee *jira.User) bool {
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	if assignee == nil {
		d.issue.Assignee = nil
	} else {
		clone := *assignee
		d.issue.Assignee = &clone
	}
	d.refreshContent()
	return true
}

// Token returns the current load generation. Tests use this to construct
// fresh and stale messages.
func (d Detail) Token() int64 { return d.token }

// SetIssue switches the pane to a new issue, cancelling any in-flight loads
// and dispatching fresh ones. A nil issue clears the pane. Returns the
// tea.Cmd that fans out the three load operations; pass it to the runtime
// (or call directly in tests).
func (d *Detail) SetIssue(issue *jira.Issue) tea.Cmd {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.token++
	if issue == nil {
		d.issue = nil
	} else {
		clone := *issue
		d.issue = &clone
	}
	d.description = ""
	d.comments = nil
	d.transitions = nil
	d.attachmentPreviews = nil
	d.detailState = sectionLoading
	d.commentState = sectionLoading
	d.transitionState = sectionLoading
	d.detailErr = nil
	d.commentErr = nil
	d.transitionErr = nil

	d.vp.GotoTop()

	if d.issue == nil {
		d.refreshContent()
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	d.ctx = ctx
	d.cancel = cancel
	key := d.issue.Key
	token := d.token
	loader := d.loader
	d.refreshContent()
	return tea.Batch(
		func() tea.Msg {
			is, err := loader.LoadIssue(ctx, key)
			return IssueLoadedMsg{Key: key, Token: token, Issue: is, Err: err}
		},
		func() tea.Msg {
			cs, err := loader.LoadComments(ctx, key)
			return CommentsLoadedMsg{Key: key, Token: token, Comments: cs, Err: err}
		},
		func() tea.Msg {
			ts, err := loader.LoadTransitions(ctx, key)
			return TransitionsLoadedMsg{Key: key, Token: token, Transitions: ts, Err: err}
		},
	)
}

// JumpToComment scrolls the viewport to the n-th rendered comment
// (1-based). Out-of-range n is a no-op.
func (d *Detail) JumpToComment(n int) {
	if n < 1 || n > len(d.commentRowOffsets) {
		return
	}
	d.vp.SetYOffset(d.commentRowOffsets[n-1])
}

// debugAttachmentf writes a single line to $RIPJIRA_GFX_LOG when set.
// Used for diagnosing terminal-graphics rendering on user machines without
// a debugger. Silent when the env var is unset, which is the default.
func debugAttachmentf(format string, args ...any) {
	path := os.Getenv("RIPJIRA_GFX_LOG")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, format+"\n", args...)
}

// humanSize renders a byte count in the smallest unit that produces a
// short, human-readable string. Sizes <1 KiB are shown in bytes; larger
// values use binary units (KiB, MiB, GiB).
func humanSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(k*k*k))
	}
}

// HasImagePreview reports whether the currently displayed issue has at
// least one image attachment small enough to preview, and the host
// terminal supports inline graphics. Used to gate the right-arrow that
// opens the preview pane.
func (d Detail) HasImagePreview() bool {
	if d.issue == nil || gfx.Detect() == gfx.None {
		return false
	}
	for _, a := range d.issue.Attachments {
		if !strings.HasPrefix(a.MimeType, "image/") || a.Content == "" {
			continue
		}
		if a.Size > 0 && a.Size > maxAttachmentPreviewBytes {
			continue
		}
		return true
	}
	return false
}

// FirstImageAttachment returns the first image attachment of the current
// issue eligible for preview, or nil when none qualifies.
func (d Detail) FirstImageAttachment() *jira.Attachment {
	if d.issue == nil {
		return nil
	}
	for i := range d.issue.Attachments {
		a := &d.issue.Attachments[i]
		if !strings.HasPrefix(a.MimeType, "image/") || a.Content == "" {
			continue
		}
		if a.Size > 0 && a.Size > maxAttachmentPreviewBytes {
			continue
		}
		return a
	}
	return nil
}

// LoadAttachmentPreview returns a tea.Cmd that fetches a single image
// attachment by ID and emits AttachmentPreviewMsg with the rendered
// inline-graphics escape. Used by the "open preview pane" action; until
// this is called we never download attachment bytes, so terminals that
// dislike large escapes (or users who don't care) pay no cost.
func (d *Detail) LoadAttachmentPreview(att jira.Attachment, cols, rows int) tea.Cmd {
	if d.issue == nil || gfx.Detect() == gfx.None {
		return nil
	}
	if cached, ok := d.attachmentPreviews[att.ID]; ok && cached.Escape != "" {
		return nil
	}
	loader := d.loader
	ctx := d.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	key := d.issue.Key
	token := d.token
	return func() tea.Msg {
		data, _, err := loader.LoadAttachment(ctx, att.Content)
		if err != nil {
			debugAttachmentf("load %s: %v", att.Filename, err)
			return AttachmentPreviewMsg{Key: key, Token: token, AttachmentID: att.ID, Err: err}
		}
		esc, fitCols, fitRows, ok := renderImage(data, cols, rows)
		if !ok {
			debugAttachmentf("render %s: protocol=%v bytes=%d ok=false",
				att.Filename, gfx.Detect(), len(data))
			return AttachmentPreviewMsg{Key: key, Token: token, AttachmentID: att.ID}
		}
		debugAttachmentf("render %s: protocol=%v bytes=%d esc=%d budget=%dx%d fit=%dx%d",
			att.Filename, gfx.Detect(), len(data), len(esc), cols, rows, fitCols, fitRows)
		return AttachmentPreviewMsg{
			Key:          key,
			Token:        token,
			AttachmentID: att.ID,
			Escape:       esc,
			Cols:         fitCols,
			Rows:         fitRows,
		}
	}
}

// AttachmentPreview returns the rendered inline-graphics escape stored
// for the given attachment ID, with its declared cell footprint, or a
// zero-value AttachmentPreview when none has been loaded yet. The app
// uses this to paint the preview pane without coupling the pane to
// Detail's internal map.
func (d Detail) AttachmentPreview(id string) AttachmentPreview {
	return d.attachmentPreviews[id]
}

// Init implements tea.Model.
func (d Detail) Init() tea.Cmd { return nil }

// Update routes load messages to the appropriate section, filtering stale
// ones via token comparison, and forwards everything else to the viewport.
func (d Detail) Update(msg tea.Msg) (Detail, tea.Cmd) {
	switch m := msg.(type) {
	case tea.KeyMsg:
		switch m.String() {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			d.JumpToComment(int(m.Runes[0] - '0'))
			return d, nil
		}
	case IssueLoadedMsg:
		if !d.matches(m.Key, m.Token) {
			return d, nil
		}
		if m.Err != nil {
			d.detailState = sectionError
			d.detailErr = m.Err
		} else {
			d.detailState = sectionLoaded
			d.description = m.Issue.Description
			merged := mergeIssue(d.issue, m.Issue)
			d.issue = &merged
		}
		d.refreshContent()
		return d, nil
	case AttachmentPreviewMsg:
		if !d.matches(m.Key, m.Token) {
			return d, nil
		}
		if m.Err == nil && m.Escape != "" {
			if d.attachmentPreviews == nil {
				d.attachmentPreviews = map[string]AttachmentPreview{}
			}
			d.attachmentPreviews[m.AttachmentID] = AttachmentPreview{
				Escape: m.Escape,
				Cols:   m.Cols,
				Rows:   m.Rows,
			}
			d.refreshContent()
		}
		return d, nil
	case CommentsLoadedMsg:
		if !d.matches(m.Key, m.Token) {
			return d, nil
		}
		if m.Err != nil {
			d.commentState = sectionError
			d.commentErr = m.Err
		} else {
			d.commentState = sectionLoaded
			d.comments = m.Comments
		}
		d.refreshContent()
		return d, nil
	case TransitionsLoadedMsg:
		if !d.matches(m.Key, m.Token) {
			return d, nil
		}
		if m.Err != nil {
			d.transitionState = sectionError
			d.transitionErr = m.Err
		} else {
			d.transitionState = sectionLoaded
			d.transitions = m.Transitions
		}
		d.refreshContent()
		return d, nil
	}
	var cmd tea.Cmd
	d.vp, cmd = d.vp.Update(msg)
	return d, cmd
}

// View renders the pane's viewport.
func (d Detail) View() string { return d.vp.View() }

// matches reports whether a load message corresponds to the current issue +
// generation. A stale message arriving after a SetIssue is silently dropped.
func (d Detail) matches(key string, token int64) bool {
	if token != d.token {
		return false
	}
	if d.issue == nil || d.issue.Key != key {
		return false
	}
	return true
}

// mergeIssue keeps any header info already shown (from the list) when the full
// issue arrives, but lets the full issue's data win where present.
func mergeIssue(prev *jira.Issue, full jira.Issue) jira.Issue {
	if prev == nil {
		return full
	}
	out := full
	if out.Summary == "" {
		out.Summary = prev.Summary
	}
	if out.Status.Name == "" {
		out.Status = prev.Status
	}
	if out.Priority.Name == "" {
		out.Priority = prev.Priority
	}
	if out.Assignee == nil {
		out.Assignee = prev.Assignee
	}
	if out.URL == "" {
		out.URL = prev.URL
	}
	return out
}

// refreshContent rebuilds the viewport content from current state.
func (d *Detail) refreshContent() {
	d.commentRowOffsets = nil
	if d.issue == nil {
		d.vp.SetContent(d.styles.Muted.Render("No issue selected."))
		return
	}
	var b strings.Builder
	b.WriteString(d.styles.PaneTitle.Render(d.issue.Key))
	if d.issue.Priority.Name != "" {
		b.WriteString("  ")
		b.WriteString(d.styles.Priority(d.issue.Priority.Name).Render("● " + d.issue.Priority.Name))
	}
	if d.HasImagePreview() {
		b.WriteString(" ")
		b.WriteString(d.styles.PreviewBadge.Render("PREVIEW"))
	}
	b.WriteString("\n")
	b.WriteString(d.issue.Summary)
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "Status:    %s\n", d.issue.Status.Name)
	if d.issue.Assignee != nil {
		fmt.Fprintf(&b, "Assignee:  %s\n", d.issue.Assignee.DisplayName)
	} else {
		b.WriteString("Assignee:  unassigned\n")
	}
	if d.issue.ParentKey != "" {
		if d.issue.ParentSummary != "" {
			fmt.Fprintf(&b, "Epic:      %s  %s\n", d.issue.ParentKey, d.issue.ParentSummary)
		} else {
			fmt.Fprintf(&b, "Epic:      %s\n", d.issue.ParentKey)
		}
	}
	if len(d.issue.Labels) > 0 {
		fmt.Fprintf(&b, "Labels:    %s\n", strings.Join(d.issue.Labels, ", "))
	}
	if d.issue.DueDate != "" {
		fmt.Fprintf(&b, "Due:       %s\n", d.issue.DueDate)
	}

	b.WriteString("\n")
	b.WriteString(d.styles.SectionHeader.Render("── Description ──"))
	b.WriteString("\n")
	switch d.detailState {
	case sectionLoading:
		b.WriteString(d.styles.Muted.Render("Loading description…"))
	case sectionError:
		b.WriteString(d.styles.Error.Render("Error: " + d.detailErr.Error()))
	case sectionLoaded:
		if strings.TrimSpace(d.description) == "" {
			b.WriteString(d.styles.Muted.Render("(no description)"))
		} else {
			b.WriteString(renderMarkdown(d.description, d.width))
		}
	}

	b.WriteString("\n\n")
	b.WriteString(d.styles.SectionHeader.Render(
		fmt.Sprintf("── Subtasks (%d) ──", len(d.issue.Subtasks))))
	b.WriteString("\n")
	if len(d.issue.Subtasks) == 0 {
		b.WriteString(d.styles.Muted.Render("(no subtasks — press S to create one)"))
	} else {
		for i, s := range d.issue.Subtasks {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%-12s  %-12s  %s",
				s.Key,
				truncate(s.Status.Name, 12),
				truncate(s.Summary, 60))
		}
	}

	b.WriteString("\n\n")
	b.WriteString(d.styles.SectionHeader.Render(
		fmt.Sprintf("── Worklogs (%d) ──", len(d.issue.Worklogs))))
	b.WriteString("\n")
	if len(d.issue.Worklogs) == 0 {
		b.WriteString(d.styles.Muted.Render("(no worklogs — press t to log time)"))
	} else {
		for i, w := range d.issue.Worklogs {
			if i > 0 {
				b.WriteString("\n")
			}
			author := "unknown"
			if w.Author != nil {
				author = w.Author.DisplayName
			}
			when := ""
			if !w.Started.IsZero() {
				when = w.Started.Format("2006-01-02")
			}
			fmt.Fprintf(&b, "%-10s  %-12s  %s",
				truncate(w.TimeSpent, 10),
				truncate(when, 12),
				truncate(author, 40))
		}
	}

	b.WriteString("\n\n")
	b.WriteString(d.styles.SectionHeader.Render(
		fmt.Sprintf("── Links (%d) ──", len(d.issue.Links))))
	b.WriteString("\n")
	if len(d.issue.Links) == 0 {
		b.WriteString(d.styles.Muted.Render("(no links — press + to add one)"))
	} else {
		for i, l := range d.issue.Links {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%s %-12s  %-12s  %s",
				d.styles.Muted.Render(truncate(l.Relation, 16)),
				l.OtherKey,
				truncate(l.Status.Name, 12),
				truncate(l.Summary, 60))
		}
	}

	b.WriteString("\n\n")
	b.WriteString(d.styles.SectionHeader.Render(
		fmt.Sprintf("── Attachments (%d) ──", len(d.issue.Attachments))))
	b.WriteString("\n")
	if len(d.issue.Attachments) == 0 {
		b.WriteString(d.styles.Muted.Render("(no attachments)"))
	} else {
		gfxOn := gfx.Detect() != gfx.None
		hasImage := false
		for i, a := range d.issue.Attachments {
			if i > 0 {
				b.WriteString("\n")
			}
			icon := "📎"
			previewable := strings.HasPrefix(a.MimeType, "image/") &&
				a.Content != "" &&
				(a.Size <= 0 || a.Size <= maxAttachmentPreviewBytes)
			if previewable {
				hasImage = true
				if gfxOn {
					icon = "🖼"
				}
			}
			fmt.Fprintf(&b, "%s %s  %s  %s",
				icon,
				truncate(a.Filename, 40),
				humanSize(a.Size),
				d.styles.Muted.Render(a.MimeType))
		}
		if hasImage && gfxOn {
			b.WriteString("\n")
			b.WriteString(d.styles.Muted.Render("→ open image preview"))
		}
	}

	b.WriteString("\n\n")
	commentCount := len(d.comments)
	b.WriteString(d.styles.SectionHeader.Render(fmt.Sprintf("── Comments (%d) ──", commentCount)))
	b.WriteString("\n")
	switch d.commentState {
	case sectionLoading:
		b.WriteString(d.styles.Muted.Render("Loading comments…"))
	case sectionError:
		b.WriteString(d.styles.Error.Render("Error: " + d.commentErr.Error()))
	case sectionLoaded:
		if commentCount == 0 {
			b.WriteString(d.styles.Muted.Render("(no comments)"))
		} else {
			d.commentRowOffsets = make([]int, 0, commentCount)
			for i, c := range d.comments {
				if i > 0 {
					b.WriteString("\n\n")
				}
				d.commentRowOffsets = append(d.commentRowOffsets,
					strings.Count(b.String(), "\n"))
				stamp := ""
				if !c.Created.IsZero() {
					stamp = c.Created.Format("2006-01-02 15:04")
				}
				fmt.Fprintf(&b, "%s — %s\n%s",
					c.Author.DisplayName, stamp, renderMarkdown(c.Body, d.width))
			}
		}
	}

	d.vp.SetContent(b.String())
}
