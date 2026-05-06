package tui

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/state"
	"github.com/billygate/ripjira/internal/tui/grouping"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
)

// dispatchWatch fires a watch/unwatch network call for the current issue.
// Watch uses an empty accountID (Jira's "self" semantics); unwatch
// requires the explicit accountID from the model.
func (m Model) dispatchWatch(watching bool) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil || m.loader == nil {
		return m, nil
	}
	loader := m.loader
	key := issue.Key
	accountID := m.accountID
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		var err error
		if watching {
			err = loader.AddWatcher(context.Background(), key, "")
		} else {
			err = loader.RemoveWatcher(context.Background(), key, accountID)
		}
		return watchDoneMsg{IssueKey: key, Watching: watching, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleWatchDone toasts success/failure of an Add/RemoveWatcher call.
func (m Model) handleWatchDone(msg watchDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	verb := "Watching"
	if !msg.Watching {
		verb = "Unwatched"
	}
	if msg.Err != nil {
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  verb + " failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{Text: verb + " " + msg.IssueKey, Level: ToastInfo}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// handleWorklogDeletePicked dispatches DeleteWorklog with optimistic
// local removal. Stores a snapshot in the model so handleWorklogDeleteDone
// can revert on failure.
func (m Model) handleWorklogDeletePicked(msg overlays.WorklogDeletedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil || msg.WorklogID == "" {
		return m, nil
	}
	var prev jira.Worklog
	if issue := m.detail.Issue(); issue != nil && issue.Key == msg.IssueKey {
		for _, w := range issue.Worklogs {
			if w.ID == msg.WorklogID {
				prev = w
				break
			}
		}
	}
	m.detail.RemoveWorklogByID(msg.IssueKey, msg.WorklogID)
	m.pendingDeletedWorklog = prev

	loader := m.loader
	key, id := msg.IssueKey, msg.WorklogID
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.DeleteWorklog(context.Background(), key, id)
		return worklogDeletedDoneMsg{IssueKey: key, WorklogID: id, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleWorklogDeleteDone toasts and reverts on failure.
func (m Model) handleWorklogDeleteDone(msg worklogDeletedDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		if m.pendingDeletedWorklog.ID != "" {
			m.detail.AppendWorklog(msg.IssueKey, m.pendingDeletedWorklog)
		}
		m.pendingDeletedWorklog = jira.Worklog{}
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Remove worklog failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	m.pendingDeletedWorklog = jira.Worklog{}
	toast := func() tea.Msg {
		return ToastMsg{Text: "Worklog removed", Level: ToastInfo}
	}
	reload := m.reloadDetailIfMatches(msg.IssueKey)
	return m, tea.Batch(stopSpinner, toast, reload)
}

// handleWorklogSubmitted dispatches the AddWorklog network call. There is
// nothing optimistic to update locally — the issue's worklog list is not
// surfaced in the detail pane in this MVP.
func (m Model) handleWorklogSubmitted(msg overlays.WorklogSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	key, ts, comment := msg.IssueKey, msg.TimeSpent, msg.Comment
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.AddWorklog(context.Background(), key, ts, comment)
		return worklogDoneMsg{IssueKey: key, TimeSpent: ts, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleWorklogDone toasts success/failure of an AddWorklog call.
func (m Model) handleWorklogDone(msg worklogDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Worklog failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Logged " + msg.TimeSpent + " on " + msg.IssueKey,
			Level: ToastInfo,
		}
	}
	reload := m.reloadDetailIfMatches(msg.IssueKey)
	return m, tea.Batch(stopSpinner, toast, reload)
}

// handlePrioritySelected synthesises an EditSubmittedMsg for EditPriority
// so the existing optimistic-update + rollback logic handles the picker
// path uniformly with the text-input path.
func (m Model) handlePrioritySelected(msg overlays.PrioritySelectedMsg) (tea.Model, tea.Cmd) {
	return m.handleEditSubmitted(overlays.EditSubmittedMsg{
		IssueKey: msg.IssueKey,
		Field:    overlays.EditPriority,
		Value:    msg.Name,
	})
}

// reloadDetailIfMatches returns a Reload command for the detail pane when it
// currently shows key, otherwise nil. Called after mutation success messages
// so subsequent overlays (transitions list, comments list) see fresh data
// without flashing a "Loading…" placeholder. Pointer receiver because
// Detail.Reload mutates state (bumps token, replaces cancel func).
func (m *Model) reloadDetailIfMatches(key string) tea.Cmd {
	if key == "" {
		return nil
	}
	cur := m.detail.Issue()
	if cur == nil || cur.Key != key {
		return nil
	}
	return m.detail.Reload()
}

// projectKeyOf returns the project portion of a Jira issue key
// ("BILLING-123" → "BILLING"). Returns the input unchanged when no
// hyphen is present.
func projectKeyOf(issueKey string) string {
	if i := strings.IndexByte(issueKey, '-'); i > 0 {
		return issueKey[:i]
	}
	return issueKey
}

// searchEpicsCmd builds the tea.Cmd that calls SearchEpics for the
// project the issue belongs to.
func (m Model) searchEpicsCmd(issueKey string) tea.Cmd {
	loader := m.loader
	project := projectKeyOf(issueKey)
	types := append([]string(nil), m.epicTypes...)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		epics, err := loader.SearchEpics(ctx, project, types)
		return epicsLoadedMsg{IssueKey: issueKey, Epics: epics, Err: err}
	}
}

// handleEpicsLoaded consumes the SearchEpics result. Stale results (the
// user closed or moved on) are dropped.
func (m Model) handleEpicsLoaded(msg epicsLoadedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.epicPicker = m.epicPicker.Hide()
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Could not load epics: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, toast
	}
	if !m.epicPicker.Visible() || m.epicPicker.IssueKey() != msg.IssueKey {
		return m, nil
	}
	m.epicPicker = m.epicPicker.SetEpics(msg.Epics)
	return m, nil
}

// handleEpicPicked applies the picked epic optimistically and dispatches
// SetParent. On error the previous parent is restored via setParentDoneMsg.
func (m Model) handleEpicPicked(msg overlays.EpicPickedMsg) (tea.Model, tea.Cmd) {
	loaded := m.epicPicker.LoadedEpics()
	m.epicPicker = m.epicPicker.Hide()
	if m.loader == nil {
		return m, nil
	}

	var oldKey, oldSum string
	if issue := m.detail.Issue(); issue != nil && issue.Key == msg.IssueKey {
		oldKey = issue.ParentKey
		oldSum = issue.ParentSummary
	} else {
		for _, is := range m.list.Issues() {
			if is.Key == msg.IssueKey {
				oldKey = is.ParentKey
				oldSum = is.ParentSummary
				break
			}
		}
	}
	if oldKey == msg.ParentKey {
		return m, nil
	}

	newSum := ""
	for _, ep := range loaded {
		if ep.Key == msg.ParentKey {
			newSum = ep.Summary
			break
		}
	}

	m.list.UpdateIssueParent(msg.IssueKey, msg.ParentKey, newSum)
	m.detail.UpdateParent(msg.IssueKey, msg.ParentKey, newSum)

	loader := m.loader
	issueKey, parentKey := msg.IssueKey, msg.ParentKey
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := loader.SetParent(ctx, issueKey, parentKey)
		return setParentDoneMsg{
			IssueKey:     issueKey,
			OldParentKey: oldKey,
			OldParentSum: oldSum,
			NewParentKey: parentKey,
			Err:          err,
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleSetParentDone consumes the SetParent result, reverting the
// optimistic update on failure.
func (m Model) handleSetParentDone(msg setParentDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.list.UpdateIssueParent(msg.IssueKey, msg.OldParentKey, msg.OldParentSum)
		m.detail.UpdateParent(msg.IssueKey, msg.OldParentKey, msg.OldParentSum)
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Could not set epic: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	reload := m.reloadDetailIfMatches(msg.IssueKey)
	return m, tea.Batch(stopSpinner, reload)
}

// handleDescriptionSubmitted dispatches UpdateDescription with optimistic
// local update of the displayed markdown body. On error the previous
// body is restored.
func (m Model) handleDescriptionSubmitted(msg overlays.DescriptionSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	prev := issue.Description
	if msg.Body == prev {
		return m, nil
	}
	m.detail.UpdateDescription(msg.IssueKey, msg.Body)

	loader := m.loader
	key, body := msg.IssueKey, msg.Body
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.UpdateDescription(context.Background(), key, body)
		return descriptionDoneMsg{IssueKey: key, NewBody: body, PrevBody: prev, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleDescriptionDone consumes the UpdateDescription result.
func (m Model) handleDescriptionDone(msg descriptionDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.detail.UpdateDescription(msg.IssueKey, msg.PrevBody)
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Edit description failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{Text: "Updated description", Level: ToastInfo}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// handleLinkDeletePicked dispatches DeleteIssueLink with optimistic local
// removal — the link disappears from the detail pane immediately. On
// failure it is restored from the prior snapshot.
func (m Model) handleLinkDeletePicked(msg overlays.LinkDeletedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil || msg.LinkID == "" {
		return m, nil
	}
	// Snapshot for revert.
	var prev jira.IssueLink
	if issue := m.detail.Issue(); issue != nil && issue.Key == msg.IssueKey {
		for _, l := range issue.Links {
			if l.ID == msg.LinkID {
				prev = l
				break
			}
		}
	}
	m.detail.RemoveLink(msg.IssueKey, msg.OtherKey)

	loader := m.loader
	linkID := msg.LinkID
	owning := msg.IssueKey
	other := msg.OtherKey
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.DeleteLink(context.Background(), linkID)
		if err != nil {
			// Reattach a copy for revert via the result handler.
			return linkDeletedDoneMsg{
				IssueKey: owning,
				OtherKey: other,
				Err:      err,
			}
		}
		_ = prev
		return linkDeletedDoneMsg{IssueKey: owning, OtherKey: other}
	}
	// We can't capture the closure-mutated `prev` without a state field;
	// stash it on the model. Errors revert by re-appending what we knew.
	m.pendingDeletedLink = prev
	return m, tea.Batch(startSpinner, call)
}

// handleLinkDeleteDone toasts success/failure of a link deletion. On
// failure, re-adds the optimistically-removed link.
func (m Model) handleLinkDeleteDone(msg linkDeletedDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		if m.pendingDeletedLink.OtherKey != "" {
			m.detail.AppendLink(msg.IssueKey, m.pendingDeletedLink)
		}
		m.pendingDeletedLink = jira.IssueLink{}
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Remove link failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	m.pendingDeletedLink = jira.IssueLink{}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Removed link to " + msg.OtherKey,
			Level: ToastInfo,
		}
	}
	reload := m.reloadDetailIfMatches(msg.IssueKey)
	return m, tea.Batch(stopSpinner, toast, reload)
}

// handleLinkSubmitted dispatches the CreateIssueLink network call. On
// success the user gets an info toast and the detail pane gains a
// provisional link entry; on error a toast surfaces the message. The
// optimistic entry is partial — only OtherKey, TypeName and Outward are
// known locally. The next refresh fills in the missing summary/status.
func (m Model) handleLinkSubmitted(msg overlays.LinkSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	issue := m.detail.Issue()
	if issue != nil && issue.Key == msg.IssueKey {
		m.detail.AppendLink(msg.IssueKey, jira.IssueLink{
			Relation: strings.ToLower(msg.Type),
			TypeName: msg.Type,
			OtherKey: msg.TargetKey,
			Outward:  true,
		})
	}
	loader := m.loader
	typ, target := msg.Type, msg.TargetKey
	owning := msg.IssueKey
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.CreateLink(context.Background(), typ, owning, target)
		return linkDoneMsg{IssueKey: owning, Type: typ, TargetKey: target, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleLinkDone consumes the CreateIssueLink result. Removes the
// optimistic entry and surfaces an error toast on failure; on success
// shows a confirmation and triggers a detail refresh so the link gets
// its summary/status from Jira.
func (m Model) handleLinkDone(msg linkDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.detail.RemoveLink(msg.IssueKey, msg.TargetKey)
		toast := func() tea.Msg {
			return ToastMsg{
				Text:  "Link failed: " + msg.Err.Error(),
				Level: ToastError,
			}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Linked " + msg.IssueKey + " " + msg.Type + " " + msg.TargetKey,
			Level: ToastInfo,
		}
	}
	reload := m.reloadDetailIfMatches(msg.IssueKey)
	return m, tea.Batch(stopSpinner, toast, reload)
}

// handleFavoriteApplied switches to the Search view with the chosen JQL
// and dispatches a refresh. Reuses the same plumbing as the search input.
func (m Model) handleFavoriteApplied(msg overlays.FavoriteAppliedMsg) (tea.Model, tea.Cmd) {
	m.view = panes.ViewSearch
	m.searchQuery = msg.JQL
	m.list.SetSearchCollapsed(msg.JQL)
	m.list.SetStrategy(grouping.ByStatus{})
	m.detail.SetIssue(nil)
	m.selectedKey = ""
	m.list.Top()
	updated, cmd := m.dispatchListRefresh()
	return updated, cmd
}

// handleFavoriteSaved persists a new named favorite to state.json. The
// write happens in a goroutine via state.Mutate so the Update loop never
// blocks on disk I/O.
func (m Model) handleFavoriteSaved(msg overlays.FavoriteSavedMsg) (tea.Model, tea.Cmd) {
	if m.statePath == "" {
		return m, nil
	}
	path := m.statePath
	name, jql := msg.Name, msg.JQL
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			// Replace any existing entry with the same name so saving
			// twice with the same name updates rather than duplicates.
			for i := range s.Favorites {
				if s.Favorites[i].Name == name {
					s.Favorites[i].JQL = jql
					return
				}
			}
			s.Favorites = append(s.Favorites, state.Favorite{Name: name, JQL: jql})
		})
	}()
	toast := func() tea.Msg {
		return ToastMsg{Text: "Saved favorite: " + name, Level: ToastInfo}
	}
	return m, toast
}

// handleFavoriteDeleted persists the removal to state.json.
func (m Model) handleFavoriteDeleted(msg overlays.FavoriteDeletedMsg) (tea.Model, tea.Cmd) {
	if m.statePath == "" {
		return m, nil
	}
	path := m.statePath
	name := msg.Name
	go func() {
		_ = state.Mutate(path, func(s *state.State) {
			out := s.Favorites[:0]
			for _, f := range s.Favorites {
				if f.Name != name {
					out = append(out, f)
				}
			}
			s.Favorites = out
		})
	}()
	toast := func() tea.Msg {
		return ToastMsg{Text: "Deleted favorite: " + name, Level: ToastInfo}
	}
	return m, toast
}

// handleEditSubmitted applies an optimistic update for the chosen field
// and dispatches the UpdateFields network call. Empty values are rejected
// for fields that cannot meaningfully be empty (summary, priority); for
// labels and due date an empty value is a deliberate "clear" operation.
func (m Model) handleEditSubmitted(msg overlays.EditSubmittedMsg) (tea.Model, tea.Cmd) {
	if msg.Value == "" && (msg.Field == overlays.EditSummary || msg.Field == overlays.EditPriority) {
		toast := func() tea.Msg {
			return ToastMsg{Text: "Empty value, edit cancelled", Level: ToastInfo}
		}
		return m, toast
	}
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }

	switch msg.Field {
	case overlays.EditSummary:
		prev := issue.Summary
		if msg.Value == prev {
			return m, nil
		}
		m.list.UpdateIssueSummary(msg.IssueKey, msg.Value)
		m.detail.UpdateSummary(msg.IssueKey, msg.Value)
		key := msg.IssueKey
		newVal := msg.Value
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"summary": newVal,
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditSummary, PrevSummary: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)

	case overlays.EditPriority:
		prev := issue.Priority
		if msg.Value == prev.Name {
			return m, nil
		}
		next := jira.Priority{Name: msg.Value}
		m.list.UpdateIssuePriority(msg.IssueKey, next)
		m.detail.UpdatePriority(msg.IssueKey, next)
		key := msg.IssueKey
		newName := msg.Value
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"priority": map[string]any{"name": newName},
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditPriority, PrevPriority: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)

	case overlays.EditLabels:
		prev := append([]string(nil), issue.Labels...)
		next := splitLabels(msg.Value)
		if labelsEqual(prev, next) {
			return m, nil
		}
		m.list.UpdateIssueLabels(msg.IssueKey, next)
		m.detail.UpdateLabels(msg.IssueKey, next)
		key := msg.IssueKey
		// Pass a fresh copy to the goroutine so the closure does not race
		// with the optimistic update we just queued on the main model.
		nextCopy := append([]string(nil), next...)
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"labels": nextCopy,
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditLabels, PrevLabels: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)

	case overlays.EditDueDate:
		prev := issue.DueDate
		if msg.Value == prev {
			return m, nil
		}
		m.list.UpdateIssueDueDate(msg.IssueKey, msg.Value)
		m.detail.UpdateDueDate(msg.IssueKey, msg.Value)
		key := msg.IssueKey
		newVal := msg.Value
		// Jira accepts "" via JSON null to clear the field; sending an empty
		// string would be rejected as an invalid date.
		var wire any = newVal
		if newVal == "" {
			wire = nil
		}
		call := func() tea.Msg {
			err := loader.UpdateFields(context.Background(), key, map[string]any{
				"duedate": wire,
			})
			return editDoneMsg{IssueKey: key, Field: overlays.EditDueDate, PrevDueDate: prev, Err: err}
		}
		return m, tea.Batch(startSpinner, call)
	}
	return m, nil
}

