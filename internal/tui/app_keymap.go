package tui

import (
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
)

// QuitArmed reports whether the user has pressed Esc once on the main view
// and the confirmation window has not yet elapsed.
func (m Model) QuitArmed() bool {
	return !m.pendingQuitUntil.IsZero() && time.Now().Before(m.pendingQuitUntil)
}

// canArmQuit reports whether an Esc keypress on the main view should arm
// the quit confirmation. False when any overlay is visible OR when the
// list pane is in search-editing mode (Esc must remain a cancel there).
func (m Model) canArmQuit() bool {
	if m.help.Visible() || m.transition.Visible() || m.comment.Visible() ||
		m.assign.Visible() || m.create.Visible() || m.options.Visible() ||
		m.edit.Visible() || m.favorites.Visible() || m.link.Visible() ||
		m.linkRemove.Visible() || m.worklog.Visible() || m.worklogRemove.Visible() ||
		m.description.Visible() || m.priority.Visible() ||
		m.epicPicker.Visible() || m.structPicker.Visible() || m.scopeEditor.Visible() || m.topGo.Visible() ||
		m.created.Visible() {
		return false
	}
	if m.list.SearchEditing() || m.list.LocalFilterEditing() {
		return false
	}
	return true
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.help.Visible() {
		var cmd tea.Cmd
		m.help, cmd = m.help.Update(msg)
		return m, cmd
	}
	if m.transition.Visible() {
		var cmd tea.Cmd
		m.transition, cmd = m.transition.Update(msg)
		return m, cmd
	}
	if m.comment.Visible() {
		// Snapshot the in-progress body and key BEFORE the overlay sees the
		// keypress: if this very keystroke closes the overlay we want to
		// know what was being typed.
		prevKey, prevBody := m.comment.IssueKey(), m.comment.Value()
		submitting := msg.String() == "ctrl+s"
		var cmd tea.Cmd
		m.comment, cmd = m.comment.Update(msg)
		// On any close that wasn't a submit, persist what the user had so
		// far so it comes back on next open. Submits clear via the success
		// branch in handleCommentDone.
		if !m.comment.Visible() && !submitting && prevBody != "" {
			m.saveDraft(prevKey, prevBody)
		}
		return m, cmd
	}
	if m.assign.Visible() {
		var cmd tea.Cmd
		m.assign, cmd = m.assign.Update(msg)
		return m, cmd
	}
	if m.create.Visible() {
		var cmd tea.Cmd
		m.create, cmd = m.create.Update(msg)
		return m, cmd
	}
	if m.options.Visible() {
		var cmd tea.Cmd
		m.options, cmd = m.options.Update(msg)
		return m, cmd
	}
	if m.edit.Visible() {
		var cmd tea.Cmd
		m.edit, cmd = m.edit.Update(msg)
		return m, cmd
	}
	if m.favorites.Visible() {
		var cmd tea.Cmd
		m.favorites, cmd = m.favorites.Update(msg)
		return m, cmd
	}
	if m.link.Visible() {
		var cmd tea.Cmd
		m.link, cmd = m.link.Update(msg)
		return m, cmd
	}
	if m.linkRemove.Visible() {
		var cmd tea.Cmd
		m.linkRemove, cmd = m.linkRemove.Update(msg)
		return m, cmd
	}
	if m.worklog.Visible() {
		var cmd tea.Cmd
		m.worklog, cmd = m.worklog.Update(msg)
		return m, cmd
	}
	if m.worklogRemove.Visible() {
		var cmd tea.Cmd
		m.worklogRemove, cmd = m.worklogRemove.Update(msg)
		return m, cmd
	}
	if m.description.Visible() {
		var cmd tea.Cmd
		m.description, cmd = m.description.Update(msg)
		return m, cmd
	}
	if m.priority.Visible() {
		var cmd tea.Cmd
		m.priority, cmd = m.priority.Update(msg)
		return m, cmd
	}
	if m.epicPicker.Visible() {
		var cmd tea.Cmd
		m.epicPicker, cmd = m.epicPicker.Update(msg)
		return m, cmd
	}
	if m.structPicker.Visible() {
		var cmd tea.Cmd
		m.structPicker, cmd = m.structPicker.Update(msg)
		return m, cmd
	}
	if m.scopeEditor.Visible() {
		var cmd tea.Cmd
		m.scopeEditor, cmd = m.scopeEditor.Update(msg)
		return m, cmd
	}
	if m.created.Visible() {
		var cmd tea.Cmd
		m.created, cmd = m.created.Update(msg)
		return m, cmd
	}
	if m.topGo.Visible() {
		var cmd tea.Cmd
		m.topGo, cmd = m.topGo.Update(msg)
		return m, cmd
	}
	// While the list pane's search input is being edited, the input must
	// own the keypress — otherwise typing "n", "s", etc. would trigger
	// global hotkeys (open create, open status…) instead of going into
	// the textinput. The list's own Update handles Enter/Esc.
	if m.list.SearchEditing() || m.list.LocalFilterEditing() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}
	// Esc on a collapsed local filter clears it before any quit-arm logic
	// runs — feels like the natural undo.
	if msg.Type == tea.KeyEsc && m.list.LocalFilter() != "" {
		m.list.ClearLocalFilter()
		return m, nil
	}
	if msg.Type == tea.KeyEsc && m.canArmQuit() {
		if m.QuitArmed() {
			return m, tea.Quit
		}
		m.pendingQuitUntil = time.Now().Add(3 * time.Second)
		var toastCmd tea.Cmd
		m.toasts, toastCmd = m.toasts.Push(
			"Press Esc again to quit  (any other key cancels)",
			ToastInfo,
		)
		return m, toastCmd
	}
	// Any non-Esc key clears a pending arm; fall through to the regular
	// handler so the keypress still does its normal job.
	if msg.Type != tea.KeyEsc && m.QuitArmed() {
		m.pendingQuitUntil = time.Time{}
	}
	// Translate Cyrillic/Greek runes to their Latin physical-position
	// equivalents so global hotkeys ([, ], q, n, …) work regardless of
	// the active keyboard layout. Suppressed when an input is focused
	// (canArmQuit encodes that predicate already).
	if m.canArmQuit() {
		msg = translateLayout(msg)
	}
	switch {
	case key.Matches(msg, m.keymap.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keymap.Help):
		m.help = m.help.Show()
		return m, nil
	case key.Matches(msg, m.keymap.Status):
		return m.openTransitionOverlay()
	case key.Matches(msg, m.keymap.Comment):
		return m.openCommentOverlay()
	case key.Matches(msg, m.keymap.Assign):
		return m.openAssignOverlay()
	case key.Matches(msg, m.keymap.New):
		return m.openCreateOverlay()
	case key.Matches(msg, m.keymap.NewSubtask):
		return m.openCreateSubtaskOverlay()
	case key.Matches(msg, m.keymap.Browser):
		return m.openInBrowser()
	case key.Matches(msg, m.keymap.CopyKey):
		return m.copySelectedIssue(false)
	case key.Matches(msg, m.keymap.CopyURL):
		return m.copySelectedIssue(true)
	case key.Matches(msg, m.keymap.Refresh):
		return m.handleManualRefresh()
	case key.Matches(msg, m.keymap.CycleFocusForward):
		return m.cycleFocus(1), nil
	case key.Matches(msg, m.keymap.CycleFocusBackward):
		return m.cycleFocus(-1), nil
	case key.Matches(msg, m.keymap.NextTab):
		return m.handleViewSelected(m.nextView())
	case key.Matches(msg, m.keymap.PrevTab):
		return m.handleViewSelected(m.prevView())
	case key.Matches(msg, m.keymap.FocusLeft):
		mm, cmd := m.stepFocus(-1)
		return mm, cmd
	case key.Matches(msg, m.keymap.FocusRight):
		mm, cmd := m.stepFocus(1)
		return mm, cmd
	case key.Matches(msg, m.keymap.OpenSearch):
		return m.openSearch()
	case key.Matches(msg, m.keymap.OpenFilter):
		m.list.BeginLocalFilter()
		m.focus = FocusList
		return m, nil
	case key.Matches(msg, m.keymap.OpenFavorites):
		return m.openFavoritesOverlay()
	case key.Matches(msg, m.keymap.EditSummary):
		return m.openEditOverlay(overlays.EditSummary)
	case key.Matches(msg, m.keymap.EditPriority):
		return m.openPriorityPicker()
	case key.Matches(msg, m.keymap.EditLabels):
		return m.openEditOverlay(overlays.EditLabels)
	case key.Matches(msg, m.keymap.EditDueDate):
		return m.openEditOverlay(overlays.EditDueDate)
	case key.Matches(msg, m.keymap.EditDescription):
		return m.openDescriptionOverlay()
	case key.Matches(msg, m.keymap.EditEpic):
		return m.openEpicPicker()
	case key.Matches(msg, m.keymap.OpenTopGo):
		return m.openTopGo()
	case key.Matches(msg, m.keymap.OpenStructures):
		return m.openStructurePicker()
	case key.Matches(msg, m.keymap.EditStructures):
		return m.editStructuresYAML()
	case key.Matches(msg, m.keymap.NextSubView):
		return m.handleViewSelected(m.nextSubView())
	case key.Matches(msg, m.keymap.PrevSubView):
		return m.handleViewSelected(m.prevSubView())
	case key.Matches(msg, m.keymap.AddLink):
		return m.openLinkOverlay()
	case key.Matches(msg, m.keymap.RemoveLink):
		return m.openRemoveLinkOverlay()
	case key.Matches(msg, m.keymap.Watch):
		return m.dispatchWatch(true)
	case key.Matches(msg, m.keymap.Unwatch):
		return m.dispatchWatch(false)
	case key.Matches(msg, m.keymap.LogWork):
		return m.openWorklogOverlay()
	case key.Matches(msg, m.keymap.RemoveWorklog):
		return m.openRemoveWorklogOverlay()
	case key.Matches(msg, m.keymap.OpenOptions):
		cur := m.list.Strategy().Name()
		sortName := "priority"
		desc := false
		if s, d := m.list.Sort(); s != nil {
			sortName = s.Name()
			desc = d
		}
		m.options = m.options.Show(cur, sortName, desc)
		return m, nil
	}

	switch m.focus {
	case FocusList:
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		selCmd := m.syncDetailFromList()
		return m, tea.Batch(cmd, selCmd)
	case FocusDetail:
		var cmd tea.Cmd
		m.detail, cmd = m.detail.Update(msg)
		return m, cmd
	}
	return m, nil
}

