package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/structure"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
)

// openTopGo pops the "Go to" overlay listing every top-level tab. The
// active top is pre-selected; Enter switches to its persisted last sub.
func (m Model) openTopGo() (tea.Model, tea.Cmd) {
	tops := panes.AllTopTabs()
	entries := make([]overlays.TopGoEntry, 0, len(tops))
	for _, t := range tops {
		entries = append(entries, overlays.TopGoEntry{Label: t.String(), ID: int(t)})
	}
	m.topGo = m.topGo.Show(entries, int(panes.TopGroup(m.view)))
	return m, nil
}

// openStructurePicker pops the picker overlay populated with the active
// project's built-ins + user structures. No-op when defaultProject is unset.
func (m Model) openStructurePicker() (tea.Model, tea.Cmd) {
	pk := m.defaultProject
	if pk == "" {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: set default project to use this picker", Level: ToastInfo}
		}
	}
	all, err := m.loadStructuresFor(pk)
	if err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: " + err.Error(), Level: ToastError}
		}
	}
	entries := make([]overlays.StructureEntry, 0, len(all))
	for _, s := range all {
		entries = append(entries, overlays.StructureEntry{
			ID: s.ID, Name: s.Name,
			ReadOnly: s.IsReadOnly(),
			Builtin:  structure.IsBuiltinID(s.ID),
		})
	}
	selID := m.currentStructID[pk]
	if selID == "" {
		selID = structure.BuiltinDefaultID
	}
	m.structPicker = m.structPicker.Show(entries, selID)
	return m, nil
}

// openTransitionOverlay opens the `s` overlay for the currently selected
// issue. When no issue is selected (group header or empty list) it is a
// no-op. Transitions come from whatever the detail pane has already loaded;
// if the load is still in flight the overlay opens with an empty list and
// shows "(no transitions available)".
func (m Model) openTransitionOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	m.transition = m.transition.Show(issue.Key, m.detail.Transitions())
	return m, nil
}

// openCommentOverlay opens the `c` overlay scoped to the currently selected
// issue, prefilled from any saved draft for that key. No-op when no issue
// is selected.
func (m Model) openCommentOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.comment, cmd = m.comment.Show(issue.Key)
	if draft := m.loadDraft(issue.Key); draft != "" {
		m.comment = m.comment.SetValue(draft)
	}
	return m, cmd
}

// openRemoveWorklogOverlay opens the worklog-remove picker over the
// current issue's worklog list.
func (m Model) openRemoveWorklogOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	entries := make([]overlays.WorklogEntry, 0, len(issue.Worklogs))
	for _, w := range issue.Worklogs {
		author := ""
		if w.Author != nil {
			author = w.Author.DisplayName
		}
		when := ""
		if !w.Started.IsZero() {
			when = w.Started.Format("2006-01-02")
		}
		entries = append(entries, overlays.WorklogEntry{
			ID:        w.ID,
			TimeSpent: w.TimeSpent,
			Author:    author,
			When:      when,
		})
	}
	m.worklogRemove = m.worklogRemove.Show(issue.Key, entries)
	return m, nil
}

// openWorklogOverlay opens the log-work overlay for the selected issue.
func (m Model) openWorklogOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.worklog, cmd = m.worklog.Show(issue.Key)
	return m, cmd
}

// openPriorityPicker opens the priority picker for the current issue
// with the cursor on the issue's current priority.
func (m Model) openPriorityPicker() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	m.priority = m.priority.Show(issue.Key, issue.Priority.Name)
	return m, nil
}

// openEpicPicker opens the epic-link picker for the current issue and
// dispatches a SearchEpics call to populate it. The project is derived
// from the issue key prefix (e.g. "BILLING-123" → "BILLING").
func (m Model) openEpicPicker() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	if m.loader == nil {
		return m, nil
	}
	m.epicPicker = m.epicPicker.Show(issue.Key, issue.ParentKey)
	return m, m.searchEpicsCmd(issue.Key)
}

// openDescriptionOverlay opens the description-edit textarea, prefilled
// with the current markdown body.
func (m Model) openDescriptionOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.description, cmd = m.description.Show(issue.Key, issue.Description)
	return m, cmd
}

