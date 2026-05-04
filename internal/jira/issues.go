package jira

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// myIssuesJQL is the fixed JQL used by MyIssues per the design spec.
const myIssuesJQL = "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"

// watchingJQL is the fixed JQL used by WatchingIssues per the design spec.
const watchingJQL = "watcher = currentUser() AND resolution = Unresolved ORDER BY updated DESC"

// reportedJQL backs ReportedIssues — issues the current user filed (is the
// reporter of) and which are still open. Useful for tracking work you've
// asked someone else to do.
const reportedJQL = "reporter = currentUser() AND resolution = Unresolved ORDER BY updated DESC"

// userDTO is the wire form of a Jira user.
type userDTO struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

func (d *userDTO) toDomain() *User {
	if d == nil {
		return nil
	}
	return &User{
		AccountID:   d.AccountID,
		DisplayName: d.DisplayName,
		Email:       d.EmailAddress,
	}
}

type statusDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	StatusCategory struct {
		Key string `json:"key"`
	} `json:"statusCategory"`
}

func (s *statusDTO) toDomain() Status {
	if s == nil {
		return Status{}
	}
	return Status{ID: s.ID, Name: s.Name, Category: s.StatusCategory.Key}
}

type priorityDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (p *priorityDTO) toDomain() Priority {
	if p == nil {
		return Priority{}
	}
	return Priority{ID: p.ID, Name: p.Name}
}

type issueTypeDTO struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
}

func (it *issueTypeDTO) toDomain() IssueType {
	if it == nil {
		return IssueType{}
	}
	return IssueType{ID: it.ID, Name: it.Name, Subtask: it.Subtask}
}

type commentDTO struct {
	ID      string   `json:"id"`
	Author  *userDTO `json:"author"`
	Created string   `json:"created"`
}

type renderedCommentDTO struct {
	ID   string `json:"id"`
	Body string `json:"body"`
}

type subtaskRefDTO struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string     `json:"summary"`
		Status  *statusDTO `json:"status"`
	} `json:"fields"`
}

type issueFieldsDTO struct {
	Summary   string        `json:"summary"`
	Status    *statusDTO    `json:"status"`
	Priority  *priorityDTO  `json:"priority"`
	IssueType *issueTypeDTO `json:"issuetype"`
	Assignee  *userDTO      `json:"assignee"`
	Reporter  *userDTO      `json:"reporter"`
	Labels    []string      `json:"labels"`
	DueDate   string        `json:"duedate"`
	Created   string        `json:"created"`
	Updated   string        `json:"updated"`
	Comment   *struct {
		Comments []commentDTO `json:"comments"`
	} `json:"comment"`
	Subtasks   []subtaskRefDTO `json:"subtasks"`
	Attachment []attachmentDTO `json:"attachment"`
	IssueLinks []issueLinkDTO  `json:"issuelinks"`
	Worklog    *struct {
		Worklogs []worklogDTO `json:"worklogs"`
	} `json:"worklog"`
}

// worklogDTO is the wire form of a single worklog entry returned inline
// with the issue when "*all" fields are requested.
type worklogDTO struct {
	ID           string   `json:"id"`
	Author       *userDTO `json:"author"`
	TimeSpent    string   `json:"timeSpent"`
	Started      string   `json:"started"`
	TimeSpentSec int64    `json:"timeSpentSeconds"`
}

// issueLinkDTO is the wire form of a Jira issue link. Exactly one of
// inwardIssue / outwardIssue is set per record; the type carries the
// direction-specific phrasing.
type issueLinkDTO struct {
	ID   string `json:"id"`
	Type struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Inward  string `json:"inward"`
		Outward string `json:"outward"`
	} `json:"type"`
	InwardIssue  *linkedIssueDTO `json:"inwardIssue"`
	OutwardIssue *linkedIssueDTO `json:"outwardIssue"`
}

type linkedIssueDTO struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string     `json:"summary"`
		Status  *statusDTO `json:"status"`
	} `json:"fields"`
}

type attachmentDTO struct {
	ID        string `json:"id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mimeType"`
	Size      int64  `json:"size"`
	Content   string `json:"content"`
	Thumbnail string `json:"thumbnail"`
}

type renderedFieldsDTO struct {
	Description string `json:"description"`
	Comment     *struct {
		Comments []renderedCommentDTO `json:"comments"`
	} `json:"comment"`
}

