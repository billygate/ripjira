package tui

import (
	"context"
	"testing"

	"github.com/billygate/ripjira/internal/jira"
	"github.com/billygate/ripjira/internal/tui/panes"
)

type stubJiraClient struct {
	calledMyIssues       int
	calledWatchingIssues int
	calledReportedIssues int
	searchJQL            string
}

func (s *stubJiraClient) MyIssues(_ context.Context) ([]jira.Issue, error) {
	s.calledMyIssues++
	return nil, nil
}
func (s *stubJiraClient) WatchingIssues(_ context.Context) ([]jira.Issue, error) {
	s.calledWatchingIssues++
	return nil, nil
}
func (s *stubJiraClient) ReportedIssues(_ context.Context) ([]jira.Issue, error) {
	s.calledReportedIssues++
	return nil, nil
}
func (s *stubJiraClient) Search(_ context.Context, jql string) ([]jira.Issue, error) {
	s.searchJQL = jql
	return nil, nil
}
func (s *stubJiraClient) GetIssue(context.Context, string) (jira.Issue, error) {
	return jira.Issue{}, nil
}
func (s *stubJiraClient) GetTransitions(context.Context, string) ([]jira.Transition, error) {
	return nil, nil
}
func (s *stubJiraClient) DownloadAttachment(context.Context, string, int64) ([]byte, string, error) {
	return nil, "", nil
}
func (s *stubJiraClient) DoTransition(context.Context, string, string) error       { return nil }
func (s *stubJiraClient) AddComment(context.Context, string, string) error         { return nil }
func (s *stubJiraClient) SearchUsers(context.Context, string) ([]jira.User, error) { return nil, nil }
func (s *stubJiraClient) AssignIssue(context.Context, string, string) error        { return nil }
func (s *stubJiraClient) UpdateIssue(context.Context, string, map[string]any) error { return nil }
func (s *stubJiraClient) UpdateDescription(context.Context, string, string) error   { return nil }
func (s *stubJiraClient) CreateIssueLink(context.Context, string, string, string) error {
	return nil
}
func (s *stubJiraClient) DeleteIssueLink(context.Context, string) error { return nil }
func (s *stubJiraClient) AddWatcher(context.Context, string, string) error    { return nil }
func (s *stubJiraClient) RemoveWatcher(context.Context, string, string) error { return nil }
func (s *stubJiraClient) AddWorklog(context.Context, string, string, string) error {
	return nil
}
func (s *stubJiraClient) Myself(context.Context) (jira.User, error)                { return jira.User{}, nil }
func (s *stubJiraClient) Projects(context.Context) ([]jira.Project, error)         { return nil, nil }
func (s *stubJiraClient) IssueTypesForProject(context.Context, string) ([]jira.IssueType, error) {
	return nil, nil
}
func (s *stubJiraClient) CreateMeta(context.Context, string, string) (jira.CreateMeta, error) {
	return jira.CreateMeta{}, nil
}
func (s *stubJiraClient) CreateIssue(context.Context, jira.CreatePayload) (jira.Issue, error) {
	return jira.Issue{}, nil
}

func TestClientLoader_LoadIssues(t *testing.T) {
	t.Run("my tasks", func(t *testing.T) {
		s := &stubJiraClient{}
		l := NewClientLoader(s)
		if _, err := l.LoadIssues(context.Background(), panes.ViewMyTasks, ""); err != nil {
			t.Fatal(err)
		}
		if s.calledMyIssues != 1 {
			t.Fatalf("MyIssues calls: %d", s.calledMyIssues)
		}
	})
	t.Run("watching", func(t *testing.T) {
		s := &stubJiraClient{}
		l := NewClientLoader(s)
		if _, err := l.LoadIssues(context.Background(), panes.ViewWatching, ""); err != nil {
			t.Fatal(err)
		}
		if s.calledWatchingIssues != 1 {
			t.Fatalf("WatchingIssues calls: %d", s.calledWatchingIssues)
		}
	})
	t.Run("reported", func(t *testing.T) {
		s := &stubJiraClient{}
		l := NewClientLoader(s)
		if _, err := l.LoadIssues(context.Background(), panes.ViewReported, ""); err != nil {
			t.Fatal(err)
		}
		if s.calledReportedIssues != 1 {
			t.Fatalf("ReportedIssues calls: %d", s.calledReportedIssues)
		}
	})
	t.Run("search wraps non-jql text", func(t *testing.T) {
		s := &stubJiraClient{}
		l := NewClientLoader(s)
		if _, err := l.LoadIssues(context.Background(), panes.ViewSearch, "PROJ-1"); err != nil {
			t.Fatal(err)
		}
		if s.searchJQL != `text ~ "PROJ-1"` {
			t.Fatalf("jql: %q", s.searchJQL)
		}
	})
	t.Run("search passes jql through", func(t *testing.T) {
		s := &stubJiraClient{}
		l := NewClientLoader(s)
		q := `project = ABC AND assignee = currentUser()`
		if _, err := l.LoadIssues(context.Background(), panes.ViewSearch, q); err != nil {
			t.Fatal(err)
		}
		if s.searchJQL != q {
			t.Fatalf("jql: %q", s.searchJQL)
		}
	})
}

func TestWrapSearchQuery(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text", "PROJ-1", `text ~ "PROJ-1"`},
		{"tilde without spaces wraps", "~done", `text ~ "~done"`},
		{"equals without spaces wraps", "c=d notes", `text ~ "c=d notes"`},
		{"jql with spaced equals", "project = ABC", "project = ABC"},
		{"jql with spaced tilde", `summary ~ "foo"`, `summary ~ "foo"`},
		{"AND case insensitive", "project = ABC and assignee = me()", "project = ABC and assignee = me()"},
		{"escapes quotes", `say "hi"`, `text ~ "say \"hi\""`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := wrapSearchQuery(tc.in); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
