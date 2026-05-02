package jira

import (
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
)

// htmlToMarkdown converts the HTML fragment Jira returns in `renderedFields`
// into Markdown, preserving headings, lists, code blocks, links, and inline
// emphasis. Whitespace-only or empty input yields "". On converter error the
// input is returned unchanged so callers always receive a non-empty body when
// the source had one.
func htmlToMarkdown(s string) string {
	if strings.TrimSpace(s) == "" {
		return ""
	}
	md, err := htmltomarkdown.ConvertString(s)
	if err != nil {
		return s
	}
	return strings.TrimSpace(md)
}