type issueDTO struct {
	Key            string             `json:"key"`
	Fields         issueFieldsDTO     `json:"fields"`
	RenderedFields *renderedFieldsDTO `json:"renderedFields,omitempty"`
}

type searchJQLResp struct {
	Issues        []issueDTO `json:"issues"`
	NextPageToken string     `json:"nextPageToken"`
	IsLast        bool       `json:"isLast"`
}

type transitionDTO struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	To   *statusDTO `json:"to"`
}

type transitionsResp struct {
	Transitions []transitionDTO `json:"transitions"`
}

// browseURL returns the user-facing URL for a Jira issue key.
func (c *Client) browseURL(key string) string {
	u := *c.baseURL
	u.Path = strings.TrimRight(u.Path, "/") + "/browse/" + key
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// dtoToIssue converts a wire DTO into the public Issue type. The renderedHTML
// description, when present, is converted to plain text; otherwise an empty
// description is left in place.
func (c *Client) dtoToIssue(d issueDTO) Issue {
	created, _ := parseJiraTime(d.Fields.Created)
	updated, _ := parseJiraTime(d.Fields.Updated)
	is := Issue{
		Key:      d.Key,
		Summary:  d.Fields.Summary,
		Status:   d.Fields.Status.toDomain(),
		Priority: d.Fields.Priority.toDomain(),
		Type:     d.Fields.IssueType.toDomain(),
		Assignee: d.Fields.Assignee.toDomain(),
		Reporter: d.Fields.Reporter.toDomain(),
		Labels:   append([]string(nil), d.Fields.Labels...),
		DueDate:  d.Fields.DueDate,
		Created:  created,
		Updated:  updated,
		URL:      c.browseURL(d.Key),
	}
	if len(d.Fields.Subtasks) > 0 {
		subs := make([]SubtaskRef, 0, len(d.Fields.Subtasks))
		for _, s := range d.Fields.Subtasks {
			if s.Key == "" {
				continue
			}
			subs = append(subs, SubtaskRef{
				Key:     s.Key,
				Summary: s.Fields.Summary,
				Status:  s.Fields.Status.toDomain(),
			})
		}
		is.Subtasks = subs
	}
	if d.Fields.Worklog != nil && len(d.Fields.Worklog.Worklogs) > 0 {
		ws := make([]Worklog, 0, len(d.Fields.Worklog.Worklogs))
		for _, w := range d.Fields.Worklog.Worklogs {
			started, _ := parseJiraTime(w.Started)
			ws = append(ws, Worklog{
				ID:        w.ID,
				Author:    w.Author.toDomain(),
				TimeSpent: w.TimeSpent,
				Seconds:   w.TimeSpentSec,
				Started:   started,
			})
		}
		is.Worklogs = ws
	}
	if len(d.Fields.IssueLinks) > 0 {
		links := make([]IssueLink, 0, len(d.Fields.IssueLinks))
		for _, l := range d.Fields.IssueLinks {
			var (
				other    *linkedIssueDTO
				outward  bool
				relation string
			)
			switch {
			case l.OutwardIssue != nil && l.OutwardIssue.Key != "":
				other = l.OutwardIssue
				outward = true
				relation = l.Type.Outward
			case l.InwardIssue != nil && l.InwardIssue.Key != "":
				other = l.InwardIssue
				outward = false
				relation = l.Type.Inward
			default:
				continue
			}
			links = append(links, IssueLink{
				ID:       l.ID,
				Relation: relation,
				TypeName: l.Type.Name,
				OtherKey: other.Key,
				Summary:  other.Fields.Summary,
				Status:   other.Fields.Status.toDomain(),
				Outward:  outward,
			})
		}
		is.Links = links
	}
	if len(d.Fields.Attachment) > 0 {
		atts := make([]Attachment, 0, len(d.Fields.Attachment))
		for _, a := range d.Fields.Attachment {
			if a.Content == "" {
				continue
			}
			atts = append(atts, Attachment(a))
		}
		is.Attachments = atts
	}
	if d.RenderedFields != nil {
		is.Description = htmlToMarkdown(d.RenderedFields.Description)
	}
	is.Comments = mergeComments(d.Fields.Comment, d.RenderedFields)
	return is
}

// mergeComments turns the structured author/created data from `fields.comment`
// and the rendered HTML body from `renderedFields.comment` into the domain
// Comment slice. Body is plain text (HTML stripped).
func mergeComments(structured *struct {
	Comments []commentDTO `json:"comments"`
}, rendered *renderedFieldsDTO) []Comment {
	if structured == nil || len(structured.Comments) == 0 {
		return nil
	}
	rmap := map[string]string{}
	if rendered != nil && rendered.Comment != nil {
		for _, rc := range rendered.Comment.Comments {
			rmap[rc.ID] = htmlToMarkdown(rc.Body)
		}
	}
	out := make([]Comment, 0, len(structured.Comments))
	for _, sc := range structured.Comments {
		created, _ := parseJiraTime(sc.Created)
		c := Comment{
			ID:      sc.ID,
			Body:    rmap[sc.ID],
			Created: created,
		}
		if u := sc.Author.toDomain(); u != nil {
			c.Author = *u
		}
		out = append(out, c)
	}
	return out
}

// Myself returns the authenticated user and caches the account ID on the
// client.
func (c *Client) Myself(ctx context.Context) (User, error) {
	var dto userDTO
	if err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil, &dto); err != nil {
		return User{}, err
	}
	u := dto.toDomain()
	if u == nil {
		return User{}, errors.New("jira: empty user response")
	}
	c.SetAccountID(u.AccountID)
	return *u, nil
}