// splitLabels turns a comma-separated user input into the de-duplicated,
// trimmed slice Jira expects. Empty segments and pure-whitespace ones are
// dropped so users can type "a, b ,c," without surprises.
func splitLabels(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	seen := map[string]bool{}
	out := []string{}
	for raw := range strings.SplitSeq(s, ",") {
		l := strings.TrimSpace(raw)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// labelsEqual reports whether two label slices contain the same elements
// in the same order.
func labelsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// handleEditDone consumes the UpdateFields result. On error the optimistic
// update is reverted and the user is told via toast.
func (m Model) handleEditDone(msg editDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err == nil {
		toast := func() tea.Msg {
			return ToastMsg{Text: "Updated " + msg.Field.FieldName(), Level: ToastInfo}
		}
		reload := m.reloadDetailIfMatches(msg.IssueKey)
		return m, tea.Batch(stopSpinner, toast, reload)
	}
	switch msg.Field {
	case overlays.EditSummary:
		m.list.UpdateIssueSummary(msg.IssueKey, msg.PrevSummary)
		m.detail.UpdateSummary(msg.IssueKey, msg.PrevSummary)
	case overlays.EditPriority:
		m.list.UpdateIssuePriority(msg.IssueKey, msg.PrevPriority)
		m.detail.UpdatePriority(msg.IssueKey, msg.PrevPriority)
	case overlays.EditLabels:
		m.list.UpdateIssueLabels(msg.IssueKey, msg.PrevLabels)
		m.detail.UpdateLabels(msg.IssueKey, msg.PrevLabels)
	case overlays.EditDueDate:
		m.list.UpdateIssueDueDate(msg.IssueKey, msg.PrevDueDate)
		m.detail.UpdateDueDate(msg.IssueKey, msg.PrevDueDate)
	}
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Edit " + msg.Field.FieldName() + " failed: " + msg.Err.Error(),
			Level: ToastError,
		}
	}
	return m, tea.Batch(stopSpinner, toast)
}

