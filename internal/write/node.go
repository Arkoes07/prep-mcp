package write

import (
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func keyNode(k string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
	if needsQuotes(k) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// strNode is a string value written the way a person would write it.
func strNode(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	switch {
	case isPlainDate(s):
		n.Tag = "!!timestamp" // makes the encoder leave it unquoted
	case needsQuotes(s):
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

func isPlainDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// needsQuotes reports whether s, written plain, would be read back as anything
// but the same string: numbers, booleans, null, lists, maps, comments, and the
// YAML 1.1 words (yes, no, on, off) other tools still read as booleans.
func needsQuotes(s string) bool {
	if s == "" {
		return true
	}
	if strings.Contains(s, "\n") {
		return false // the encoder writes a block scalar
	}
	switch strings.ToLower(s) {
	case "yes", "no", "on", "off":
		return true
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(s), &doc); err != nil || len(doc.Content) != 1 {
		return true
	}
	c := doc.Content[0]
	return c.Kind != yaml.ScalarNode || c.ShortTag() != "!!str" || c.Value != s
}
