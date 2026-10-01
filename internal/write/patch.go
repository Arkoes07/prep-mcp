package write

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// Patch is a set of frontmatter changes.
type Patch struct {
	Set                        *yaml.Node // ordered mapping of keys to set; nil for none
	Unset, AddTags, RemoveTags []string
}

// ApplyPatch changes only the affected top-level keys of src's frontmatter and
// returns the new file. Untouched lines and the whole body keep their exact
// bytes. changed is false, and out is src, when the patch changes nothing.
// Any real change also sets updated to today, unless the patch sets or unsets it.
func ApplyPatch(src []byte, p Patch, today string) (out []byte, changed bool, err error) {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil {
		return nil, false, err
	}
	if fm == nil {
		return nil, false, errors.New("no frontmatter to patch; use replace_doc to give this file frontmatter")
	}

	setKeys, err := setKeyList(p.Set)
	if err != nil {
		return nil, false, err
	}
	hasTagOps := len(p.AddTags) > 0 || len(p.RemoveTags) > 0
	for _, k := range p.Unset {
		switch {
		case slices.Contains(setKeys, k):
			return nil, false, fmt.Errorf("%s is in both set and unset; choose one", k)
		case k == "type":
			return nil, false, errors.New("type cannot be unset: every document needs a type (set it to change it)")
		case k == "tags" && hasTagOps:
			return nil, false, errors.New("tags is unset and also changed by add_tags/remove_tags; use one or the other")
		}
	}
	setTags := slices.Contains(setKeys, "tags")
	if setTags && hasTagOps {
		return nil, false, errors.New("set.tags cannot be combined with add_tags or remove_tags; use one or the other")
	}

	eol := "\n"
	if fm.CRLF {
		eol = "\r\n"
	}
	lines := splitLinesKeep(string(src[fm.Start:fm.End]))
	spans := keySpans(fm.Map, lines)

	type edit struct {
		replace []string // rendered lines, without line endings
		drop    bool
	}
	edits := map[string]edit{}
	var appended []string
	touched := map[string]bool{}
	commentOf := func(k string) string {
		s, ok := spans[k]
		if !ok {
			return ""
		}
		if c := s.val.LineComment; c != "" {
			return c
		}
		return s.key.LineComment
	}
	put := func(k string, lines []string) {
		touched[k] = true
		if _, ok := spans[k]; ok {
			edits[k] = edit{replace: lines}
		} else {
			appended = append(appended, lines...)
		}
	}

	for _, k := range p.Unset {
		if _, ok := spans[k]; ok {
			edits[k] = edit{drop: true}
			touched[k] = true
		}
	}

	if p.Set != nil {
		for i := 0; i+1 < len(p.Set.Content); i += 2 {
			k, v := p.Set.Content[i].Value, p.Set.Content[i+1]
			if k == "tags" {
				continue
			}
			if s, ok := spans[k]; ok && nodeEqual(s.val, v) {
				continue
			}
			put(k, renderKey(k, v, commentOf(k)))
		}
	}

	var wantTags []string
	if hasTagOps || setTags {
		cur := vault.NormalizeTags(vault.Field(fm.Map, "tags"))
		if setTags {
			wantTags = vault.NormalizeTags(vault.Field(p.Set, "tags"))
		} else {
			wantTags = slices.Clone(cur)
			for _, t := range vault.NormalizeTagList(p.AddTags) {
				if !slices.Contains(wantTags, t) {
					wantTags = append(wantTags, t)
				}
			}
			drop := vault.NormalizeTagList(p.RemoveTags)
			wantTags = slices.DeleteFunc(wantTags, func(t string) bool { return slices.Contains(drop, t) })
		}
		if !slices.Equal(cur, wantTags) {
			put("tags", renderTags(vault.Field(fm.Map, "tags"), wantTags, commentOf("tags")))
		}
	}

	if len(touched) == 0 {
		return src, false, nil
	}
	if !touched["updated"] && !slices.Contains(p.Unset, "updated") {
		day := strNode(today)
		if s, ok := spans["updated"]; !ok || !nodeEqual(s.val, day) {
			put("updated", renderKey("updated", day, commentOf("updated")))
		}
	}

	// Rebuild the frontmatter block line by line.
	var block strings.Builder
	emit := func(rendered []string) {
		for _, l := range rendered {
			block.WriteString(l)
			block.WriteString(eol)
		}
	}
	byStart := map[int]string{}
	for k, s := range spans {
		if _, ok := edits[k]; ok {
			byStart[s.from] = k
		}
	}
	for i := 0; i < len(lines); i++ {
		k, ok := byStart[i]
		if !ok {
			block.WriteString(lines[i])
			continue
		}
		emit(edits[k].replace)
		i = spans[k].to - 1
	}
	emit(appended)
	out = slices.Concat(src[:fm.Start], []byte(block.String()), src[fm.End:])

	if err := verifyPatch(src, out, fm, p, setKeys, touched, wantTags, hasTagOps || setTags); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func setKeyList(set *yaml.Node) ([]string, error) {
	if set == nil {
		return nil, nil
	}
	if set.Kind != yaml.MappingNode {
		return nil, errors.New("set must be an object of key: value pairs")
	}
	var keys []string
	for i := 0; i+1 < len(set.Content); i += 2 {
		keys = append(keys, set.Content[i].Value)
	}
	return keys, nil
}

// span is where one top-level key lives in the frontmatter block, in line
// indexes: lines[from:to]. Blank and comment-only lines after the value belong
// to no key.
type span struct {
	key, val *yaml.Node
	from, to int
}

func keySpans(m *yaml.Node, lines []string) map[string]span {
	spans := map[string]span{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		from, to := key.Line-1, len(lines)
		if i+2 < len(m.Content) {
			to = m.Content[i+2].Line - 1
		}
		block := val.Kind == yaml.ScalarNode && val.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0
		for to-1 > from {
			t := strings.TrimSpace(lines[to-1])
			if t == "" || (!block && strings.HasPrefix(t, "#")) {
				to--
				continue
			}
			break
		}
		spans[key.Value] = span{key: key, val: val, from: from, to: to}
	}
	return spans
}

func splitLinesKeep(s string) []string {
	var out []string
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			out = append(out, s)
			break
		}
		out = append(out, s[:i+1])
		s = s[i+1:]
	}
	return out
}

