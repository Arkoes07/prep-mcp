package vault_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// tagsNode parses "tags: <value>" and returns the value node.
func tagsNode(t *testing.T, value string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("tags: "+value+"\n"), &doc); err != nil {
		t.Fatalf("bad test YAML %q: %v", value, err)
	}
	return doc.Content[0].Content[1]
}

func TestNormalizeTags(t *testing.T) {
	tests := []struct {
		name  string
		value string // YAML text of the tags value
		want  []string
	}{
		{"list", `[a, b]`, []string{"a", "b"}},
		{"list_mixed_case_spaces", `[A, " B "]`, []string{"a", "b"}},
		{"comma_string", `"a, b"`, []string{"a", "b"}},
		{"comma_string_spaces", `"A,  B ,c"`, []string{"a", "b", "c"}},
		{"bracketed_string", `"[a, 'b']"`, []string{"a", "b"}},
		{"single_scalar", `a`, []string{"a"}},
		{"null", `null`, nil},
		{"empty_list", `[]`, nil},
		{"empty_entries", `"a,,b,"`, []string{"a", "b"}},
		{"dedupe_keeps_first", `[a, A, "a "]`, []string{"a"}},
		{"scalars_stringified", `[2026, true]`, []string{"2026", "true"}},
		{"nested_ignored", `[a, [b], {c: d}]`, []string{"a"}},
		{"path_like", `"Issue/Buries-Headline"`, []string{"issue/buries-headline"}},
		{"control_whitespace", `["\ta\n"]`, []string{"a"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := vault.NormalizeTags(tagsNode(t, tc.value))
			if !slices.Equal(got, tc.want) {
				t.Errorf("NormalizeTags(%s) = %q, want %q", tc.value, got, tc.want)
			}
		})
	}
	t.Run("key_missing", func(t *testing.T) {
		if got := vault.NormalizeTags(nil); len(got) != 0 {
			t.Errorf("NormalizeTags(nil) = %q, want none", got)
		}
	})
}

func TestParseVocabulary(t *testing.T) {
	t.Run("example", func(t *testing.T) {
		root := testvault.Copy(t)
		src, err := os.ReadFile(filepath.Join(root, "tags.md"))
		if err != nil {
			t.Fatal(err)
		}
		voc := vault.ParseVocabulary(src)
		for _, tag := range []string{
			"q/failure", "q/hardest-project", "q/changed-mind",
			"skill/trade-off", "skill/scale", "domain/payments",
			"issue/buries-headline", "issue/missing-tradeoff",
			"company/acme", "meta/plan",
		} {
			if !voc.Known(tag) {
				t.Errorf("Known(%q) = false, want true", tag)
			}
		}
		if voc.Known("q/nope") {
			t.Error("Known(q/nope) = true, want false")
		}
	})
	t.Run("one_per_line", func(t *testing.T) {
		voc := vault.ParseVocabulary([]byte("`a/b`\n`c/d`\n"))
		if !voc.Known("a/b") || !voc.Known("c/d") {
			t.Error("both tags should be known")
		}
	})
	t.Run("many_per_line", func(t *testing.T) {
		voc := vault.ParseVocabulary([]byte("`a/b` `c/d` `e/f`\n"))
		if !voc.Known("a/b") || !voc.Known("c/d") || !voc.Known("e/f") {
			t.Error("all three tags should be known")
		}
	})
	t.Run("no_slash_ignored", func(t *testing.T) {
		voc := vault.ParseVocabulary([]byte("`foo` `a/b`\n"))
		if voc.Known("foo") {
			t.Error("a backticked token without a slash is not a tag")
		}
		if !voc.Known("a/b") {
			t.Error("a/b should be known")
		}
	})
	t.Run("lowercased", func(t *testing.T) {
		voc := vault.ParseVocabulary([]byte("`Q/Failure`\n"))
		if !voc.Known("q/failure") {
			t.Error("Q/Failure should be stored as q/failure")
		}
	})
	t.Run("empty_file", func(t *testing.T) {
		voc := vault.ParseVocabulary(nil)
		if voc.Known("a/b") {
			t.Error("empty vocabulary should know nothing")
		}
		if got := voc.Closest("a/b"); got != "" {
			t.Errorf("Closest on an empty vocabulary = %q, want empty", got)
		}
	})
}

func TestClosest(t *testing.T) {
	root := testvault.Copy(t)
	src, err := os.ReadFile(filepath.Join(root, "tags.md"))
	if err != nil {
		t.Fatal(err)
	}
	example := vault.ParseVocabulary(src)

	tests := []struct {
		name string
		voc  *vault.Vocabulary
		in   string
		want string
	}{
		{"typo_missing_letter", example, "issue/buries-headlin", "issue/buries-headline"},
		{"typo_hyphen", example, "skill/tradeoff", "skill/trade-off"},
		{"tie_alphabetical", vault.ParseVocabulary([]byte("`a/by` `a/bx`\n")), "a/bz", "a/bx"},
		{"empty_vocabulary", vault.ParseVocabulary(nil), "a/b", ""},
		{"input_is_normalised", example, "Skill/Tradeoff", "skill/trade-off"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.voc.Closest(tc.in); got != tc.want {
				t.Errorf("Closest(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	t.Run("known_is_normalised", func(t *testing.T) {
		if !example.Known("Q/FAILURE") {
			t.Error("Known(Q/FAILURE) = false, want true after normalising")
		}
	})
}

func TestMatchTag(t *testing.T) {
	tests := []struct {
		pattern, tag string
		want         bool
	}{
		{"issue/", "issue/x", true},
		{"issue/", "issues/x", false},
		{"issue", "issue/x", false},
		{"issue/x", "issue/x", true},
		{"Issue/", "issue/x", true},
	}
	for _, tc := range tests {
		if got := vault.MatchTag(tc.pattern, tc.tag); got != tc.want {
			t.Errorf("MatchTag(%q, %q) = %v, want %v", tc.pattern, tc.tag, got, tc.want)
		}
	}
}