// myIssuesFields lists the fields requested from the new search/jql endpoint.
// The endpoint returns no fields by default, so they must be enumerated
// (or `*all` requested) for summary/status/priority/assignee to be populated.
const myIssuesFields = "summary,status,priority,issuetype,assignee,reporter,updated"

// Search runs an arbitrary JQL query against /rest/api/3/search/jql,
// pages through nextPageToken, and requests the explicit fields list
// the new endpoint requires.
func (c *Client) Search(ctx context.Context, jql string) ([]Issue, error) {
	out := []Issue{}
	token := ""
	for {
		q := url.Values{}
		q.Set("jql", jql)
		q.Set("fields", myIssuesFields)
		if token != "" {
			q.Set("nextPageToken", token)
		}
		path := "/rest/api/3/search/jql?" + q.Encode()
		var resp searchJQLResp
		if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, err
		}
		for _, d := range resp.Issues {
			out = append(out, c.dtoToIssue(d))
		}
		if resp.IsLast || resp.NextPageToken == "" {
			break
		}
		token = resp.NextPageToken
	}
	return out, nil
}

// MyIssues returns the open issues assigned to the current user, ordered by
// most recently updated.
func (c *Client) MyIssues(ctx context.Context) ([]Issue, error) {
	return c.Search(ctx, myIssuesJQL)
}

// WatchingIssues returns the open issues the current user is watching,
// ordered by most recently updated.
func (c *Client) WatchingIssues(ctx context.Context) ([]Issue, error) {
	return c.Search(ctx, watchingJQL)
}

// ReportedIssues returns the open issues the current user reported (filed),
// ordered by most recently updated.
func (c *Client) ReportedIssues(ctx context.Context) ([]Issue, error) {
	return c.Search(ctx, reportedJQL)
}

// UpdateIssue patches the named fields on key via PUT /issue/{key}. The
// fields map keys must match Jira's REST v3 field IDs (e.g. "summary",
// "labels", "duedate", "priority"); values must already be in the wire
// shape Jira expects (e.g. priority is {"name":"High"}, not "High"). A 204
// is the happy path.
func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	if key == "" {
		return errors.New("jira: issue key is required")
	}
	if len(fields) == 0 {
		return errors.New("jira: no fields to update")
	}
	body := map[string]any{"fields": fields}
	path := "/rest/api/3/issue/" + url.PathEscape(key)
	return c.do(ctx, http.MethodPut, path, body, nil)
}

// UpdateDescription replaces the issue's description with the given
// markdown body. The text is parsed by the in-package markdownToADF
// converter, which handles paragraphs, ATX headings, fenced code blocks,
// bullet/ordered lists, and the inline marks **strong**, *em*, `code`,
// [text](url). Constructs outside that subset fall through as literal
// text so submission never fails — round-trip fidelity is best-effort.
func (c *Client) UpdateDescription(ctx context.Context, key, body string) error {
	if key == "" {
		return errors.New("jira: issue key is required")
	}
	doc := markdownToADF(body)
	return c.UpdateIssue(ctx, key, map[string]any{"description": doc})
}

