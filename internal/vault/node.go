package vault

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// NewKeyNode returns a mapping key, quoted when other tools would read it as a boolean.
func NewKeyNode(k string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k}
	if needsQuotes(k) {
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// NewStrNode returns a string value written the way a person would write it.
func NewStrNode(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	switch {
	case isPlainDate(s):
		n.Tag = "!!timestamp" // makes the encoder leave it unquoted
	case needsQuotes(s):
		n.Style = yaml.DoubleQuotedStyle
	}
	return n
}

// NewTagsNode returns a flow list of tags, the form new documents and patches write.
func NewTagsNode(tags []string) *yaml.Node {
	seq := &yaml.Node{
		Kind:    yaml.SequenceNode,
		Tag:     "!!seq",
		Style:   yaml.FlowStyle,
		Content: make([]*yaml.Node, 0, len(tags)),
	}
	for _, t := range tags {
		seq.Content = append(seq.Content, NewStrNode(t))
	}
	return seq
}

// Encode writes n as YAML with a two-space indent. It panics on a node tree
// the encoder rejects: trees built from DecodeJSON and these builders never
// are, so that is a bug, not an input to report.
func Encode(n *yaml.Node) string {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		panic(fmt.Sprintf("yaml encode: %v", err))
	}
	if err := enc.Close(); err != nil {
		panic(fmt.Sprintf("yaml encode: %v", err))
	}
	return b.String()
}

func isPlainDate(s string) bool {
	if len(s) != 10 {
		return false
	}
	_, err := time.Parse(time.DateOnly, s)
	return err == nil
}

// needsQuotes reports whether s is one of the YAML 1.1 words (yes, no, on,
// off) that other tools still read as booleans, or holds an LS or PS. The
// encoder already quotes any other string that would not read back as itself,
// such as "", "12", "true" or "[a]", but it writes these four plain, and an LS
// or PS raw in single quotes, where yaml.v3 reads it as a line break and Edit
// then refuses the block. Double quotes escape them as \L and \P.
func needsQuotes(s string) bool {
	if strings.ContainsAny(s, "  ") {
		return true
	}
	switch strings.ToLower(s) {
	case "yes", "no", "on", "off":
		return true
	}
	return false
}