// cycleFocus advances the focus by step (positive = forward) between
// FocusList and FocusDetail.
func (m Model) cycleFocus(step int) Model {
	order := []Focus{FocusList, FocusDetail}
	idx := 0
	for i, f := range order {
		if f == m.focus {
			idx = i
			break
		}
	}
	idx = (idx + step + len(order)) % len(order)
	m.focus = order[idx]
	return m
}

// stepFocus moves left (-1) or right (+1) one pane, clamping at the
// edges. The preview pane extends the cascade: from FocusDetail with an
// image attachment available, →  opens the preview pane (list collapses);
// from inside the preview pane, ←  closes it.
func (m Model) stepFocus(step int) (Model, tea.Cmd) {
	if m.preview.Active {
		if step < 0 {
			m.preview.Active = false
			m.preview.Attachment = jira.Attachment{}
			m.focus = FocusDetail
			return m, clearGraphicsCmd()
		}
		return m, nil
	}
	switch m.focus {
	case FocusList:
		if step > 0 {
			m.focus = FocusDetail
		}
	case FocusDetail:
		if step > 0 {
			if att := m.detail.FirstImageAttachment(); att != nil {
				m.preview.Active = true
				m.preview.Attachment = *att
				_, _, previewW, contentH := m.paneDims()
				// Reserve 80% of the available preview area so the
				// rendered image leaves a bit of breathing room on
				// every side; the remaining cells stay free for the
				// title row, the "← back" hint, and visual padding.
				cols := max((previewW-2)*4/5, 4)
				rows := max((contentH-3)*4/5, 4)
				return m, m.detail.LoadAttachmentPreview(*att, cols, rows)
			}
		} else {
			m.focus = FocusList
		}
	}
	return m, nil
}

