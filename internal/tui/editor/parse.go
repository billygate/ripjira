// Package editor invokes the user's $EDITOR on a markdown buffer combining
// an issue's summary (as # H1) with its description body, then parses the
// edited content back into the two fields.
package editor

import (
	"regexp"
	"strings"
)

// bannerRE matches a single leading HTML comment whose body starts with
// "ripjira:" — these are the instructional banners we prepend to the temp
// file. Only the first such block is stripped.
var bannerRE = regexp.MustCompile(`(?s)\A\s*<!--\s*ripjira:.*?-->\s*`)

// h1RE matches a level-1 ATX heading line and captures the heading text.
// We require at least one non-space character after the `# ` so that an
// empty heading ("# ") is not treated as a summary.
var h1RE = regexp.MustCompile(`^#\s+(\S.*?)\s*$`)

// SplitSummaryBody parses the contents of an editor buffer back into a
// summary string and a body string.
//
// Rules:
//
//  1. Strip any leading <!-- ripjira: ... --> comment block.
//  2. Normalise CRLF to LF.
//  3. Find the first non-blank line. If it matches `^#\s+(\S.*?)\s*$` (a
//     `#`, one or more spaces, then non-empty heading text), the captured
//     text — trimmed — is the summary; everything after the first blank
//     line below the heading (or immediately after the heading if no blank
//     line) is the body.
//  4. Otherwise the summary is empty (caller leaves the existing summary
//     untouched) and the entire content is the body.
//  5. Trailing whitespace is trimmed from the body.
func SplitSummaryBody(in string) (summary, body string) {
	in = strings.ReplaceAll(in, "\r\n", "\n")
	in = bannerRE.ReplaceAllString(in, "")
	lines := strings.Split(in, "\n")

	firstIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			firstIdx = i
			break
		}
	}
	if firstIdx == -1 {
		return "", ""
	}

	if m := h1RE.FindStringSubmatch(lines[firstIdx]); m != nil {
		sum := strings.TrimSpace(m[1])
		bodyStart := firstIdx + 1
		for bodyStart < len(lines) && strings.TrimSpace(lines[bodyStart]) == "" {
			bodyStart++
		}
		bodyText := strings.Join(lines[bodyStart:], "\n")
		return sum, strings.TrimRight(bodyText, " \t\n")
	}

	bodyText := strings.Join(lines[firstIdx:], "\n")
	return "", strings.TrimRight(bodyText, " \t\n")
}
