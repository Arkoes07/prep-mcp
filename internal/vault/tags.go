package vault

import (
	"errors"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// NormalizeTags reads the value of a tags: key. It accepts a YAML list, a
// comma-separated string (optionally in brackets), or a single scalar, and
// returns lowercase, trimmed, de-duplicated tags in first-seen order. Nested
// lists and maps are ignored.
func NormalizeTags(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	var raw []string
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" {
			return nil
		}
		s := strings.TrimSpace(n.Value)
		if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
			s = s[1 : len(s)-1]
		}
		for part := range strings.SplitSeq(s, ",") {
			raw = append(raw, strings.Trim(strings.TrimSpace(part), `"'`))
		}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && c.Tag != "!!null" {
				raw = append(raw, c.Value)
			}
		}
	}
	return NormalizeTagList(raw)
}

// NormalizeTagList lowercases and trims tags, drops empty ones and removes
// duplicates, keeping the first.
func NormalizeTagList(in []string) []string {
	var out []string
	for _, t := range in {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	return out
}

// MatchTag reports whether tag matches pattern. A trailing "/" makes the
// pattern a prefix ("issue/" matches "issue/x"); otherwise it is an exact match.
func MatchTag(pattern, tag string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	t := strings.ToLower(strings.TrimSpace(tag))
	if p == "" {
		return false
	}
	if strings.HasSuffix(p, "/") {
		return strings.HasPrefix(t, p)
	}
	return p == t
}

// Vocabulary is the controlled tag list from the tags file.
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

// Vocabulary loads the tags file named in the config. It returns nil, nil
// when no tags file is configured or the file does not exist.
func (v *Vault) Vocabulary() (*Vocabulary, error) {
	if v.cfg.TagsFile == "" {
		return nil, nil
	}
	src, err := v.root.ReadFile(v.cfg.TagsFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
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

// Closest returns the vocabulary tag with the smallest edit distance to tag,
// the alphabetically first on a tie, or "" for an empty vocabulary.
func (voc *Vocabulary) Closest(tag string) string {
	if voc == nil {
		return ""
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	best, bestDist := "", -1
	for _, cand := range voc.list {
		if d := levenshtein(tag, cand); bestDist < 0 || d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
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
