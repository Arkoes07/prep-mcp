package write_test

import (
	"strings"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/write"
)

func TestInsertSection(t *testing.T) {
	tests := []struct {
		name, src, heading, content string
		want                        string   // exact result, when the plan pins it
		inOrder                     []string // otherwise: these must appear in this order
		wantErr                     string
	}{
		{name: "new_heading", src: "a\n", heading: "H", content: "c", want: "a\n\n## H\n\nc\n"},
		{name: "new_no_trailing_nl", src: "a", heading: "H", content: "c", want: "a\n\n## H\n\nc\n"},
		{name: "existing_prose", src: "## H\nold\n", heading: "H", content: "new", want: "## H\nold\n\nnew\n"},
		{name: "existing_list", src: "## Log\n- a\n", heading: "Log", content: "- b", want: "## Log\n- a\n- b\n"},
		// The other half of the rule: only a list after a list skips the blank line.
		{name: "prose_after_list", src: "## Log\n- a\n", heading: "Log", content: "note", want: "## Log\n- a\n\nnote\n"},
		{name: "list_after_prose", src: "## Log\nintro\n", heading: "Log", content: "- a", want: "## Log\nintro\n\n- a\n"},
		{name: "before_next_section", src: "## A\nx\n\n## B\ny\n", heading: "A", content: "z", want: "## A\nx\n\nz\n\n## B\ny\n"},
		{name: "empty_section", src: "## A\n\n## B\n", heading: "A", content: "z", want: "## A\n\nz\n\n## B\n"},
		{name: "subsections_included", src: "## A\nx\n### A1\ny\n## B\n", heading: "A", content: "z", inOrder: []string{"### A1\ny\n", "z\n", "## B\n"}},
		{name: "h1_terminates", src: "## A\nx\n# T\n", heading: "A", content: "z", inOrder: []string{"## A\nx\n", "z\n", "# T\n"}},
		{name: "no_prefix_match", src: "## Feedback notes\nn\n", heading: "Feedback", content: "z", want: "## Feedback notes\nn\n\n## Feedback\n\nz\n"},
		{name: "other_level", src: "### H\n", heading: "H", content: "z", want: "### H\n\n## H\n\nz\n"},
		{name: "in_code_fence", src: "```\n## H\n```\n", heading: "H", content: "z", want: "```\n## H\n```\n\n## H\n\nz\n"},
		{name: "duplicate_uses_last", src: "## H\none\n\n## H\ntwo\n", heading: "H", content: "z", want: "## H\none\n\n## H\ntwo\n\nz\n"},
		{name: "trailing_ws_heading", src: "## H  \nold\n", heading: "H", content: "z", want: "## H  \nold\n\nz\n"},
		{name: "heading_normalised", src: "## H\nold\n", heading: "## H", content: "z", want: "## H\nold\n\nz\n"},
		{name: "heading_newline", src: "a\n", heading: "a\nb", content: "c", wantErr: "heading"},
		{name: "content_trailing_nls", src: "a\n", heading: "H", content: "c\n\n\n", want: "a\n\n## H\n\nc\n"},
		{name: "empty_content", src: "a\n", heading: "H", content: "", wantErr: "content is empty"},
		{name: "crlf", src: "## H\r\nold\r\n", heading: "H", content: "new\nmore", want: "## H\r\nold\r\n\r\nnew\r\nmore\r\n"},
		// A new heading in a CRLF file.
		{name: "crlf_new_heading", src: "a\r\n", heading: "H", content: "c", want: "a\r\n\r\n## H\r\n\r\nc\r\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := write.InsertSection([]byte(tc.src), tc.heading, tc.content)
			if tc.wantErr != "" {
				mustContain(t, err, tc.wantErr)
				return
			}
			if err != nil {
				t.Fatalf("InsertSection: %v", err)
			}
			out := string(got)
			if tc.want != "" && out != tc.want {
				t.Errorf("result:\n%q\nwant:\n%q", out, tc.want)
			}
			at := 0
			for _, frag := range tc.inOrder {
				i := strings.Index(out[at:], frag)
				if i < 0 {
					t.Fatalf("result %q: want %q after offset %d", out, frag, at)
				}
				at += i + len(frag)
			}
			onlyInserted(t, tc.src, out)
		})
	}
}

// onlyInserted checks that out is src with exactly one contiguous block added:
// everything before the insertion point and everything after it is unchanged.
func onlyInserted(t *testing.T, src, out string) {
	t.Helper()
	p := 0
	for p < len(src) && p < len(out) && src[p] == out[p] {
		p++
	}
	s := 0
	for s < len(src)-p && s < len(out)-p && src[len(src)-1-s] == out[len(out)-1-s] {
		s++
	}
	if p+s != len(src) {
		t.Errorf("existing text was changed, not just added to:\nsrc %q\nout %q", src, out)
	}
}
