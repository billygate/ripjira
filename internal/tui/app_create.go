package tui

import (
	"strings"

	"github.com/billygate/ripjira/internal/jira"
)

// shouldOfferEpicLink reports whether the create wizard should advance to
// Step 4 (link to epic) for the just-created issue. Skipped when:
//   - subtask mode (parent is set; subtasks inherit their parent's epic);
//   - the issue type itself is an Epic (linking an epic to an epic is not
//     a meaningful flow in this client).
func (m Model) shouldOfferEpicLink(parentKey, typeName string) bool {
	if parentKey != "" {
		return false
	}
	for _, t := range m.epicTypes {
		if strings.EqualFold(t, typeName) {
			return false
		}
	}
	return true
}

// createProjectsLoadedMsg carries the result of the projects fetch
// triggered by openCreateOverlay. It is internal — never emitted by the
// overlay itself.
type createProjectsLoadedMsg struct {
	Projects []jira.Project
	Err      error
}

// createSubtaskProjectsLoadedMsg is the result of the projects fetch
// triggered by openCreateSubtaskOverlay. It carries the parent issue so
// the subsequent ShowAsSubtask call has the right context.
type createSubtaskProjectsLoadedMsg struct {
	Parent   jira.Issue
	Projects []jira.Project
	Err      error
}
