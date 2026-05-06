package jira

import (
	"reflect"
	"testing"
)

func TestMarkdownToADF_EmptyDoc(t *testing.T) {
	got := markdownToADF("   \n  ")
	if got.Type != "doc" || got.Version != 1 {
		t.Fatalf("envelope: %+v", got)
	}
	if len(got.Content) != 0 {
		t.Fatalf("blank input should have 0 blocks, got %d", len(got.Content))
	}
}

func TestMarkdownToADF_Paragraph(t *testing.T) {
	got := markdownToADF("hello world")
	if len(got.Content) != 1 || got.Content[0].Type != "paragraph" {
		t.Fatalf("blocks: %+v", got.Content)
	}
	if got.Content[0].Content[0].Text != "hello world" {
		t.Fatalf("text: %+v", got.Content[0])
	}
}

func TestMarkdownToADF_Heading(t *testing.T) {
	got := markdownToADF("## Heading two\n\nbody")
	if len(got.Content) != 2 {
		t.Fatalf("blocks: %d, want 2", len(got.Content))
	}
	h := got.Content[0]
	if h.Type != "heading" || h.Attrs["level"] != 2 {
		t.Fatalf("heading: %+v", h)
	}
	if h.Content[0].Text != "Heading two" {
		t.Fatalf("heading text: %q", h.Content[0].Text)
	}
	if got.Content[1].Type != "paragraph" {
		t.Fatalf("second block: %+v", got.Content[1])
	}
}

func TestMarkdownToADF_BulletList(t *testing.T) {
	got := markdownToADF("- one\n- two\n- three")
	if len(got.Content) != 1 || got.Content[0].Type != "bulletList" {
		t.Fatalf("blocks: %+v", got.Content)
	}
	items := got.Content[0].Content
	if len(items) != 3 {
		t.Fatalf("items: %d, want 3", len(items))
	}
	for i, label := range []string{"one", "two", "three"} {
		if items[i].Type != "listItem" {
			t.Errorf("item %d type: %s", i, items[i].Type)
		}
		para := items[i].Content[0]
		if para.Type != "paragraph" || para.Content[0].Text != label {
			t.Errorf("item %d: %+v", i, items[i])
		}
	}
}

func TestMarkdownToADF_OrderedList(t *testing.T) {
	got := markdownToADF("1. first\n2. second")
	if len(got.Content) != 1 || got.Content[0].Type != "orderedList" {
		t.Fatalf("blocks: %+v", got.Content)
	}
	if len(got.Content[0].Content) != 2 {
		t.Fatalf("items: %d", len(got.Content[0].Content))
	}
}

func TestMarkdownToADF_FencedCode(t *testing.T) {
	got := markdownToADF("```go\nfmt.Println(\"hi\")\n```")
	if len(got.Content) != 1 {
		t.Fatalf("blocks: %d", len(got.Content))
	}
	cb := got.Content[0]
	if cb.Type != "codeBlock" || cb.Attrs["language"] != "go" {
		t.Fatalf("codeBlock: %+v", cb)
	}
	if cb.Content[0].Text != "fmt.Println(\"hi\")" {
		t.Fatalf("body: %q", cb.Content[0].Text)
	}
}

func TestMarkdownToADF_InlineMarks(t *testing.T) {
	got := markdownToADF("**bold** and *em* and `code` and [text](http://x)")
	if len(got.Content) != 1 {
		t.Fatalf("blocks: %d", len(got.Content))
	}
	nodes := got.Content[0].Content
	want := []struct {
		text  string
		marks []string
	}{
		{"bold", []string{"strong"}},
		{" and ", nil},
		{"em", []string{"em"}},
		{" and ", nil},
		{"code", []string{"code"}},
		{" and ", nil},
		{"text", []string{"link"}},
	}
	if len(nodes) != len(want) {
		t.Fatalf("nodes = %d, want %d: %+v", len(nodes), len(want), nodes)
	}
	for i, w := range want {
		if nodes[i].Text != w.text {
			t.Errorf("node %d text = %q, want %q", i, nodes[i].Text, w.text)
		}
		gotMarks := []string{}
		for _, m := range nodes[i].Marks {
			gotMarks = append(gotMarks, m.Type)
		}
		if !reflect.DeepEqual(gotMarks, w.marks) && (len(gotMarks) != 0 || w.marks != nil) {
			t.Errorf("node %d marks = %v, want %v", i, gotMarks, w.marks)
		}
	}
}

func TestMarkdownToADF_LinkPreservesHref(t *testing.T) {
	got := markdownToADF("see [docs](https://example.com)")
	nodes := got.Content[0].Content
	for _, n := range nodes {
		for _, m := range n.Marks {
			if m.Type == "link" {
				if m.Attrs["href"] != "https://example.com" {
					t.Fatalf("link href = %v", m.Attrs)
				}
				return
			}
		}
	}
	t.Fatal("no link mark found")
}
