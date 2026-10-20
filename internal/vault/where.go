package vault

import (
	"errors"
	"fmt"
	"slices"

	"gopkg.in/yaml.v3"
)

// Where maps a frontmatter field to the values it may equal. Its values are
// read as a field's values are (Values), so tags compare normalised.
type Where map[string][]string

// ParseWhere builds the conditions of a mapping of field: value pairs, each
// value one scalar or a list of them. The config's YAML and a tool's JSON
// both come through here, so its messages use syntax that reads right in both.
func ParseWhere(m *yaml.Node) (Where, error) {
	w, _, err := parseWhere(m)
	return w, err
}

// parseWhere is ParseWhere, also returning the node an error is about.
func parseWhere(m *yaml.Node) (Where, *yaml.Node, error) {
	m = deref(m)
	if m.Kind != yaml.MappingNode {
		return nil, m, errors.New(`where must be field: value pairs, such as {"status": "open"}`)
	}
	w := make(Where, len(m.Content)/2)
	for k, v := range Pairs(m) {
		if _, dup := w[k.Value]; dup {
			return nil, k, fmt.Errorf("where.%s appears twice; use each field once", k.Value)
		}
		if !isScalarOrList(v) {
			return nil, v, fmt.Errorf(
				`where.%s: use one value or a list of values, such as "open" or ["open", "closed"]`,
				k.Value,
			)
		}
		w[k.Value] = Values(m, k.Value)
	}
	return w, nil, nil
}

var _ yaml.Unmarshaler = (*Where)(nil)

// UnmarshalYAML reads conditions from a config file, whose errors name the
// line of the value at fault.
func (w *Where) UnmarshalYAML(n *yaml.Node) error {
	parsed, at, err := parseWhere(n)
	if err != nil {
		return fmt.Errorf("line %d: %w", at.Line, err)
	}
	*w = parsed
	return nil
}

// Match reports whether the frontmatter m meets every condition: for each
// field, one of its values is among the ones listed.
func (w Where) Match(m *yaml.Node) bool {
	for key, want := range w {
		if !slices.ContainsFunc(Values(m, key), func(v string) bool { return slices.Contains(want, v) }) {
			return false
		}
	}
	return true
}

// isScalarOrList reports whether n is a scalar or a list of scalars, aliases
// followed: the shapes of the values a condition compares a field with.
func isScalarOrList(n *yaml.Node) bool {
	n = deref(n)
	items := []*yaml.Node{n}
	if n.Kind == yaml.SequenceNode {
		items = n.Content
	}
	return !slices.ContainsFunc(items, func(c *yaml.Node) bool { return deref(c).Kind != yaml.ScalarNode })
}
