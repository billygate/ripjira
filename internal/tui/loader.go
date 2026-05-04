package tui

import (
	"context"
	"strings"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/panes"
)

// AppLoader bundles the per-issue loaders consumed by the detail pane
// with the list refresh that drives the centre pane's contents.
type AppLoader interface {
	panes.Loader
	LoadIssues(ctx context.Context, view panes.ViewKind, query string) ([]jira.Issue, error)
	DoTransition(ctx context.Context, key, transitionID string) error
	AddComment(ctx context.Context, key, body string) error
	SearchUsers(ctx context.Context, query string) ([]jira.User, error)
	AssignIssue(ctx context.Context, key, accountID string) error
	UpdateFields(ctx context.Context, key string, fields map[string]any) error
	UpdateDescription(ctx context.Context, key, body string) error
	CreateLink(ctx context.Context, typeName, inwardKey, outwardKey string) error
	DeleteLink(ctx context.Context, linkID string) error
	AddWatcher(ctx context.Context, key, accountID string) error
	RemoveWatcher(ctx context.Context, key, accountID string) error
	AddWorklog(ctx context.Context, key, timeSpent, comment string) error
	DeleteWorklog(ctx context.Context, key, worklogID string) error
	GetMyself(ctx context.Context) (jira.User, error)
	Projects(ctx context.Context) ([]jira.Project, error)
	IssueTypesForProject(ctx context.Context, projectKey string) ([]jira.IssueType, error)
	CreateMeta(ctx context.Context, projectKey, issueTypeID string) (jira.CreateMeta, error)
	CreateIssue(ctx context.Context, p jira.CreatePayload) (jira.Issue, error)
	SearchEpics(ctx context.Context, projectKey string, epicTypes []string) ([]jira.Issue, error)
	SetParent(ctx context.Context, key, parentKey string) error
}

// jiraClient is the adapter from *jira.Client (or anything matching its
// method set) to AppLoader. Defining the dependency as an interface keeps
// tui_test free of an actual *jira.Client and lets tests substitute stubs
// for both detail and list loads.
type jiraClient interface {
	MyIssues(ctx context.Context) ([]jira.Issue, error)
	WatchingIssues(ctx context.Context) ([]jira.Issue, error)
	ReportedIssues(ctx context.Context) ([]jira.Issue, error)
	Search(ctx context.Context, jql string) ([]jira.Issue, error)
	GetIssue(ctx context.Context, key string) (jira.Issue, error)
	GetTransitions(ctx context.Context, key string) ([]jira.Transition, error)
	DownloadAttachment(ctx context.Context, contentURL string, maxBytes int64) ([]byte, string, error)
	DoTransition(ctx context.Context, key, transitionID string) error
	AddComment(ctx context.Context, key, body string) error
	SearchUsers(ctx context.Context, query string) ([]jira.User, error)
	AssignIssue(ctx context.Context, key, accountID string) error
	UpdateIssue(ctx context.Context, key string, fields map[string]any) error
	UpdateDescription(ctx context.Context, key, body string) error
	CreateIssueLink(ctx context.Context, typeName, inwardKey, outwardKey string) error
	DeleteIssueLink(ctx context.Context, linkID string) error
	AddWatcher(ctx context.Context, key, accountID string) error
	RemoveWatcher(ctx context.Context, key, accountID string) error
	AddWorklog(ctx context.Context, key, timeSpent, comment string) error
	DeleteWorklog(ctx context.Context, key, worklogID string) error
	Myself(ctx context.Context) (jira.User, error)
	Projects(ctx context.Context) ([]jira.Project, error)
	IssueTypesForProject(ctx context.Context, projectKey string) ([]jira.IssueType, error)
	CreateMeta(ctx context.Context, projectKey, issueTypeID string) (jira.CreateMeta, error)
	CreateIssue(ctx context.Context, p jira.CreatePayload) (jira.Issue, error)
	SearchEpics(ctx context.Context, projectKey string, epicTypes []string) ([]jira.Issue, error)
	SetParent(ctx context.Context, key, parentKey string) error
}

// NewClientLoader wraps a *jira.Client (or compatible interface) as an
// AppLoader. LoadComments reuses the issue fetch's embedded comment list
// because the Jira REST v3 issue endpoint already returns them — splitting
// would just double the round-trip count.
func NewClientLoader(c jiraClient) AppLoader {
	return &clientLoader{c: c}
}

type clientLoader struct{ c jiraClient }

func (l *clientLoader) LoadIssues(ctx context.Context, view panes.ViewKind, query string) ([]jira.Issue, error) {
	switch view {
	case panes.ViewWatching:
		return l.c.WatchingIssues(ctx)
	case panes.ViewReported:
		return l.c.ReportedIssues(ctx)
	case panes.ViewRecent, panes.ViewSprint, panes.ViewMentions:
		// All three use a model-constructed JQL passed via the query
		// argument. Empty query means "we couldn't construct one" (no
		// recents yet, or the displayName isn't loaded yet) — return
		// nothing rather than burning a Jira request.
		if strings.TrimSpace(query) == "" {
			return nil, nil
		}
		return l.c.Search(ctx, query)
	case panes.ViewSearch:
		return l.c.Search(ctx, wrapSearchQuery(query))
	default:
		return l.c.MyIssues(ctx)
	}
}