// renderKey encodes one "key: value" pair as YAML lines, keeping the old line comment.
func renderKey(k string, v *yaml.Node, comment string) []string {
	pair := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{keyNode(k), v}}
	lines := strings.Split(strings.TrimSuffix(encode(pair), "\n"), "\n")
	if comment != "" {
		lines[0] += " " + comment
	}
	return lines
}

// renderTags writes a normalised tag list in the style of the existing node:
// block if it was a block list, flow otherwise (also for a comma string).
func renderTags(existing *yaml.Node, tags []string, comment string) []string {
	isBlockList := existing != nil && existing.Kind == yaml.SequenceNode && existing.Style&yaml.FlowStyle == 0
	if isBlockList && len(existing.Content) > 0 && len(tags) > 0 {
		indent := strings.Repeat(" ", max(existing.Column-1, 0))
		head := "tags:"
		if comment != "" {
			head += " " + comment
		}
		out := []string{head}
		for _, t := range tags {
			out = append(out, indent+"- "+strings.TrimSuffix(encode(strNode(t)), "\n"))
		}
		return out
	}
	return renderKey("tags", tagsNode(tags), comment)
}

// tagsNode is a flow list of tags, the form create_doc and patches write.
func tagsNode(tags []string) *yaml.Node {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Style: yaml.FlowStyle}
	for _, t := range tags {
		seq.Content = append(seq.Content, strNode(t))
	}
	return seq
}

func encode(n *yaml.Node) string {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		panic(fmt.Sprintf("yaml encode: %v", err)) // only for an invalid node tree we built ourselves; guard turns it into an error result
	}
	enc.Close()
	return b.String()
}

