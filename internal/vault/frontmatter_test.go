package vault_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name string
		src  string
		// none: SplitFrontmatter must return nil, nil.
		none bool
		// wantErr: substring the error must contain. Empty means success.
		wantErr string

		wantType  string // value of the type key; "" means the key must be absent
		wantBody  string // src[BodyOffset:]
		wantLine  int    // BodyLine; 0 means not checked
		wantCRLF  bool
		checkBody bool // wantBody is checked even when empty
	}{
		{name: "standard", src: "---\ntype: x\n---\nbody\n", wantType: "x", wantBody: "body\n", wantLine: 4, checkBody: true},
		{name: "none", src: "# hi\n", none: true},
		{name: "crlf", src: "---\r\ntype: x\r\n---\r\nb", wantType: "x", wantBody: "b", wantCRLF: true, checkBody: true},
		{name: "bom", src: "\xef\xbb\xbf---\ntype: x\n---\nb\n", wantType: "x", wantBody: "b\n", checkBody: true},
		{name: "unterminated", src: "---\ntype: x\nbody", wantErr: "frontmatter not closed"},
		{name: "empty", src: "---\n---\nb", wantBody: "b", checkBody: true},
		{name: "not_mapping", src: "---\n- a\n---\n", wantErr: "must be a mapping"},
		{name: "duplicate_key", src: "---\na: 1\na: 2\n---\n", wantErr: "duplicate"},
		{name: "leading_blank_line", src: "\n---\ntype: x\n---\nb\n", none: true},
		{name: "four_dashes", src: "----\ntype: x\n----\nb\n", none: true},
		{name: "closing_trailing_space", src: "---\ntype: x\n--- \nbody", wantType: "x", wantBody: "body", checkBody: true},
		{
			name:     "hr_in_body",
			src:      "---\ntype: x\n---\nbody\n---\nmore\n---\n",
			wantType: "x", wantBody: "body\n---\nmore\n---\n", checkBody: true,
		},
		{name: "no_trailing_newline", src: "---\ntype: x\n---", wantType: "x", wantBody: "", checkBody: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fm, err := vault.SplitFrontmatter([]byte(tc.src))
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("want error containing %q, got frontmatter %+v", tc.wantErr, fm)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("SplitFrontmatter: %v", err)
			}
			if tc.none {
				if fm != nil {
					t.Fatalf("want nil frontmatter, got %+v", fm)
				}
				return
			}
			if fm == nil {
				t.Fatal("got nil frontmatter, want one")
			}
			if fm.Map == nil || fm.Map.Kind != yaml.MappingNode {
				t.Fatalf("Map = %+v, want a mapping node", fm.Map)
			}
			got, has := mapValue(fm.Map, "type")
			if tc.wantType == "" && has {
				t.Errorf("type = %q, want it absent", got)
			}
			if tc.wantType != "" && got != tc.wantType {
				t.Errorf("type = %q, want %q", got, tc.wantType)
			}
			if fm.BodyOffset < 0 || fm.BodyOffset > len(tc.src) {
				t.Fatalf("BodyOffset = %d is outside the %d-byte input", fm.BodyOffset, len(tc.src))
			}
			if tc.checkBody {
				if body := tc.src[fm.BodyOffset:]; body != tc.wantBody {
					t.Errorf("body = %q, want %q", body, tc.wantBody)
				}
			}
			if tc.wantLine != 0 && fm.BodyLine != tc.wantLine {
				t.Errorf("BodyLine = %d, want %d", fm.BodyLine, tc.wantLine)
			}
			if fm.CRLF != tc.wantCRLF {
				t.Errorf("CRLF = %v, want %v", fm.CRLF, tc.wantCRLF)
			}
		})
	}
}

// Invalid YAML must be reported with the line in the FILE (not in the
// frontmatter block), so a human can jump straight to it.
func TestSplitFrontmatterInvalidYAML(t *testing.T) {
	// Line 1 is the fence, line 2 is fine, the tab on line 3 is the error.
	src := "---\ntype: x\n\tbad: 1\n---\nbody\n"
	fm, err := vault.SplitFrontmatter([]byte(src))
	if err == nil {
		t.Fatalf("want an error, got %+v", fm)
	}
	if !strings.Contains(err.Error(), "line 3") {
		t.Errorf("error = %q, want it to name file line 3", err)
	}
}

// yaml.v3 numbers some parse errors from 0 and some from 1, and its line is
// used as is, so the reported line is the bad one or the one before it, in
// the FILE. Line 1 of each source is the opening fence.
func TestSplitFrontmatterErrorLine(t *testing.T) {
	tests := []struct {
		name, src string
		line      int
	}{
		{"tab_indent", "---\ntype: x\n\tbad: 1\n---\n", 3},
		{"unterminated_flow_list", "---\na: 1\nb: [1\n---\n", 3},
		{"unterminated_flow_list_then_keys", "---\na: 1\nb: [1\nc: 2\nd: 3\n---\n", 3},
		{"unterminated_flow_list_on_first_key", "---\nb: [1\n---\n", 2},
		{"unterminated_flow_map", "---\na: 1\nb: {x: 1\n---\n", 3},
		{"unterminated_quote", "---\na: 1\nb: \"open\n---\n", 3},
		{"double_colon", "---\na: 1\nb: : x\n---\n", 3},
		{"multi_line_flow_then_error", "---\nf: [a,\n  b]\nc: : x\n---\n", 4},
		{"unterminated_flow_after_a_valid_multi_line_one", "---\nf: [a,\n  b]\ng: 1\nh: [1\n---\n", 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := vault.SplitFrontmatter([]byte(tc.src))
			if err == nil {
				t.Fatal("want an error")
			}
			m := regexp.MustCompile(`near line (\d+)\b`).FindStringSubmatch(err.Error())
			if m == nil {
				t.Fatalf("error = %q, want it to name a line", err)
			}
			if n, _ := strconv.Atoi(m[1]); n < tc.line-1 || n > tc.line {
				t.Errorf("error = %q, want it to name line %d or %d", err, tc.line-1, tc.line)
			}
			if regexp.MustCompile(`line \d+.*line \d+`).MatchString(err.Error()) {
				t.Errorf("error = %q names more than one line", err)
			}
			if !strings.Contains(err.Error(), "fix") {
				t.Errorf("error = %q, want it to say what to do next", err)
			}
		})
	}
}
