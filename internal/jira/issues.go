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
	Created   string        `json:"created"`
	Updated   string        `json:"updated"`
	Comment   *struct {
		Comments []commentDTO `json:"comments"`
	} `json:"comment"`
	Subtasks   []subtaskRefDTO `json:"subtasks"`
	Attachment []attachmentDTO `json:"attachment"`
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
