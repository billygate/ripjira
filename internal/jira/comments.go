package jira

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// AddComment posts a new comment on the issue. The body is plain text and is
// converted to ADF before being sent.
func (c *Client) AddComment(ctx context.Context, key, body string) error {
	if key == "" {
		return errors.New("jira: issue key is required")
	}
	payload := map[string]any{"body": textToADF(body)}
	path := "/rest/api/3/issue/" + url.PathEscape(key) + "/comment"
	return c.do(ctx, http.MethodPost, path, payload, nil)
}
