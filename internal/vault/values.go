package vault

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// The frontmatter fields the server reads or writes by name.
const (
	FieldType    = "type"
	FieldTags    = "tags"
	FieldTitle   = "title"
	FieldCreated = "created"
	FieldUpdated = "updated"
)

// What a frontmatter field's values are, whether it has a value, and the day
// a date field holds, is decided here; reads, writes and the where conditions
// of the server and the config all ask these functions rather than look at
// the nodes. A value is text, trimmed, with null as "". An alias reads as
// what it points to. tags are the normalised tags, in whatever form they are
// written.

// Values returns the values of field key of the mapping m: one for a scalar,
// one per scalar item of a list, and none for a missing field, [] or a nested
// mapping.
func Values(m *yaml.Node, key string) []string {
	n := deref(Field(m, key))
	switch {
	case key == FieldTags:
		return normalizeTags(n)
	case n == nil:
		return []string{}
	case n.Kind == yaml.SequenceNode:
		return scalarTexts(n.Content)
	}
	return scalarTexts([]*yaml.Node{n})
}

// HasValue reports whether field key of m has a value: a scalar that is not
// blank, a mapping that is not empty, or a list with an item that has one.
// tags have one when a tag survives normalising.
func HasValue(m *yaml.Node, key string) bool {
	if key == FieldTags {
		return len(Values(m, key)) > 0
	}
	return hasValue(Field(m, key))
}

func hasValue(n *yaml.Node) bool {
	n = deref(n)
	if n == nil {
		return false
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return scalarText(n) != ""
	case yaml.MappingNode:
		return len(n.Content) > 0
	case yaml.SequenceNode:
		return slices.ContainsFunc(n.Content, hasValue)
	}
	return false
}

// Text returns the one value of field key of m, or "" when it has none or
// several.
func Text(m *yaml.Node, key string) string {
	if vals := Values(m, key); len(vals) == 1 {
		return vals[0]
	}
	return ""
}

// Day returns the day the date field key of m holds, at local midnight, as
// ParseDate reads it. It returns the zero time and no error when the field
// has no value, and an error naming the field when it holds anything but one
// date.
func Day(m *yaml.Node, key string) (time.Time, error) {
	if !HasValue(m, key) {
		return time.Time{}, nil
	}
	s := Text(m, key)
	if s == "" {
		return time.Time{}, fmt.Errorf("%s holds a list or mapping; use one date, YYYY-MM-DD", key)
	}
	t, err := ParseDate(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %w", key, err)
	}
	return t, nil
}

// ParseDate parses YYYY-MM-DD, or a timestamp starting with one. It returns
// the calendar date as written, at local midnight; anything after the date
// (a time of day, a zone) is dropped unread, so the same vault gives the same
// answers in every timezone.
func ParseDate(s string) (time.Time, error) {
	d := strings.TrimSpace(s)
	if len(d) > 10 && (d[10] == 'T' || d[10] == ' ') {
		d = d[:10]
	}
	t, err := time.ParseInLocation(time.DateOnly, d, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a date; use YYYY-MM-DD", s)
	}
	return t, nil
}

// IsScalarOrList reports whether n is a scalar or a list of scalars, aliases
// followed: the shapes of the values a filter compares a field with.
func IsScalarOrList(n *yaml.Node) bool {
	n = deref(n)
	items := []*yaml.Node{n}
	if n.Kind == yaml.SequenceNode {
		items = n.Content
	}
	return !slices.ContainsFunc(items, func(c *yaml.Node) bool { return deref(c).Kind != yaml.ScalarNode })
}

// scalarTexts returns the text of each scalar among nodes, skipping the rest.
func scalarTexts(nodes []*yaml.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n = deref(n); n.Kind == yaml.ScalarNode {
			out = append(out, scalarText(n))
		}
	}
	return out
}

// scalarText is a scalar's text, trimmed, with null as "".
func scalarText(n *yaml.Node) string {
	if n.Tag == "!!null" {
		return ""
	}
	return strings.TrimSpace(n.Value)
}

// deref returns the node an alias points to, and any other node as it is.
func deref(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode {
		return n.Alias
	}
	return n
}
