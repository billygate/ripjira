package jira

import (
	"strings"
	"unicode"
)

// markdownToADF converts a CommonMark-ish markdown string into an ADF
// document. Supported block constructs: paragraphs, ATX headings (`#` to
// `######`), fenced code blocks (```), unordered lists (`-` / `*`),
// ordered lists (`1.`). Inline marks supported: `**strong**`, `*em*` /
// `_em_`, `` `code` ``, `[text](url)`. Unrecognised constructs fall
// through as plain text — round-tripping is not perfect, but the common
// shapes survive submit→fetch.
//
// The implementation is hand-rolled rather than using a full CommonMark
// parser because ripjira only needs to round-trip its own htmlToMarkdown
// output; pulling in goldmark + a custom renderer was disproportionate.
func markdownToADF(s string) ADF {
	doc := ADF{Type: "doc", Version: 1, Content: []ADFNode{}}
	if strings.TrimSpace(s) == "" {
		return doc
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")

	for i := 0; i < len(lines); {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Skip blank lines between blocks.
		if trimmed == "" {
			i++
			continue
		}

		// Fenced code block.
		if rest, ok := strings.CutPrefix(trimmed, "```"); ok {
			lang := strings.TrimSpace(rest)
			start := i + 1
			end := start
			for end < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[end]), "```") {
				end++
			}
			body := strings.Join(lines[start:end], "\n")
			node := ADFNode{
				Type:    "codeBlock",
				Content: []ADFNode{{Type: "text", Text: body}},
			}
			if lang != "" {
				node.Attrs = map[string]any{"language": lang}
			}
			doc.Content = append(doc.Content, node)
			i = end + 1
			continue
		}

		// ATX heading: 1-6 # then space.
		if h, level, ok := parseHeading(trimmed); ok {
			doc.Content = append(doc.Content, ADFNode{
				Type:    "heading",
				Attrs:   map[string]any{"level": level},
				Content: parseInline(h),
			})
			i++
			continue
		}

		// Unordered list block: consecutive lines starting with "- " or "* ".
		if isBullet(trimmed) {
			items := []ADFNode{}
			for i < len(lines) {
				lt := strings.TrimSpace(lines[i])
				if lt == "" || !isBullet(lt) {
					break
				}
				body := strings.TrimSpace(lt[2:])
				items = append(items, ADFNode{
					Type: "listItem",
					Content: []ADFNode{
						{Type: "paragraph", Content: parseInline(body)},
					},
				})
				i++
			}
			doc.Content = append(doc.Content, ADFNode{
				Type:    "bulletList",
				Content: items,
			})
			continue
		}

		// Ordered list block: consecutive lines starting with "<digits>. ".
		if isOrdered(trimmed) {
			items := []ADFNode{}
			for i < len(lines) {
				lt := strings.TrimSpace(lines[i])
				if lt == "" || !isOrdered(lt) {
					break
				}
				_, body := splitOrdered(lt)
				items = append(items, ADFNode{
					Type: "listItem",
					Content: []ADFNode{
						{Type: "paragraph", Content: parseInline(body)},
					},
				})
				i++
			}
			doc.Content = append(doc.Content, ADFNode{
				Type:    "orderedList",
				Content: items,
			})
			continue
		}

		// Paragraph: read until blank line.
		para := []string{line}
		i++
		for i < len(lines) {
			lt := strings.TrimSpace(lines[i])
			if lt == "" {
				break
			}
			// Stop a paragraph if a heading / list / code-fence starts.
			if _, _, ok := parseHeading(lt); ok {
				break
			}
			if isBullet(lt) || isOrdered(lt) || strings.HasPrefix(lt, "```") {
				break
			}
			para = append(para, lines[i])
			i++
		}
		joined := strings.Join(para, "\n")
		doc.Content = append(doc.Content, ADFNode{
			Type:    "paragraph",
			Content: parseInline(joined),
		})
	}
	return doc
}

