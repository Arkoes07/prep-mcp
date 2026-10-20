package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"gopkg.in/yaml.v3"
)

// DecodeJSON decodes a JSON object into a mapping node that keeps key order.
// Scalars carry the tag and style that make the encoder write them the way a
// person would: date-like strings plain (so editors still see a date), strings
// that YAML would read as something else quoted, short lists in flow style.
func DecodeJSON(raw json.RawMessage) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	const want = `must be a JSON object such as {"key": "value"}`
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %w; %s", err, want)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("%s, got %v", want, tok)
	}
	n, err := decodeObject(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected text after the JSON object; pass one object")
	}
	return n, nil
}

func explainInvalidJSON(err error) error {
	return fmt.Errorf("invalid JSON: %w; check its quotes, commas and brackets", err)
}

func decodeObject(dec *json.Decoder) (*yaml.Node, error) {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, explainInvalidJSON(err)
		}
		key, _ := kt.(string) // inside an object the decoder only yields string keys
		if seen[key] {
			return nil, fmt.Errorf("duplicate key %q in the JSON object; use each key once", key)
		}
		seen[key] = true
		v, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		m.Content = append(m.Content, NewKeyNode(key), v)
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, explainInvalidJSON(err)
	}
	if len(m.Content) == 0 {
		m.Style = yaml.FlowStyle
	}
	return m, nil
}

func decodeValue(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, explainInvalidJSON(err)
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			return decodeObject(dec)
		}
		return decodeArray(dec)
	case string:
		return NewStrNode(t), nil
	case json.Number:
		tag := "!!float"
		if _, err := strconv.ParseInt(string(t), 10, 64); err == nil {
			tag = "!!int"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: string(t)}, nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(t)}, nil
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
}

func decodeArray(dec *json.Decoder) (*yaml.Node, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for dec.More() {
		v, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		if v.Kind != yaml.ScalarNode {
			seq.Style = 0 // a list holding lists or objects reads better as a block
		}
		seq.Content = append(seq.Content, v)
	}
	if _, err := dec.Token(); err != nil { // the closing bracket
		return nil, explainInvalidJSON(err)
	}
	return seq, nil
}
