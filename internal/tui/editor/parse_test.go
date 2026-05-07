package editor

import "testing"

func TestSplitSummaryBody(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantSummary string
		wantBody    string
	}{
		{
			name:        "empty input",
			in:          "",
			wantSummary: "",
			wantBody:    "",
		},
		{
			name:        "banner only",
			in:          "<!-- ripjira: edit ABC-1 — :wq to apply, :cq to cancel. -->\n",
			wantSummary: "",
			wantBody:    "",
		},
		{
			name:        "banner + h1 + body",
			in:          "<!-- ripjira: hi -->\n# Hello\n\nbody line one\nbody line two\n",
			wantSummary: "Hello",
			wantBody:    "body line one\nbody line two",
		},
		{
			name:        "no banner",
			in:          "# Hello\n\nbody\n",
			wantSummary: "Hello",
			wantBody:    "body",
		},
		{
			name:        "no h1 — full content is body, summary stays untouched",
			in:          "this is just body\nwith two lines\n",
			wantSummary: "",
			wantBody:    "this is just body\nwith two lines",
		},
		{
			name:        "h1 with no blank line below",
			in:          "# Title\nimmediate body\n",
			wantSummary: "Title",
			wantBody:    "immediate body",
		},
		{
			name:        "h1 with multiple blank lines below",
			in:          "# Title\n\n\nbody\n",
			wantSummary: "Title",
			wantBody:    "body",
		},
		{
			name:        "empty H1 (just '# ') treated as no H1",
			in:          "# \n\nbody\n",
			wantSummary: "",
			wantBody:    "# \n\nbody",
		},
		{
			name:        "CRLF line endings",
			in:          "# Title\r\n\r\nbody\r\n",
			wantSummary: "Title",
			wantBody:    "body",
		},
		{
			name:        "trailing whitespace trimmed",
			in:          "# Title\n\nbody\n\n   \n",
			wantSummary: "Title",
			wantBody:    "body",
		},
		{
			name:        "h1 in middle (first non-blank is body) → no summary",
			in:          "intro line\n\n# Not a summary\nmore\n",
			wantSummary: "",
			wantBody:    "intro line\n\n# Not a summary\nmore",
		},
		{
			name:        "h1 with surrounding whitespace in heading text",
			in:          "#   Padded   \n\nbody\n",
			wantSummary: "Padded",
			wantBody:    "body",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotSum, gotBody := SplitSummaryBody(tc.in)
			if gotSum != tc.wantSummary {
				t.Errorf("summary: got %q want %q", gotSum, tc.wantSummary)
			}
			if gotBody != tc.wantBody {
				t.Errorf("body: got %q want %q", gotBody, tc.wantBody)
			}
		})
	}
}
