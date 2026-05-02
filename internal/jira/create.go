package jira

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"strconv"
)

type projectDTO struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (d projectDTO) toDomain() Project {
	return Project(d)
}

type projectSearchResp struct {
	Values     []projectDTO `json:"values"`
	IsLast     bool         `json:"isLast"`
	NextPage   string       `json:"nextPage"`
	StartAt    int          `json:"startAt"`
	MaxResults int          `json:"maxResults"`
}

// fieldOptionDTO covers the various shapes Jira returns inside allowedValues.
// Different field kinds use either "name" or "value" for the label, so we
// accept both and prefer name when both are present.
type fieldOptionDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Value string `json:"value"`
}

func (o fieldOptionDTO) toDomain() FieldOption {
	name := o.Name
	if name == "" {
		name = o.Value
	}
	return FieldOption{ID: o.ID, Name: name}
}

type fieldSchemaDTO struct {
	Type   string `json:"type"`
	Items  string `json:"items"`
	System string `json:"system"`
	Custom string `json:"custom"`
}

type fieldMetaDTO struct {
	FieldID       string           `json:"fieldId"`
	Name          string           `json:"name"`
	Required      bool             `json:"required"`
	Schema        fieldSchemaDTO   `json:"schema"`
	AllowedValues []fieldOptionDTO `json:"allowedValues"`
}

type createMetaResp struct {
	Fields []fieldMetaDTO `json:"fields"`
}

type createIssueResp struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

// Projects returns all projects visible to the authenticated user. Pagination
// is followed until isLast=true so callers receive the full list.
func (c *Client) Projects(ctx context.Context) ([]Project, error) {
	const pageSize = 50
	out := make([]Project, 0, pageSize)
	startAt := 0
	for {
		q := url.Values{}
		q.Set("startAt", strconv.Itoa(startAt))
		q.Set("maxResults", strconv.Itoa(pageSize))
		path := "/rest/api/3/project/search?" + q.Encode()
		var resp projectSearchResp
		if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
			return nil, err
		}
		for _, d := range resp.Values {
			out = append(out, d.toDomain())
		}
		if resp.IsLast || len(resp.Values) == 0 {
			return out, nil
		}
		startAt += len(resp.Values)
	}
}

// issueTypesResp is the wire form of GET /rest/api/3/issue/createmeta/{key}/issuetypes.
type issueTypesResp struct {
	IssueTypes []issueTypeDTO `json:"issueTypes"`
}

// IssueTypesForProject returns the issue types available in the project.
// Subtask types are flagged via IssueType.Subtask.
func (c *Client) IssueTypesForProject(ctx context.Context, projectKey string) ([]IssueType, error) {
	if projectKey == "" {
		return nil, errors.New("jira: project key is required")
	}
	path := "/rest/api/3/issue/createmeta/" + url.PathEscape(projectKey) + "/issuetypes"
	var resp issueTypesResp
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]IssueType, 0, len(resp.IssueTypes))
	for _, t := range resp.IssueTypes {
		out = append(out, t.toDomain())
	}
	return out, nil
}

// CreateMeta returns the field metadata for the given project and issue type.
func (c *Client) CreateMeta(ctx context.Context, projectKey, issueTypeID string) (CreateMeta, error) {
	if projectKey == "" {
		return CreateMeta{}, errors.New("jira: project key is required")
	}
	if issueTypeID == "" {
		return CreateMeta{}, errors.New("jira: issue type id is required")
	}
	path := "/rest/api/3/issue/createmeta/" +
		url.PathEscape(projectKey) + "/issuetypes/" + url.PathEscape(issueTypeID)
	var resp createMetaResp
	if err := c.do(ctx, http.MethodGet, path, nil, &resp); err != nil {
		return CreateMeta{}, err
	}
	fields := make([]FieldMeta, 0, len(resp.Fields))
	for _, f := range resp.Fields {
		fm := FieldMeta{
			ID:          f.FieldID,
			Name:        f.Name,
			Required:    f.Required,
			SchemaType:  f.Schema.Type,
			SchemaItems: f.Schema.Items,
		}
		if len(f.AllowedValues) > 0 {
			fm.AllowedValues = make([]FieldOption, 0, len(f.AllowedValues))
			for _, av := range f.AllowedValues {
				fm.AllowedValues = append(fm.AllowedValues, av.toDomain())
			}
		}
		fields = append(fields, fm)
	}
	return CreateMeta{Fields: fields}, nil
}

// CreateIssue creates a new issue and returns it. Returned Issue is populated
// from the payload plus the server-assigned key and the browse URL — it is not
// re-fetched.
func (c *Client) CreateIssue(ctx context.Context, p CreatePayload) (Issue, error) {
	if p.ProjectKey == "" {
		return Issue{}, errors.New("jira: project key is required")
	}
	if p.IssueTypeID == "" {
		return Issue{}, errors.New("jira: issue type id is required")
	}
	if p.Summary == "" {
		return Issue{}, errors.New("jira: summary is required")
	}
	fields := map[string]any{
		"project":   map[string]string{"key": p.ProjectKey},
		"issuetype": map[string]string{"id": p.IssueTypeID},
		"summary":   p.Summary,
	}
	if p.Description != "" {
		fields["description"] = textToADF(p.Description)
	}
	if p.Priority != "" {
		fields["priority"] = map[string]string{"id": p.Priority}
	}
	if p.Assignee != "" {
		fields["assignee"] = map[string]string{"accountId": p.Assignee}
	}
	if len(p.Labels) > 0 {
		fields["labels"] = p.Labels
	}
	if p.ParentKey != "" {
		fields["parent"] = map[string]string{"key": p.ParentKey}
	}
	maps.Copy(fields, p.Fields)
	body := map[string]any{"fields": fields}
	var resp createIssueResp
	if err := c.do(ctx, http.MethodPost, "/rest/api/3/issue", body, &resp); err != nil {
		return Issue{}, err
	}
	return Issue{
		Key:     resp.Key,
		Summary: p.Summary,
		Type:    IssueType{ID: p.IssueTypeID},
		URL:     c.browseURL(resp.Key),
	}, nil
}
