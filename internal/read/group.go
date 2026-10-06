package read

import (
	"cmp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// NoValue stands for a missing or empty field in a Group.
const NoValue = "(none)"

// Group is one distinct combination of field values and how many documents
// have it.
type Group struct {
	Values []string `json:"values"` // one per grouped field, in the order asked
	Count  int      `json:"count"`
}

// Group scans the vault, keeps the documents f matches (its sort and limit do
// not apply), and counts them per combination of the fields' values: most
// documents first, then by the values. A list field counts a document once
// under each of its values.
func (r *Reader) Group(f Filter, fields []string) ([]Group, []Problem, error) {
	docs, problems, err := r.Scan()
	if err != nil {
		return nil, nil, err
	}
	byKey := map[string]*Group{}
	for _, d := range docs {
		if !matches(d, f) {
			continue
		}
		for _, values := range combinations(d, fields) {
			// Frontmatter text cannot hold a NUL, so the joined values are a unique key.
			key := strings.Join(values, "\x00")
			if g, ok := byKey[key]; ok {
				g.Count++
				continue
			}
			byKey[key] = &Group{Values: values, Count: 1}
		}
	}
	out := make([]Group, 0, len(byKey))
	for _, g := range byKey {
		out = append(out, *g)
	}
	slices.SortFunc(out, func(a, b Group) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), slices.Compare(a.Values, b.Values))
	})
	return out, problems, nil
}

// combinations returns every combination of d's values for fields, one value
// per field, so a document with two tags is in two combinations.
func combinations(d *Doc, fields []string) [][]string {
	combos := [][]string{{}}
	for _, field := range fields {
		var next [][]string
		for _, c := range combos {
			for _, v := range fieldValues(d, field) {
				next = append(next, append(slices.Clone(c), v))
			}
		}
		combos = next
	}
	return combos
}

// fieldValues returns the distinct texts of d's field, or NoValue alone if it
// has none. tags are the normalised tags; null and nested values count as none.
func fieldValues(d *Doc, field string) []string {
	if field == "tags" {
		if len(d.Tags) == 0 {
			return []string{NoValue}
		}
		return d.Tags
	}
	var vals []string
	if n := d.Fields[field]; n != nil && n.Kind == yaml.SequenceNode {
		for _, c := range n.Content {
			vals = append(vals, strings.TrimSpace(vault.Scalar(c)))
		}
	} else {
		vals = append(vals, strings.TrimSpace(vault.Scalar(n)))
	}
	vals = slices.DeleteFunc(vals, func(v string) bool { return v == "" })
	if len(vals) == 0 {
		return []string{NoValue}
	}
	slices.Sort(vals)
	return slices.Compact(vals)
}