// openRemoveLinkOverlay opens the remove-link picker over the current
// issue's links. No-op when no issue is selected; opens with an empty
// state when the issue has no links yet (the user gets visual feedback
// the keypress was seen).
func (m Model) openRemoveLinkOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	entries := make([]overlays.LinkEntry, 0, len(issue.Links))
	for _, l := range issue.Links {
		entries = append(entries, overlays.LinkEntry{
			ID:       l.ID,
			Relation: l.Relation,
			OtherKey: l.OtherKey,
			Summary:  l.Summary,
		})
	}
	m.linkRemove = m.linkRemove.Show(issue.Key, entries)
	return m, nil
}

// openLinkOverlay opens the add-link overlay scoped to the currently
// selected issue. No-op when nothing is selected.
func (m Model) openLinkOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.link, cmd = m.link.Show(issue.Key)
	return m, cmd
}

// openFavoritesOverlay reads the saved favorites from disk and opens the
// picker. The current search query (when in Search view) is passed in so
// the overlay can offer save-mode. Disk read happens on demand here, not
// at startup, so the picker always shows fresh data when a save from
// another window has just landed.
func (m Model) openFavoritesOverlay() (tea.Model, tea.Cmd) {
	entries := m.loadFavoriteEntries()
	current := ""
	if m.view == panes.ViewSearch {
		current = m.searchQuery
	}
	m.favorites = m.favorites.Show(entries, current)
	return m, nil
}

// openEditOverlay opens the generic field-edit overlay scoped to the
// currently selected issue and field. No-op when nothing is selected. The
// pre-fill is the field's current display value so users can start from
// the existing text rather than retyping.
func (m Model) openEditOverlay(field overlays.EditField) (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		issue = m.list.Selected()
	}
	if issue == nil {
		return m, nil
	}
	current := ""
	switch field {
	case overlays.EditSummary:
		current = issue.Summary
	case overlays.EditPriority:
		current = issue.Priority.Name
	case overlays.EditLabels:
		current = strings.Join(issue.Labels, ", ")
	case overlays.EditDueDate:
		current = issue.DueDate
	}
	var cmd tea.Cmd
	m.edit, cmd = m.edit.Show(issue.Key, field, current)
	return m, cmd
}

// openAssignOverlay opens the `a` overlay scoped to the currently selected
// issue. When no issue is selected it is a no-op.
func (m Model) openAssignOverlay() (tea.Model, tea.Cmd) {
	issue := m.detail.Issue()
	if issue == nil {
		return m, nil
	}
	var cmd tea.Cmd
	m.assign, cmd = m.assign.Show(issue.Key, issue.Assignee)
	return m, cmd
}

// openCreateOverlay starts the create wizard. The projects fetch is
// asynchronous; the overlay opens at Step 1 once the response arrives.
func (m Model) openCreateOverlay() (tea.Model, tea.Cmd) {
	loader := m.loader
	if loader == nil {
		return m, nil
	}
	return m, func() tea.Msg {
		ps, err := loader.Projects(context.Background())
		return createProjectsLoadedMsg{Projects: ps, Err: err}
	}
}

// openCreateSubtaskOverlay opens the create wizard in subtask mode for
// the issue currently displayed in the detail pane. No-op when the
// detail is empty or no loader is wired.
func (m Model) openCreateSubtaskOverlay() (tea.Model, tea.Cmd) {
	loader := m.loader
	parent := m.detail.Issue()
	if loader == nil || parent == nil {
		return m, nil
	}
	parentClone := *parent
	return m, func() tea.Msg {
		ps, err := loader.Projects(context.Background())
		return createSubtaskProjectsLoadedMsg{Parent: parentClone, Projects: ps, Err: err}
	}
}

// openSearch flips the active view to Search, focuses the list pane, and
// puts its search header into editing mode pre-filled with the previous
// query (if any). Available from any focus, including FocusDetail.
func (m Model) openSearch() (tea.Model, tea.Cmd) {
	m.focus = FocusList
	m.view = panes.ViewSearch
	m.list.SetSearchEditing(m.searchQuery)
	return m, nil
}
