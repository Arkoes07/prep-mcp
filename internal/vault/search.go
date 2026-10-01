package vault

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"time"
)

const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 500
	// MaxHitLines is how many matching lines a hit reports; Total counts all.
	MaxHitLines = 3
)

// Hit is one document that matched a search.
type Hit struct {
	Path, Title string
	Lines       []Line // at most MaxHitLines, in file order
	Total       int    // all matching lines
}

// Line is one matching file line: 1-based number and the verbatim text.
type Line struct {
	N    int
	Text string
}

// Search finds documents whose body or title field contains q, ignoring case.
// Other frontmatter fields and file names are not searched. Hits are ordered
// by path. typ, when set, limits the search to one type.
func (v *Vault) Search(q, typ string, limit int) ([]Hit, error) {
	if strings.TrimSpace(q) == "" {
		return nil, errors.New("query is empty; pass the text to search for")
	}
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	limit = min(limit, MaxSearchLimit)
	needle := strings.ToLower(q)

	docs, _, err := v.Scan()
	if err != nil {
		return nil, err
	}
	var hits []Hit
	for _, d := range docs {
		if typ != "" && d.Type != typ {
			continue
		}
		var lines []Line
		if d.titleLine > 0 && strings.Contains(strings.ToLower(d.Title), needle) {
			lines = append(lines, Line{N: d.titleLine, Text: d.line(d.titleLine)})
		}
		for i, text := range strings.Split(string(d.Body), "\n") {
			text = strings.TrimSuffix(text, "\r")
			if strings.Contains(strings.ToLower(text), needle) {
				lines = append(lines, Line{N: d.BodyLine + i, Text: text})
			}
		}
		if len(lines) == 0 {
			continue
		}
		h := Hit{Path: d.Path, Title: d.Title, Total: len(lines), Lines: lines}
		if len(h.Lines) > MaxHitLines {
			h.Lines = h.Lines[:MaxHitLines]
		}
		hits = append(hits, h)
		if len(hits) == limit {
			break
		}
	}
	return hits, nil
}

// line returns the verbatim text of a 1-based line of the scanned file,
// without its line ending.
func (d *Doc) line(n int) string {
	lines := bytes.Split(d.src, []byte("\n"))
	if n < 1 || n > len(lines) {
		return ""
	}
	return strings.TrimSuffix(string(lines[n-1]), "\r")
}

// TagCount is one tag with its usage.
type TagCount struct {
	Tag     string
	Count   int       // documents carrying the tag
	Last    time.Time // newest document date
	Unknown bool      // not in the tags file (never set without one)
}

// TagStats counts tags over typed documents, most used first, then by name.
// prefix limits it to tags starting with it; since drops documents dated
// before that day.
func (v *Vault) TagStats(prefix string, since *time.Time) ([]TagCount, error) {
	docs, _, err := v.Scan()
	if err != nil {
		return nil, err
	}
	voc, err := v.Vocabulary()
	if err != nil {
		return nil, err
	}
	return tagStats(docs, voc, prefix, since), nil
}

func tagStats(docs []*Doc, voc *Vocabulary, prefix string, since *time.Time) []TagCount {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	byTag := map[string]*TagCount{}
	for _, d := range docs {
		if since != nil && dayOf(d.Date).Before(dayOf(*since)) {
			continue
		}
		for _, tag := range d.Tags {
			if !strings.HasPrefix(tag, prefix) {
				continue
			}
			tc := byTag[tag]
			if tc == nil {
				tc = &TagCount{Tag: tag, Unknown: voc != nil && !voc.Known(tag)}
				byTag[tag] = tc
			}
			tc.Count++
			if d.Date.After(tc.Last) {
				tc.Last = d.Date
			}
		}
	}
	out := make([]TagCount, 0, len(byTag))
	for _, tc := range byTag {
		out = append(out, *tc)
	}
	slices.SortFunc(out, func(a, b TagCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Tag, b.Tag)
	})
	return out
}
