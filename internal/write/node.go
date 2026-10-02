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

// needsQuotes reports whether s is one of the YAML 1.1 words (yes, no, on,
// off) that other tools still read as booleans. The encoder already quotes any
// other string that would not read back as itself, such as "", "12", "true"
// or "[a]", but it writes these four plain.
func needsQuotes(s string) bool {
	switch strings.ToLower(s) {
	case "yes", "no", "on", "off":
		return true
	}
	return false
}