// AddWorklog logs work against issue. timeSpent uses Jira's compact
// format ("1h 30m", "2d", "45m") and is validated server-side. comment is
// optional; when non-empty it is sent as an ADF paragraph.
func (c *Client) AddWorklog(ctx context.Context, key, timeSpent, comment string) error {
	if key == "" {
		return errors.New("jira: issue key is required")
	}
	if strings.TrimSpace(timeSpent) == "" {
		return errors.New("jira: timeSpent is required")
	}
	body := map[string]any{"timeSpent": timeSpent}
	if strings.TrimSpace(comment) != "" {
		body["comment"] = map[string]any{
			"type":    "doc",
			"version": 1,
			"content": []any{
				map[string]any{
					"type": "paragraph",
					"content": []any{
						map[string]any{"type": "text", "text": comment},
					},
				},
			},
		}
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/worklog"
	return c.do(ctx, http.MethodPost, path, body, nil)
}

// DeleteWorklog removes a worklog entry by ID. Returns 404 from the API
// when the entry doesn't exist; 403 when the current user can't delete
// it (Jira lets only the author and admins delete).
func (c *Client) DeleteWorklog(ctx context.Context, issueKey, worklogID string) error {
	if issueKey == "" || worklogID == "" {
		return errors.New("jira: issue key and worklog ID are required")
	}
	path := "/rest/api/3/issue/" + url.PathEscape(issueKey) + "/worklog/" + url.PathEscape(worklogID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// AddWatcher subscribes the given account to issue change notifications.
// Empty accountID means "the authenticated user" — Jira interprets a body
// of `null` as self.
func (c *Client) AddWatcher(ctx context.Context, key, accountID string) error {
	if key == "" {
		return errors.New("jira: issue key is required")
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/watchers"
	var body any
	if accountID != "" {
		body = accountID
	}
	return c.do(ctx, http.MethodPost, path, body, nil)
}

// RemoveWatcher unsubscribes the given account. accountID is required —
// Jira does not allow self-unwatch via empty argument on this endpoint.
func (c *Client) RemoveWatcher(ctx context.Context, key, accountID string) error {
	if key == "" || accountID == "" {
		return errors.New("jira: issue key and accountId are required")
	}
	q := url.Values{}
	q.Set("accountId", accountID)
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/watchers?" + q.Encode()
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// CreateIssueLink links two issues with the given relationship type. The
// API is direction-aware: outwardIssue is the issue the relationship
// "points to". For example, to mark PROJ-1 as blocking PROJ-2 you call
// CreateIssueLink(ctx, "Blocks", "PROJ-1", "PROJ-2"). typeName must match
// one of the link types configured on the Jira instance — invalid names
// are rejected with a 400.
func (c *Client) CreateIssueLink(ctx context.Context, typeName, inwardKey, outwardKey string) error {
	if typeName == "" {
		return errors.New("jira: link type is required")
	}
	if inwardKey == "" || outwardKey == "" {
		return errors.New("jira: both keys are required")
	}
	body := map[string]any{
		"type":         map[string]any{"name": typeName},
		"inwardIssue":  map[string]any{"key": inwardKey},
		"outwardIssue": map[string]any{"key": outwardKey},
	}
	return c.do(ctx, http.MethodPost, "/rest/api/3/issueLink", body, nil)
}

// DeleteIssueLink removes the link with the given ID. Returns 404 from
// the API if the link doesn't exist; the caller's error toast is fine for
// that.
func (c *Client) DeleteIssueLink(ctx context.Context, linkID string) error {
	if linkID == "" {
		return errors.New("jira: link ID is required")
	}
	path := "/rest/api/3/issueLink/" + url.PathEscape(linkID)
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// GetIssue returns the full issue including rendered description and
// comments.
func (c *Client) GetIssue(ctx context.Context, key string) (Issue, error) {
	if key == "" {
		return Issue{}, errors.New("jira: issue key is required")
	}
	q := url.Values{}
	q.Set("fields", "*all")
	q.Set("expand", "renderedFields")
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "?" + q.Encode()
	var d issueDTO
	if err := c.do(ctx, http.MethodGet, path, nil, &d); err != nil {
		return Issue{}, err
	}
	return c.dtoToIssue(d), nil
}

// GetTransitions returns the workflow transitions available for the issue.
func (c *Client) GetTransitions(ctx context.Context, key string) ([]Transition, error) {
	if key == "" {
		return nil, errors.New("jira: issue key is required")
	}
	var resp transitionsResp
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/transitions"
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]Transition, 0, len(resp.Transitions))
	for _, t := range resp.Transitions {
		out = append(out, Transition{
			ID:   t.ID,
			Name: t.Name,
			To:   t.To.toDomain(),
		})
	}
	return out, nil
}

// DoTransition triggers the named transition on the issue.
func (c *Client) DoTransition(ctx context.Context, key, transitionID string) error {
	if key == "" {
		return errors.New("jira: issue key is required")
	}
	if transitionID == "" {
		return errors.New("jira: transition id is required")
	}
	body := map[string]any{"transition": map[string]string{"id": transitionID}}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/transitions"
	return c.do(ctx, http.MethodPost, path, body, nil)
}

// AssignIssue assigns the issue to the user with the given account ID. An
// empty accountID unassigns the issue.
func (c *Client) AssignIssue(ctx context.Context, key, accountID string) error {
	if key == "" {
		return errors.New("jira: issue key is required")
	}
	body := map[string]any{"accountId": nil}
	if accountID != "" {
		body["accountId"] = accountID
	}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/assignee"
	return c.do(ctx, http.MethodPut, path, body, nil)
}

// SearchUsers returns users matching the query. Used by the assignee picker.
func (c *Client) SearchUsers(ctx context.Context, query string) ([]User, error) {
	q := url.Values{}
	q.Set("query", query)
	path := "/rest/api/3/user/search?" + q.Encode()
	var dtos []userDTO
	if err := c.do(ctx, http.MethodGet, path, nil, &dtos); err != nil {
		return nil, err
	}
	out := make([]User, 0, len(dtos))
	for _, d := range dtos {
		if u := d.toDomain(); u != nil {
			out = append(out, *u)
		}
	}
	return out, nil
}

// DownloadAttachment fetches the bytes of an attachment from its absolute
// `content` URL (as returned by the issue API). The response is capped at
// maxBytes; pass 0 to use the default 8 MiB cap. Returns the body and the
// server-reported Content-Type.
func (c *Client) DownloadAttachment(ctx context.Context, contentURL string, maxBytes int64) ([]byte, string, error) {
	if contentURL == "" {
		return nil, "", errors.New("jira: empty attachment URL")
	}
	if maxBytes <= 0 {
		maxBytes = 8 << 20
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, contentURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", c.authHeader)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("User-Agent", UserAgent())
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return nil, "", &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Method:     http.MethodGet,
			URL:        contentURL,
		}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if int64(len(body)) > maxBytes {
		return nil, "", fmt.Errorf("jira: attachment exceeds %d bytes", maxBytes)
	}
	return body, resp.Header.Get("Content-Type"), nil
}

// --- helpers ---------------------------------------------------------------

// jiraTimeLayouts lists the time formats Jira may produce for `updated`,
// `created`, etc. We try them in order and return the first that matches.
var jiraTimeLayouts = []string{
	"2006-01-02T15:04:05.000-0700",
	"2006-01-02T15:04:05-0700",
	time.RFC3339Nano,
	time.RFC3339,
}

// parseJiraTime parses a timestamp produced by the Jira REST API. An empty
// input yields a zero time without error.
func parseJiraTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	var lastErr error
	for _, layout := range jiraTimeLayouts {
		t, err := time.Parse(layout, s)
		if err == nil {
			return t, nil
		}
		lastErr = err
	}
	return time.Time{}, fmt.Errorf("jira: parse time %q: %w", s, lastErr)
}

var (
	// blockClose matches the closing tag of a block-level element after which
	// we want a paragraph break.
	blockClose = regexp.MustCompile(`(?i)</(p|h[1-6]|blockquote|pre)\s*>`)
	// lineBreak matches block elements that should produce a single newline.
	lineBreak = regexp.MustCompile(`(?i)<br\s*/?>|</li\s*>|</tr\s*>|</div\s*>`)
	// anyTag strips remaining HTML tags.
	anyTag = regexp.MustCompile(`<[^>]*>`)
	// extraBlankLines collapses 3+ consecutive newlines into 2.
	extraBlankLines = regexp.MustCompile(`\n{3,}`)
)

func stripHTML(s string) string {
	if s == "" {
		return ""
	}
	s = blockClose.ReplaceAllString(s, "\n\n")
	s = lineBreak.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	// Normalise no-break spaces to regular spaces so callers that compare
	// against simple ASCII strings behave.
	s = strings.ReplaceAll(s, " ", " ")
	s = extraBlankLines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
