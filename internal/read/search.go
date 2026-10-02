package read

import (
	"bytes"
	"errors"
	"strings"
)

// Limits of a Search.
const (
	DefaultSearchLimit = 20
	MaxSearchLimit     = 500
	// MaxHitLines is how many matching lines a hit reports; Total counts all.
	MaxHitLines = 3
)

// Hit is one document that matched a search.
type Hit struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	Total int    `json:"total"` // all matching lines
	Lines []Line `json:"lines"` // at most MaxHitLines, in file order
}

// Line is one matching file line: 1-based number and the verbatim text.
type Line struct {
	N    int    `json:"n"`
	Text string `json:"text"`
}

// Search finds documents whose body or title field contains q, ignoring case.
// Other frontmatter fields and file names are not searched. Hits are ordered
// by path. typ, when set, limits the search to one type.
func (r *Reader) Search(q, typ string, limit int) ([]Hit, error) {
	if strings.TrimSpace(q) == "" {
		return nil, errors.New("query is empty; pass the text to search for")
	}
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	limit = min(limit, MaxSearchLimit)
	needle := []byte(strings.ToLower(q))

	docs, _, err := r.Scan() // problems are reported by List and Context
	if err != nil {
		return nil, err
	}
	hits := []Hit{}
	for _, d := range docs {
		if typ != "" && d.Type != typ {
			continue
		}
		h := d.match(needle)
		if h.Total == 0 {
			continue
		}
		hits = append(hits, h)
		if len(hits) == limit {
			break
		}
	}
	return hits, nil
}

// match finds the lines of d that contain needle (already lowercase): the
// title field's line, then body lines. It keeps the first MaxHitLines and
// counts them all.
func (d *Doc) match(needle []byte) Hit {
	h := Hit{Path: d.Path, Title: d.Title}
	add := func(n int, text string) {
		h.Total++
		if len(h.Lines) < MaxHitLines {
			h.Lines = append(h.Lines, Line{N: n, Text: text})
		}
	}
	if d.titleLine > 0 && bytes.Contains(bytes.ToLower([]byte(d.Title)), needle) {
		add(d.titleLine, d.line(d.titleLine))
	}
	n := d.BodyLine
	for raw := range bytes.Lines(d.Body) {
		if text := trimEOL(raw); bytes.Contains(bytes.ToLower(text), needle) {
			add(n, string(text))
		}
		n++
	}
	return h
}

// line returns the verbatim text of a 1-based line of the scanned file,
// without its line ending.
func (d *Doc) line(n int) string {
	i := 1
	for raw := range bytes.Lines(d.src) {
		if i == n {
			return string(trimEOL(raw))
		}
		i++
	}
	return ""
}

// trimEOL removes one trailing LF or CRLF.
func trimEOL(b []byte) []byte {
	return bytes.TrimSuffix(bytes.TrimSuffix(b, []byte("\n")), []byte("\r"))
}
