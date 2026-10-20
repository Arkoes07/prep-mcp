package vault

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Edit is a change to the top-level keys of a Frontmatter. Apply makes it and
// keeps every other byte of the file: the other lines with their comments and
// alignment, the body and the line endings.
//
// It splices lines instead of re-encoding the block through yaml.Node, which
// collapses the aligned "# a | b" comments templates produce and would make
// every edit a noisy diff. The cost is more code, checked by verify.
type Edit struct {
	fm    *Frontmatter
	lines []string            // the block's lines, with their line endings
	spans map[string]span     // the block's keys
	edits map[string]lineEdit // the keys set or removed
	added []string            // the keys set that are new, in order
}

// lineEdit is what happens to one key.
type lineEdit struct {
	lines []string   // rendered lines, without line endings; none removes the key
	want  *yaml.Node // what the key must re-read as; nil for absent
}

// Edit starts an edit of fm. Neither it nor Apply changes fm or its nodes.
func (fm *Frontmatter) Edit() *Edit {
	lines := slices.Collect(strings.Lines(string(fm.src[fm.start:fm.end])))
	return &Edit{fm: fm, lines: lines, spans: keySpans(fm.Map, lines), edits: map[string]lineEdit{}}
}

// Set gives key the value val: in place, keeping its line comment, when the
// key exists, else after the last key. It does nothing when the file already
// has an Equal value there and the edit has not changed it.
func (e *Edit) Set(key string, val *yaml.Node) {
	if _, edited := e.edits[key]; !edited && Equal(Field(e.fm.Map, key), val) {
		return
	}
	e.put(key, val, renderKey(key, val, e.commentOf(key)))
}

// Unset removes key, if the block has it.
func (e *Edit) Unset(key string) {
	if _, ok := e.spans[key]; ok {
		e.edits[key] = lineEdit{}
	}
}

// SetTags sets tags to the normalised list tags, in the style of the list
// there: block if it is a block list, flow otherwise (also for a comma
// string). It does nothing when Values already reads tags as tags.
func (e *Edit) SetTags(tags []string) {
	if slices.Equal(Values(e.fm.Map, FieldTags), tags) {
		return
	}
	e.put(FieldTags, TagsNode(tags), renderTags(Field(e.fm.Map, FieldTags), tags, e.commentOf(FieldTags)))
}

// Changed reports whether the edit changes anything.
func (e *Edit) Changed() bool { return len(e.edits) > 0 }

// Apply makes the edit and returns the new file and its frontmatter, re-read
// and verified. Without a change it returns the file and fm as they are. Its
// error says what did not re-read; the caller says what to do about it.
func (e *Edit) Apply() ([]byte, *Frontmatter, error) {
	if !e.Changed() {
		return e.fm.src, e.fm, nil
	}
	fm := e.fm
	eol := "\n"
	if fm.crlf {
		eol = "\r\n"
	}
	out := slices.Concat(fm.src[:fm.start], []byte(e.rebuild(eol)), fm.src[fm.end:])
	re, err := e.verify(out)
	if err != nil {
		return nil, nil, fmt.Errorf("the edited frontmatter did not re-read as intended: %w", err)
	}
	return out, re, nil
}

// commentOf returns the line comment of an existing key, so a rewrite keeps it.
func (e *Edit) commentOf(k string) string {
	s, ok := e.spans[k]
	if !ok {
		return ""
	}
	if c := s.val.LineComment; c != "" {
		return c
	}
	return s.key.LineComment
}

// put writes k as the rendered lines, which must re-read as val.
func (e *Edit) put(k string, val *yaml.Node, lines []string) {
	_, exists := e.spans[k]
	if _, again := e.edits[k]; !exists && !again {
		e.added = append(e.added, k)
	}
	e.edits[k] = lineEdit{lines: lines, want: val}
}

// rebuild writes the block with the edits: changed keys in place, removed
// keys gone, new keys before the closing fence, and every other line byte for
// byte.
func (e *Edit) rebuild(eol string) string {
	var block strings.Builder
	emit := func(k string) {
		for _, l := range e.edits[k].lines {
			block.WriteString(l)
			block.WriteString(eol)
		}
	}
	byStart := make(map[int]string, len(e.edits))
	for k, s := range e.spans {
		if _, ok := e.edits[k]; ok {
			byStart[s.from] = k
		}
	}
	for i := 0; i < len(e.lines); i++ {
		k, ok := byStart[i]
		if !ok {
			block.WriteString(e.lines[i])
			continue
		}
		emit(k)
		i = e.spans[k].to - 1
	}
	for _, k := range e.added {
		emit(k)
	}
	return block.String()
}

// verify is the safety net: out must re-read with each edited key as
// intended, and every other key and the body unchanged and in order.
func (e *Edit) verify(out []byte) (*Frontmatter, error) {
	re, err := SplitFrontmatter(out)
	var fe *FrontmatterError
	if errors.As(err, &fe) {
		return nil, errors.New(fe.Reason) // its Fix is for a file on disk, and out was never written
	}
	if re == nil {
		return nil, errors.New("no frontmatter found")
	}
	if !bytes.Equal(re.Body(), e.fm.Body()) {
		return nil, errors.New("the body changed")
	}
	var oldOrder, newOrder []string
	for key, val := range Pairs(e.fm.Map) {
		k := key.Value
		if _, edited := e.edits[k]; edited {
			continue
		}
		oldOrder = append(oldOrder, k)
		if !Equal(val, Field(re.Map, k)) {
			return nil, fmt.Errorf("%s changed", k)
		}
	}
	for key := range Pairs(re.Map) {
		if _, edited := e.edits[key.Value]; !edited {
			newOrder = append(newOrder, key.Value)
		}
	}
	if !slices.Equal(oldOrder, newOrder) {
		return nil, errors.New("key order changed")
	}
	for k, ed := range e.edits {
		switch got := Field(re.Map, k); {
		case Equal(ed.want, got):
		case ed.want == nil:
			return nil, fmt.Errorf("%s is still present", k)
		default:
			return nil, fmt.Errorf("%s was not written as given", k)
		}
	}
	return re, nil
}

// span is where one top-level key lives in the block, in line indexes:
// lines[from:to]. Blank and comment-only lines after the value belong to no
// key.
type span struct {
	key, val *yaml.Node
	from, to int
}

func keySpans(m *yaml.Node, lines []string) map[string]span {
	spans := make(map[string]span, len(m.Content)/2)
	for i := 0; i+1 < len(m.Content); i += 2 {
		key, val := m.Content[i], m.Content[i+1]
		// yaml.v3 counts a node's line from 1 in the block.
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

// renderKey encodes one "key: value" pair as YAML lines, keeping the old line comment.
func renderKey(k string, v *yaml.Node, comment string) []string {
	pair := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{KeyNode(k), v}}
	lines := strings.Split(strings.TrimSuffix(Encode(pair), "\n"), "\n")
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
		head := FieldTags + ":"
		if comment != "" {
			head += " " + comment
		}
		out := []string{head}
		for _, t := range tags {
			out = append(out, indent+"- "+strings.TrimSuffix(Encode(StrNode(t)), "\n"))
		}
		return out
	}
	return renderKey(FieldTags, TagsNode(tags), comment)
}