// nextView returns the view immediately after m.view in the cyclic
// MyTasks → Watching → Search → MyTasks order.
// Search is intentionally excluded from the tab cycle — it is reachable
// only via the `/` hotkey and behaves as a transient mode rather than a
// tab.
func (m Model) nextView() panes.ViewKind {
	return m.cycleTopTab(+1)
}

// prevView is nextView's mirror.
func (m Model) prevView() panes.ViewKind {
	return m.cycleTopTab(-1)
}

// cycleTopTab advances the active top-level tab by step, returning the
// preferred sub-view of the new top: the persisted last sub-view if any,
// else the first sub.
func (m Model) cycleTopTab(step int) panes.ViewKind {
	tops := panes.AllTopTabs()
	cur := panes.TopGroup(m.view)
	idx := 0
	for i, t := range tops {
		if t == cur {
			idx = i
			break
		}
	}
	idx = (idx + step + len(tops)) % len(tops)
	target := tops[idx]
	if v, ok := m.lastSubView[target]; ok {
		return v
	}
	subs := panes.SubViews(target)
	if len(subs) == 0 {
		return panes.ViewMyTasks
	}
	return subs[0]
}

// nextSubView / prevSubView cycle within the active top tab's sub-views.
// Returns m.view unchanged when the top has a single sub.
func (m Model) nextSubView() panes.ViewKind { return m.cycleSubView(+1) }
func (m Model) prevSubView() panes.ViewKind { return m.cycleSubView(-1) }

func (m Model) cycleSubView(step int) panes.ViewKind {
	subs := panes.SubViews(panes.TopGroup(m.view))
	if len(subs) <= 1 {
		return m.view
	}
	idx := 0
	for i, v := range subs {
		if v == m.view {
			idx = i
			break
		}
	}
	return subs[(idx+step+len(subs))%len(subs)]
}
