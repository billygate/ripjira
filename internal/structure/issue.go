package structure

import "strings"

// Issue is the minimal accessor the evaluator needs. UI code passes an
// adapter that maps logical field names ("status", "priority", "assignee",
// "labels", …) to their string representation. Multi-value fields (labels)
// are joined with ", " and split back out by splitFieldValue.
type Issue interface {
	Field(name string) string
}

// splitFieldValue splits comma-separated multi-value fields, trimming
// whitespace and dropping empty parts. Single-value fields produce a
// one-element slice; empty input returns nil.
func splitFieldValue(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := parts[:0]
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
