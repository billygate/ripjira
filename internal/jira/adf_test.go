package jira

import (
	"reflect"
	"testing"
)

func TestTextToADF(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want ADF
	}{
		{
			name: "empty",
			in:   "",
			want: ADF{Type: "doc", Version: 1, Content: []ADFNode{}},
		},
		{
			name: "only whitespace",
			in:   "   \n\t  ",
			want: ADF{Type: "doc", Version: 1, Content: []ADFNode{}},
		},
		{
			name: "single line",
			in:   "Hello world",
			want: ADF{Type: "doc", Version: 1, Content: []ADFNode{
				{Type: "paragraph", Content: []ADFNode{
					{Type: "text", Text: "Hello world"},
				}},
			}},
		},
		{
			name: "multi paragraph",
			in:   "p1\n\np2\n\np3",
			want: ADF{Type: "doc", Version: 1, Content: []ADFNode{
				{Type: "paragraph", Content: []ADFNode{{Type: "text", Text: "p1"}}},
				{Type: "paragraph", Content: []ADFNode{{Type: "text", Text: "p2"}}},
				{Type: "paragraph", Content: []ADFNode{{Type: "text", Text: "p3"}}},
			}},
		},
		{
			name: "mixed line breaks and paragraphs",
			in:   "line1\nline2\n\npara2",
			want: ADF{Type: "doc", Version: 1, Content: []ADFNode{
				{Type: "paragraph", Content: []ADFNode{
					{Type: "text", Text: "line1"},
					{Type: "hardBreak"},
					{Type: "text", Text: "line2"},
				}},
				{Type: "paragraph", Content: []ADFNode{{Type: "text", Text: "para2"}}},
			}},
		},
		{
			name: "leading newline within paragraph drops empty text",
			in:   "\nfoo",
			want: ADF{Type: "doc", Version: 1, Content: []ADFNode{
				{Type: "paragraph", Content: []ADFNode{
					{Type: "hardBreak"},
					{Type: "text", Text: "foo"},
				}},
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := textToADF(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("textToADF mismatch\n got: %#v\nwant: %#v", got, tc.want)
			}
		})
	}
}
