package vault_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

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
		if got := voc.FindClosest("a/b"); got != "" {
			t.Errorf("FindClosest on an empty vocabulary = %q, want empty", got)
		}
	})
}

func TestFindClosest(t *testing.T) {
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
			if got := tc.voc.FindClosest(tc.in); got != tc.want {
				t.Errorf("FindClosest(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	t.Run("known_is_normalised", func(t *testing.T) {
		if !example.Known("Q/FAILURE") {
			t.Error("Known(Q/FAILURE) = false, want true after normalising")
		}
	})
}
