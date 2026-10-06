package read

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

const (
	DefaultListLimit = 50
	MaxListLimit     = 500
)

// Orders a Filter can sort by. Every order breaks ties by path, ascending.
const (
	SortDateDesc = "date_desc"
	SortDateAsc  = "date_asc"
	SortTitle    = "title"
)

// Filter selects and orders documents. Every set field must match (AND).
type Filter struct {
	Type         string              // exact type name
	Tags         []string            // all must match; a trailing "/" is a prefix
	AnyTags      []string            // at least one must match
	Where        map[string][]string // frontmatter equality; a list means any of
	Since, Until *time.Time          // inclusive, compared as calendar dates
	Sort         string              // date_desc (default), date_asc, title
	Limit        int                 // 0 means DefaultListLimit; capped at MaxListLimit
}

// Apply filters, sorts and limits docs. It does not touch the input slice.
// Ties in every sort are broken by path, ascending.
func (f Filter) Apply(docs []*Doc) ([]*Doc, error) {
	sortBy := f.Sort
	switch sortBy {
	case "":
		sortBy = SortDateDesc
	case SortDateDesc, SortDateAsc, SortTitle:
	default:
		return nil, fmt.Errorf("unknown sort %q; use one of %s, %s, %s", f.Sort, SortDateDesc, SortDateAsc, SortTitle)
	}
	limit := f.Limit
	if limit <= 0 {
		limit = DefaultListLimit
	}
	limit = min(limit, MaxListLimit)

	var out []*Doc
	for _, d := range docs {
		if matches(d, f) {
			out = append(out, d)
		}
	}
	slices.SortStableFunc(out, func(a, b *Doc) int {
		var c int
		switch sortBy {
		case SortDateDesc:
			c = b.Date.Compare(a.Date)
		case SortDateAsc:
			c = a.Date.Compare(b.Date)
		case SortTitle:
			c = strings.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title))
		}
		return cmp.Or(c, strings.Compare(a.Path, b.Path))
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func matches(d *Doc, f Filter) bool {
	if f.Type != "" && d.Type != f.Type {
		return false
	}
	hasTag := func(pattern string) bool {
		return slices.ContainsFunc(d.Tags, func(t string) bool { return MatchTag(pattern, t) })
	}
	for _, p := range f.Tags {
		if !hasTag(p) {
			return false
		}
	}
	if len(f.AnyTags) > 0 && !slices.ContainsFunc(f.AnyTags, hasTag) {
		return false
	}
	for key, want := range f.Where {
		vals := vault.Values(d.Frontmatter, key)
		if !slices.ContainsFunc(vals, func(v string) bool { return slices.Contains(want, v) }) {
			return false
		}
	}
	day := dayOf(d.Date)
	if f.Since != nil && day.Before(dayOf(*f.Since)) {
		return false
	}
	if f.Until != nil && day.After(dayOf(*f.Until)) {
		return false
	}
	return true
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

// List scans the vault and applies f. Problems are files the scan had to skip.
func (r *Reader) List(f Filter) ([]*Doc, []Problem, error) {
	docs, problems, err := r.Scan()
	if err != nil {
		return nil, nil, err
	}
	out, err := f.Apply(docs)
	if err != nil {
		return nil, nil, err
	}
	return out, problems, nil
}
