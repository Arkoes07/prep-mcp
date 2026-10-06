package vault

import (
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// What a frontmatter field's values are, and whether it has a value, is
// decided here; reads, writes and the where conditions of the server and the
// config all ask these functions rather than look at the nodes. A value is
// text, trimmed, with null as "". An alias reads as what it points to. tags
// are the normalised tags, in whatever form they are written.

// Values returns the values of field key of the mapping m: one for a scalar,
// one per scalar item of a list, and none for a missing field, [] or a nested
// mapping.
func Values(m *yaml.Node, key string) []string {
	n := deref(Field(m, key))
	switch {
	case key == "tags":
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
	if key == "tags" {
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
