package jira

import "strings"

// ADF is a minimal Atlassian Document Format document. The Jira REST API v3
// requires comment and description bodies to be ADF rather than plain text or
// wiki markup.
type ADF struct {
	Type    string    `json:"type"`
	Version int       `json:"version"`
	Content []ADFNode `json:"content"`
}

// ADFNode is a node within an ADF document. Only the small subset of fields
// ripjira produces is modelled here; unknown fields from server responses are
// ignored.
type ADFNode struct {
	Type    string         `json:"type"`
	Text    string         `json:"text,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Marks   []ADFMark      `json:"marks,omitempty"`
	Content []ADFNode      `json:"content,omitempty"`
}

// ADFMark is an inline mark such as strong, em, code, or link.
type ADFMark struct {
	Type  string         `json:"type"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// textToADF converts plain text into an ADF document. Paragraphs are split on
// blank lines (`\n\n`); single newlines within a paragraph become hardBreak
// nodes. An empty or whitespace-only input yields a document with no content.
func textToADF(s string) ADF {
	doc := ADF{Type: "doc", Version: 1, Content: []ADFNode{}}
	if strings.TrimSpace(s) == "" {
		return doc
	}
	for p := range strings.SplitSeq(s, "\n\n") {
		if p == "" {
			continue
		}
		lines := strings.Split(p, "\n")
		para := ADFNode{Type: "paragraph", Content: []ADFNode{}}
		for i, line := range lines {
			if i > 0 {
				para.Content = append(para.Content, ADFNode{Type: "hardBreak"})
			}
			if line != "" {
				para.Content = append(para.Content, ADFNode{Type: "text", Text: line})
			}
		}
		doc.Content = append(doc.Content, para)
	}
	return doc
}