func parseHeading(s string) (string, int, bool) {
	if !strings.HasPrefix(s, "#") {
		return "", 0, false
	}
	level := 0
	for level < len(s) && s[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(s) || s[level] != ' ' {
		return "", 0, false
	}
	return strings.TrimSpace(s[level+1:]), level, true
}

func isBullet(s string) bool {
	return (strings.HasPrefix(s, "- ") || strings.HasPrefix(s, "* ")) && len(s) > 2
}

func isOrdered(s string) bool {
	_, body := splitOrdered(s)
	return body != ""
}

// splitOrdered returns the text body when s is "<digits>. <body>", else "".
func splitOrdered(s string) (digits, body string) {
	end := 0
	for end < len(s) && unicode.IsDigit(rune(s[end])) {
		end++
	}
	if end == 0 {
		return "", ""
	}
	if end+1 >= len(s) || s[end] != '.' || s[end+1] != ' ' {
		return "", ""
	}
	return s[:end], strings.TrimSpace(s[end+2:])
}

// parseInline turns a chunk of text into a sequence of ADF text nodes,
// applying inline marks for **strong**, *em* / _em_, `code`, and
// [link](url). Hard line breaks (within a paragraph) are emitted as
// hardBreak nodes between segments. Unrecognised markup is left as
// literal text.
//
// The parser is single-pass and greedy, scanning left-to-right. It does
// NOT support nested marks (e.g. **bold *and* italic**) — the inner
// marker is treated as literal characters. That is a deliberate scope
// cut; nested parsing requires a real grammar and ripjira's MVP doesn't
// need it.
func parseInline(text string) []ADFNode {
	if text == "" {
		return []ADFNode{}
	}
	out := []ADFNode{}
	for _, line := range splitWithSeparators(text, "\n") {
		if line == "\n" {
			out = append(out, ADFNode{Type: "hardBreak"})
			continue
		}
		out = append(out, parseInlineLine(line)...)
	}
	return out
}

// splitWithSeparators returns the slice {chunk, sep, chunk, sep, …},
// keeping separator entries literal so the caller can replace them with
// dedicated nodes.
func splitWithSeparators(s, sep string) []string {
	out := []string{}
	for {
		idx := strings.Index(s, sep)
		if idx < 0 {
			if s != "" {
				out = append(out, s)
			}
			return out
		}
		if idx > 0 {
			out = append(out, s[:idx])
		}
		out = append(out, sep)
		s = s[idx+len(sep):]
	}
}

func parseInlineLine(s string) []ADFNode {
	out := []ADFNode{}
	pending := strings.Builder{}
	flush := func() {
		if pending.Len() > 0 {
			out = append(out, ADFNode{Type: "text", Text: pending.String()})
			pending.Reset()
		}
	}
	i := 0
	for i < len(s) {
		// **strong**
		if i+1 < len(s) && s[i] == '*' && s[i+1] == '*' {
			if end := strings.Index(s[i+2:], "**"); end >= 0 {
				body := s[i+2 : i+2+end]
				if body != "" {
					flush()
					out = append(out, ADFNode{
						Type: "text", Text: body,
						Marks: []ADFMark{{Type: "strong"}},
					})
					i += 2 + end + 2
					continue
				}
			}
		}
		// *em* or _em_ — single-char delimiters. Use a small helper so we
		// don't double the logic for the two characters.
		if s[i] == '*' || s[i] == '_' {
			delim := s[i]
			// Avoid eating list-marker or word boundary cases: require the
			// next char to be non-space and the closing delim to also abut
			// non-space.
			if i+1 < len(s) && s[i+1] != ' ' && s[i+1] != delim {
				// Find a closing delim that isn't a doubled "**" (handled above).
				j := i + 1
				for j < len(s) {
					if s[j] == delim {
						break
					}
					j++
				}
				if j < len(s) && j > i+1 && s[j-1] != ' ' {
					body := s[i+1 : j]
					flush()
					out = append(out, ADFNode{
						Type: "text", Text: body,
						Marks: []ADFMark{{Type: "em"}},
					})
					i = j + 1
					continue
				}
			}
		}
		// `code`
		if s[i] == '`' {
			if end := strings.Index(s[i+1:], "`"); end > 0 {
				body := s[i+1 : i+1+end]
				flush()
				out = append(out, ADFNode{
					Type: "text", Text: body,
					Marks: []ADFMark{{Type: "code"}},
				})
				i += 1 + end + 1
				continue
			}
		}
		// [text](url) — emit text with a link mark.
		if s[i] == '[' {
			if close1 := strings.Index(s[i+1:], "]"); close1 > 0 {
				rest := s[i+1+close1+1:]
				if strings.HasPrefix(rest, "(") {
					if close2 := strings.Index(rest, ")"); close2 > 0 {
						label := s[i+1 : i+1+close1]
						href := rest[1:close2]
						flush()
						out = append(out, ADFNode{
							Type: "text", Text: label,
							Marks: []ADFMark{{
								Type:  "link",
								Attrs: map[string]any{"href": href},
							}},
						})
						i += 1 + close1 + 1 + close2 + 1
						continue
					}
				}
			}
		}
		pending.WriteByte(s[i])
		i++
	}
	flush()
	return out
}
