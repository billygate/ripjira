package tui

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/structure"
	"github.com/billygate/ripjira/internal/tui/overlays"
	"github.com/billygate/ripjira/internal/tui/panes"
	"github.com/billygate/ripjira/internal/tui/structureadapter"
)

// treeToSectionNodes converts a structure.TreeNode forest (carrying the
// adapter-wrapped Issue interface) into the panes.SectionNode shape used by
// the list-pane renderer (which works with concrete jira.Issue values).
func treeToSectionNodes(nodes []structure.TreeNode) []panes.SectionNode {
	out := make([]panes.SectionNode, 0, len(nodes))
	for _, n := range nodes {
		pn := panes.SectionNode{
			Title: n.Title,
			Path:  n.Path,
			Depth: n.Depth,
		}
		if len(n.Children) > 0 {
			pn.Children = treeToSectionNodes(n.Children)
		} else {
			pn.Issues = make([]jira.Issue, 0, len(n.Issues))
			for _, x := range n.Issues {
				if a, ok := x.(structureadapter.Adapter); ok {
					pn.Issues = append(pn.Issues, a.Issue())
				}
			}
		}
		out = append(out, pn)
	}
	return out
}

// activeStructure resolves the currently-selected structure for the default
// project. Falls back to the Default built-in when no selection exists.
func (m *Model) activeStructure() (structure.Structure, bool) {
	pk := m.defaultProject
	if pk == "" {
		return structure.Structure{}, false
	}
	id := m.currentStructID[pk]
	if id == "" {
		id = structure.BuiltinDefaultID
	}
	all, err := m.loadStructuresFor(pk)
	if err != nil {
		return structure.Structure{}, false
	}
	for i := range all {
		if all[i].ID == id {
			return all[i], true
		}
	}
	if len(all) > 0 {
		return all[0], true
	}
	return structure.Structure{}, false
}

// editStructuresYAML suspends the TUI and runs the user's $EDITOR (fallback
// vim) on the structures YAML for the active project. Creates the file with
// a starter template if it doesn't exist. The fsnotify watcher picks up the
// change after exit; toast surfaces an error if the editor failed.
func (m Model) editStructuresYAML() (tea.Model, tea.Cmd) {
	if m.structures == nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: store unavailable", Level: ToastError}
		}
	}
	pk := m.defaultProject
	if pk == "" {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: set default project to edit YAML", Level: ToastInfo}
		}
	}
	path := m.structures.Path(pk)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: " + err.Error(), Level: ToastError}
		}
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		template := []byte(starterStructuresYAML(pk))
		if werr := os.WriteFile(path, template, 0o600); werr != nil {
			return m, func() tea.Msg {
				return ToastMsg{Text: "structures: " + werr.Error(), Level: ToastError}
			}
		}
	}
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vim"
	}
	cmd := exec.Command(editor, path)
	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			return ToastMsg{Text: "editor: " + err.Error(), Level: ToastError}
		}
		return ToastMsg{Text: "structures reloaded", Level: ToastInfo}
	})
}

// starterStructuresYAML returns a commented-out example users can edit when
// no file exists yet for project pk.
func starterStructuresYAML(pk string) string {
	return "# ripjira structures for " + pk + "\n" +
		"# Each entry is a structure shown in the STRUCTURES picker.\n" +
		"# Field whitelist: status, status_category, priority, issuetype,\n" +
		"# assignee, reporter, parent_key, labels, project.\n" +
		"#\n" +
		"# - id: my-team\n" +
		"#   name: My team\n" +
		"#   sections:\n" +
		"#     - title: In progress\n" +
		"#       filter:\n" +
		"#         status: [Open, \"In Progress\"]\n" +
		"#         assignee: { exists: true }\n" +
		"#       group_by: [priority]\n" +
		"#     - title: Blocked\n" +
		"#       filter:\n" +
		"#         labels: [blocker]\n"
}

// loadStructuresFor returns built-ins + user structures for project, caching
// the result. The watcher invalidates the cache on file changes.
func (m *Model) loadStructuresFor(project string) ([]structure.Structure, error) {
	if v, ok := m.loadedStructs[project]; ok {
		return v, nil
	}
	if m.structures == nil {
		v := structure.Builtins(project)
		m.loadedStructs[project] = v
		return v, nil
	}
	v, err := m.structures.Load(project)
	if err != nil {
		return nil, err
	}
	m.loadedStructs[project] = v
	return v, nil
}

// watchStructuresNextCmd blocks for the next watcher event and translates it
// into structureChangedMsg. Re-armed by the Update handler after each event.
func (m Model) watchStructuresNextCmd() tea.Cmd {
	if m.structureEvents == nil {
		return nil
	}
	events := m.structureEvents
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil
		}
		return structureChangedMsg{Project: ev.ProjectKey}
	}
}

// handleEditScope opens the visual scope editor for the structure with the
// given id. Falls back to a toast on read-only structures or load errors.
func (m Model) handleEditScope(id string) (tea.Model, tea.Cmd) {
	pk := m.defaultProject
	if pk == "" || m.structures == nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: store unavailable", Level: ToastError}
		}
	}
	str, err := m.structures.FindByID(pk, id)
	if err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: " + err.Error(), Level: ToastError}
		}
	}
	if str.IsReadOnly() {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structure is read-only", Level: ToastInfo}
		}
	}
	rows := structureadapter.RowsFromFilter(str.Scope)
	issues := m.list.Issues()
	provider := func(field string) []string { return UniqueValues(issues, field) }
	m.scopeEditor = m.scopeEditor.ShowWithID(str.ID, str.Name, rows, provider)
	return m, nil
}

// handleScopeSaved persists the new scope to disk via the structure store
// and re-applies the active structure so the list reflects the change.
func (m Model) handleScopeSaved(msg overlays.ScopeSavedMsg) (tea.Model, tea.Cmd) {
	pk := m.defaultProject
	if pk == "" || m.structures == nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "structures: store unavailable", Level: ToastError}
		}
	}
	str, err := m.structures.FindByID(pk, msg.StructureID)
	if err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "scope save: " + err.Error(), Level: ToastError}
		}
	}
	str.Scope = structureadapter.FilterFromRows(msg.Rows)
	if err := m.structures.SaveStructure(&str); err != nil {
		return m, func() tea.Msg {
			return ToastMsg{Text: "scope save: " + err.Error(), Level: ToastError}
		}
	}
	delete(m.loadedStructs, pk)
	m.feedList(m.list.Issues())
	return m, func() tea.Msg {
		return ToastMsg{Text: "scope saved", Level: ToastInfo}
	}
}
