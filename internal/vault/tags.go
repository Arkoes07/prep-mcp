package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// normalizeTags reads the value node of a tags: key, with aliases already
// followed. It accepts a YAML list, a comma-separated string (optionally in
// brackets), or a single scalar, and returns lowercase, trimmed,
// de-duplicated tags in first-seen order. Nested lists and maps are ignored.
func normalizeTags(n *yaml.Node) []string {
	if n == nil {
		return []string{}
	}
	switch n.Kind {
	case yaml.SequenceNode:
		return NormalizeTagList(scalarTexts(n.Content))
	case yaml.ScalarNode:
		s := scalarText(n)
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			s = s[1 : len(s)-1]
		}
		parts := strings.Split(s, ",")
		for i, part := range parts {
			parts[i] = strings.Trim(strings.TrimSpace(part), `"'`)
		}
		return NormalizeTagList(parts)
	}
	return []string{}
}

// NormalizeTagList lowercases and trims tags, drops empty ones and removes
// duplicates, keeping the first.
func NormalizeTagList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

type Vocabulary struct {
	set  map[string]bool
	list []string // sorted
}

var vocabTokenRE = regexp.MustCompile("`([^`\\s]+)`")

// ParseVocabulary reads every backticked prefix/value token as an allowed tag,
// wherever it appears in the file.
func ParseVocabulary(src []byte) *Vocabulary {
	voc := &Vocabulary{set: map[string]bool{}}
	for _, m := range vocabTokenRE.FindAllSubmatch(src, -1) {
		tag := strings.ToLower(string(m[1]))
		prefix, value, ok := strings.Cut(tag, "/")
		if !ok || prefix == "" || value == "" || voc.set[tag] {
			continue
		}
		voc.set[tag] = true
		voc.list = append(voc.list, tag)
	}
	slices.Sort(voc.list)
	return voc
}

// LoadVocabulary loads the vault-relative tags file. It returns nil, nil when
// tagsFile is empty or the file does not exist.
func (v *Vault) LoadVocabulary(tagsFile string) (*Vocabulary, error) {
	if tagsFile == "" {
		return nil, nil
	}
	src, err := v.root.ReadFile(tagsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf(
			"read tags file %s: %w; check its permissions or fix tags_file in the config",
			tagsFile,
			err,
		)
	}
	return ParseVocabulary(src), nil
}

// Known reports whether tag is in the vocabulary. It is false for a nil or empty vocabulary.
func (voc *Vocabulary) Known(tag string) bool {
	if voc == nil {
		return false
	}
	return voc.set[strings.ToLower(strings.TrimSpace(tag))]
}

// Tags returns the vocabulary's tags, sorted. It is nil for a nil vocabulary.
func (voc *Vocabulary) Tags() []string {
	if voc == nil {
		return nil
	}
	return voc.list
}

// FindClosest returns the vocabulary tag with the smallest edit distance to tag,
// the alphabetically first on a tie, or "" for an empty vocabulary.
func (voc *Vocabulary) FindClosest(tag string) string {
	if voc == nil {
		return ""
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	best, bestDist := "", -1
	for _, cand := range voc.list {
		if d := measureEditDistance(tag, cand); bestDist < 0 || d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best
}

func measureEditDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