// wrapSearchQuery applies a deliberate heuristic: if the query already looks
// like JQL (contains "=", "~", " AND ", " OR ", " IN ", or " ORDER " — case
// insensitive) it is sent verbatim. Otherwise it is wrapped as
// `text ~ "<escaped>"`.
func wrapSearchQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return q
	}
	lower := strings.ToLower(q)
	for _, op := range []string{" = ", " ~ ", " and ", " or ", " in ", " order "} {
		if strings.Contains(lower, op) {
			return q
		}
	}
	escaped := strings.ReplaceAll(q, `"`, `\"`)
	return `text ~ "` + escaped + `"`
}

func (l *clientLoader) LoadIssue(ctx context.Context, key string) (jira.Issue, error) {
	return l.c.GetIssue(ctx, key)
}

func (l *clientLoader) LoadComments(ctx context.Context, key string) ([]jira.Comment, error) {
	is, err := l.c.GetIssue(ctx, key)
	if err != nil {
		return nil, err
	}
	return is.Comments, nil
}

func (l *clientLoader) LoadTransitions(ctx context.Context, key string) ([]jira.Transition, error) {
	return l.c.GetTransitions(ctx, key)
}

func (l *clientLoader) LoadAttachment(ctx context.Context, contentURL string) ([]byte, string, error) {
	return l.c.DownloadAttachment(ctx, contentURL, 0)
}

func (l *clientLoader) DoTransition(ctx context.Context, key, transitionID string) error {
	return l.c.DoTransition(ctx, key, transitionID)
}

func (l *clientLoader) AddComment(ctx context.Context, key, body string) error {
	return l.c.AddComment(ctx, key, body)
}

func (l *clientLoader) SearchUsers(ctx context.Context, query string) ([]jira.User, error) {
	return l.c.SearchUsers(ctx, query)
}

func (l *clientLoader) AssignIssue(ctx context.Context, key, accountID string) error {
	return l.c.AssignIssue(ctx, key, accountID)
}

func (l *clientLoader) UpdateFields(ctx context.Context, key string, fields map[string]any) error {
	return l.c.UpdateIssue(ctx, key, fields)
}

func (l *clientLoader) UpdateDescription(ctx context.Context, key, body string) error {
	return l.c.UpdateDescription(ctx, key, body)
}

func (l *clientLoader) CreateLink(ctx context.Context, typeName, inwardKey, outwardKey string) error {
	return l.c.CreateIssueLink(ctx, typeName, inwardKey, outwardKey)
}

func (l *clientLoader) DeleteLink(ctx context.Context, linkID string) error {
	return l.c.DeleteIssueLink(ctx, linkID)
}

func (l *clientLoader) AddWatcher(ctx context.Context, key, accountID string) error {
	return l.c.AddWatcher(ctx, key, accountID)
}

func (l *clientLoader) RemoveWatcher(ctx context.Context, key, accountID string) error {
	return l.c.RemoveWatcher(ctx, key, accountID)
}

func (l *clientLoader) AddWorklog(ctx context.Context, key, timeSpent, comment string) error {
	return l.c.AddWorklog(ctx, key, timeSpent, comment)
}

func (l *clientLoader) DeleteWorklog(ctx context.Context, key, worklogID string) error {
	return l.c.DeleteWorklog(ctx, key, worklogID)
}

func (l *clientLoader) GetMyself(ctx context.Context) (jira.User, error) {
	return l.c.Myself(ctx)
}

func (l *clientLoader) Projects(ctx context.Context) ([]jira.Project, error) {
	return l.c.Projects(ctx)
}

func (l *clientLoader) IssueTypesForProject(ctx context.Context, projectKey string) ([]jira.IssueType, error) {
	return l.c.IssueTypesForProject(ctx, projectKey)
}

func (l *clientLoader) CreateMeta(ctx context.Context, projectKey, issueTypeID string) (jira.CreateMeta, error) {
	return l.c.CreateMeta(ctx, projectKey, issueTypeID)
}

func (l *clientLoader) CreateIssue(ctx context.Context, p jira.CreatePayload) (jira.Issue, error) {
	return l.c.CreateIssue(ctx, p)
}

func (l *clientLoader) SearchEpics(ctx context.Context, projectKey string, epicTypes []string) ([]jira.Issue, error) {
	return l.c.SearchEpics(ctx, projectKey, epicTypes)
}

func (l *clientLoader) SetParent(ctx context.Context, key, parentKey string) error {
	return l.c.SetParent(ctx, key, parentKey)
}