// handleAssignSearchRequest dispatches a SearchUsers call for the query the
// overlay just debounced. Stale requests (the user kept typing) and
// requests arriving after the overlay closed are silently dropped.
func (m Model) handleAssignSearchRequest(msg overlays.AssignSearchRequestMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	if !m.assign.Visible() || msg.Token != m.assign.Token() {
		return m, nil
	}
	loader := m.loader
	query := msg.Query
	token := msg.Token
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		users, err := loader.SearchUsers(context.Background(), query)
		return assignSearchDoneMsg{
			Result: overlays.AssignResultsMsg{Query: query, Token: token, Users: users, Err: err},
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleAssignSelected applies an optimistic assignee change to the list +
// detail panes, then dispatches the AssignIssue network call. The spinner
// counter is bumped while the call is in flight.
func (m Model) handleAssignSelected(msg overlays.AssignSelectedMsg) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	next := msg.User
	m.list.UpdateIssueAssignee(msg.IssueKey, &next)
	m.detail.UpdateAssignee(msg.IssueKey, &next)

	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	issueKey := msg.IssueKey
	accountID := msg.User.AccountID
	prev := msg.PrevAssignee
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.AssignIssue(context.Background(), issueKey, accountID)
		return assignDoneMsg{
			IssueKey:     issueKey,
			NewAssignee:  next,
			PrevAssignee: prev,
			Err:          err,
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleAssignDone consumes the AssignIssue result. On error the assignee
// is rolled back; on success a confirmation toast is shown.
func (m Model) handleAssignDone(msg assignDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		m.list.UpdateIssueAssignee(msg.IssueKey, msg.PrevAssignee)
		m.detail.UpdateAssignee(msg.IssueKey, msg.PrevAssignee)
		toast := func() tea.Msg {
			return ToastMsg{Text: "Assign failed: " + msg.Err.Error(), Level: ToastError}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	toast := func() tea.Msg {
		return ToastMsg{Text: "Assigned to " + msg.NewAssignee.DisplayName, Level: ToastInfo}
	}
	reload := m.reloadDetailIfMatches(msg.IssueKey)
	return m, tea.Batch(stopSpinner, toast, reload)
}

// handleCommentSubmitted dispatches the AddComment network call when the
// overlay confirms a draft. With no loader wired (e.g. tests asserting only
// overlay behaviour), the message is dropped on the floor.
func (m Model) handleCommentSubmitted(msg overlays.CommentSubmittedMsg) (tea.Model, tea.Cmd) {
	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	issueKey := msg.IssueKey
	body := msg.Body
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.AddComment(context.Background(), issueKey, body)
		return commentDoneMsg{IssueKey: issueKey, Body: body, Err: err}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleCommentDone consumes the AddComment result. On success the body is
// appended to the detail pane (when the user has not navigated away) and an
// info toast is shown; on error the user is told via toast and nothing is
// appended.
func (m Model) handleCommentDone(msg commentDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err != nil {
		toast := func() tea.Msg {
			return ToastMsg{Text: "Comment failed: " + msg.Err.Error(), Level: ToastError}
		}
		return m, tea.Batch(stopSpinner, toast)
	}
	m.detail.AppendComment(msg.IssueKey, jira.Comment{
		Body:    msg.Body,
		Created: time.Now(),
	})
	m.clearDraft(msg.IssueKey)
	toast := func() tea.Msg { return ToastMsg{Text: "Comment added", Level: ToastInfo} }
	reload := m.reloadDetailIfMatches(msg.IssueKey)
	return m, tea.Batch(stopSpinner, toast, reload)
}

// handleTransitionSelected applies an optimistic status change to the list
// and detail panes, then dispatches the DoTransition network call. The
// spinner counter is bumped while the call is in flight; the corresponding
// decrement happens in handleTransitionDone.
func (m Model) handleTransitionSelected(msg overlays.TransitionSelectedMsg) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil || issue.Key != msg.IssueKey {
		return m, nil
	}
	prev := issue.Status
	next := msg.Transition.To
	m.list.UpdateIssueStatus(msg.IssueKey, next)
	m.detail.UpdateStatus(msg.IssueKey, next)

	if m.loader == nil {
		return m, nil
	}
	loader := m.loader
	key := msg.IssueKey
	id := msg.Transition.ID
	startSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: 1} }
	call := func() tea.Msg {
		err := loader.DoTransition(context.Background(), key, id)
		return transitionDoneMsg{
			IssueKey:   key,
			PrevStatus: prev,
			NewStatus:  next,
			Err:        err,
		}
	}
	return m, tea.Batch(startSpinner, call)
}

// handleTransitionDone consumes the DoTransition result. On error the
// optimistic status is reverted and the user is told via toast; on success
// nothing else needs to happen because the optimistic state is now real.
func (m Model) handleTransitionDone(msg transitionDoneMsg) (tea.Model, tea.Cmd) {
	stopSpinner := func() tea.Msg { return BackgroundActivityMsg{Delta: -1} }
	if msg.Err == nil {
		reload := m.reloadDetailIfMatches(msg.IssueKey)
		return m, tea.Batch(stopSpinner, reload)
	}
	m.list.UpdateIssueStatus(msg.IssueKey, msg.PrevStatus)
	m.detail.UpdateStatus(msg.IssueKey, msg.PrevStatus)
	toast := func() tea.Msg {
		return ToastMsg{
			Text:  "Transition failed: " + msg.Err.Error(),
			Level: ToastError,
		}
	}
	return m, tea.Batch(stopSpinner, toast)
}