// nodeEqual compares what two nodes say, not how they are written.
func nodeEqual(a, b *yaml.Node) bool {
	if a != nil && a.Kind == yaml.AliasNode {
		a = a.Alias
	}
	if b != nil && b.Kind == yaml.AliasNode {
		b = b.Alias
	}
	if a == nil || b == nil {
		return a == b
	}
	if isNull(a) || isNull(b) {
		return isNull(a) && isNull(b)
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case yaml.ScalarNode:
		return a.Value == b.Value
	case yaml.SequenceNode, yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !nodeEqual(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	}
	return false
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!null" }

// verifyPatch is the safety net: the result must re-read as the patch intended,
// with every untouched key and the body unchanged.
func verifyPatch(src, out []byte, old *vault.Frontmatter, p Patch, setKeys []string, touched map[string]bool, wantTags []string, tagsTouched bool) error {
	bug := func(format string, args ...any) error {
		return fmt.Errorf("internal error: the patched frontmatter did not re-read as intended (%s); nothing was written. Report this as a bug and use replace_doc instead", fmt.Sprintf(format, args...))
	}
	re, err := vault.SplitFrontmatter(out)
	if err != nil || re == nil {
		return bug("%v", err)
	}
	if !bytes.Equal(out[re.BodyOffset:], src[old.BodyOffset:]) {
		return bug("the body changed")
	}
	var oldOrder, newOrder []string
	for i := 0; i+1 < len(old.Map.Content); i += 2 {
		k := old.Map.Content[i].Value
		if touched[k] {
			continue
		}
		oldOrder = append(oldOrder, k)
		if !nodeEqual(old.Map.Content[i+1], vault.Field(re.Map, k)) {
			return bug("%s changed", k)
		}
	}
	for i := 0; i+1 < len(re.Map.Content); i += 2 {
		if k := re.Map.Content[i].Value; !touched[k] && slices.Contains(oldOrder, k) {
			newOrder = append(newOrder, k)
		}
	}
	if !slices.Equal(oldOrder, newOrder) {
		return bug("key order changed")
	}
	for _, k := range setKeys {
		if k == "tags" {
			continue
		}
		if !nodeEqual(vault.Field(p.Set, k), vault.Field(re.Map, k)) {
			return bug("%s was not written as given", k)
		}
	}
	for _, k := range p.Unset {
		if vault.Field(re.Map, k) != nil {
			return bug("%s is still present", k)
		}
	}
	if tagsTouched && !slices.Equal(vault.NormalizeTags(vault.Field(re.Map, "tags")), wantTags) {
		return bug("tags differ")
	}
	return nil
}

// ---- JSON to yaml.Node ----

// JSONToNode decodes a JSON object into a mapping node that keeps key order.
// Scalars carry the tag and style that make the encoder write them the way a
// person would: date-like strings plain (so editors still see a date), strings
// that YAML would read as something else quoted, short lists in flow style.
func JSONToNode(raw json.RawMessage) (*yaml.Node, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	const want = `must be a JSON object such as {"key": "value"}`
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %v; %s", err, want)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("%s, got %v", want, describeToken(tok))
	}
	n, err := decodeObject(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected text after the JSON object; pass one object")
	}
	return n, nil
}

func describeToken(t json.Token) string {
	switch v := t.(type) {
	case json.Delim:
		return "an array"
	case string:
		return "a string"
	case json.Number:
		return "a number"
	case bool:
		return "a boolean"
	case nil:
		return "null"
	default:
		return fmt.Sprintf("%T", v)
	}
}

func decodeObject(dec *json.Decoder) (*yaml.Node, error) {
	m := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	seen := map[string]bool{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("invalid JSON: %v", err)
		}
		key, _ := kt.(string)
		if seen[key] {
			return nil, fmt.Errorf("duplicate key %q in the JSON object; use each key once", key)
		}
		seen[key] = true
		v, err := decodeValue(dec)
		if err != nil {
			return nil, err
		}
		m.Content = append(m.Content, keyNode(key), v)
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	if len(m.Content) == 0 {
		m.Style = yaml.FlowStyle
	}
	return m, nil
}

func decodeValue(dec *json.Decoder) (*yaml.Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	switch t := tok.(type) {
	case json.Delim:
		if t == '{' {
			return decodeObject(dec)
		}
		return decodeArray(dec)
	case string:
		return strNode(t), nil
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
		return nil, fmt.Errorf("invalid JSON: %v", err)
	}
	return seq, nil
}

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
