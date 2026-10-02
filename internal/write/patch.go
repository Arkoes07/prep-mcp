package write

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// Patch is a set of frontmatter changes.
type Patch struct {
	Set                        *yaml.Node // ordered mapping of keys to set; nil for none
	Unset, AddTags, RemoveTags []string
}

// ApplyPatch changes only the affected top-level keys of src's frontmatter and
// returns the new file with its frontmatter, re-read. Untouched lines and the
// whole body keep their exact bytes. newFM is nil, and out is src, when the
// patch changes nothing. Any real change also sets updated to today, unless
// the patch sets or unsets it.
func ApplyPatch(src []byte, p Patch, today string) (out []byte, newFM *vault.Frontmatter, err error) {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil {
		return nil, nil, err
	}
	if fm == nil {
		return nil, nil, errors.New("no frontmatter to patch; use replace_doc to give this file frontmatter")
	}
	setKeys, err := checkPatch(p)
	if err != nil {
		return nil, nil, err
	}

	lines := slices.Collect(strings.Lines(string(src[fm.Start:fm.End])))
	pl := &patchPlan{
		patch:   p,
		setKeys: setKeys,
		spans:   keySpans(fm.Map, lines),
		edits:   map[string]lineEdit{},
		touched: map[string]bool{},
	}
	pl.unsetKeys()
	pl.setValues()
	pl.changeTags(fm.Map)
	if len(pl.touched) == 0 {
		return src, nil, nil
	}
	pl.stampUpdated(today)

	eol := "\n"
	if fm.CRLF {
		eol = "\r\n"
	}
	out = slices.Concat(src[:fm.Start], []byte(pl.rebuild(lines, eol)), src[fm.End:])
	newFM, err = verifyPatch(src, out, fm, pl)
	if err != nil {
		return nil, nil, err
	}
	return out, newFM, nil
}

// checkPatch rejects a patch that contradicts itself and returns the keys of
// p.Set in order.
func checkPatch(p Patch) ([]string, error) {
	setKeys, err := setKeyList(p.Set)
	if err != nil {
		return nil, err
	}
	for _, k := range p.Unset {
		switch {
		case slices.Contains(setKeys, k):
			return nil, fmt.Errorf("%s is in both set and unset; choose one", k)
		case k == "type":
			return nil, errors.New("type cannot be unset: every document needs a type (set it to change it)")
		case k == "tags" && p.hasTagOps():
			return nil, errors.New("tags is unset and also changed by add_tags/remove_tags; use one or the other")
		}
	}
	if slices.Contains(setKeys, "tags") && p.hasTagOps() {
		return nil, errors.New("set.tags cannot be combined with add_tags or remove_tags; use one or the other")
	}
	return setKeys, nil
}

func (p Patch) hasTagOps() bool { return len(p.AddTags) > 0 || len(p.RemoveTags) > 0 }

// lineEdit is what happens to the lines of one key already in the block.
type lineEdit struct {
	replace   []string // rendered lines, without line endings
	isDropped bool     // the key is removed; replace is empty
}

// patchPlan collects the line edits one Patch makes to one frontmatter block.
type patchPlan struct {
	patch        Patch
	setKeys      []string
	spans        map[string]span
	edits        map[string]lineEdit // keys already in the block
	appended     []string            // rendered lines of new keys, in order
	touched      map[string]bool     // keys set, changed or removed
	hasTagChange bool                // the patch sets or changes tags
	wantTags     []string            // the normalised tags asked for, when hasTagChange
}

// commentOf returns the line comment of an existing key, so a rewrite keeps it.
func (pl *patchPlan) commentOf(k string) string {
	s, ok := pl.spans[k]
	if !ok {
		return ""
	}
	if c := s.val.LineComment; c != "" {
		return c
	}
	return s.key.LineComment
}

// put writes k as the rendered lines: in place if the key exists, else appended.
func (pl *patchPlan) put(k string, lines []string) {
	pl.touched[k] = true
	if _, ok := pl.spans[k]; ok {
		pl.edits[k] = lineEdit{replace: lines}
		return
	}
	pl.appended = append(pl.appended, lines...)
}

func (pl *patchPlan) unsetKeys() {
	for _, k := range pl.patch.Unset {
		if _, ok := pl.spans[k]; ok {
			pl.edits[k] = lineEdit{isDropped: true}
			pl.touched[k] = true
		}
	}
}

// setValues writes every set key whose value differs, except tags, which
// changeTags handles.
func (pl *patchPlan) setValues() {
	for key, val := range vault.Pairs(pl.patch.Set) {
		k := key.Value
		if k == "tags" {
			continue
		}
		if s, ok := pl.spans[k]; ok && nodeEqual(s.val, val) {
			continue
		}
		pl.put(k, renderKey(k, val, pl.commentOf(k)))
	}
}

// changeTags works out the tag list the patch asks for and rewrites the tags
// key if that differs from the current list. m is the current frontmatter.
func (pl *patchPlan) changeTags(m *yaml.Node) {
	p := pl.patch
	setTags := slices.Contains(pl.setKeys, "tags")
	if !setTags && !p.hasTagOps() {
		return
	}
	pl.hasTagChange = true
	cur := vault.NormalizeTags(vault.Field(m, "tags"))
	if setTags {
		pl.wantTags = vault.NormalizeTags(vault.Field(p.Set, "tags"))
	} else {
		want := slices.Clone(cur)
		for _, t := range vault.NormalizeTagList(p.AddTags) {
			if !slices.Contains(want, t) {
				want = append(want, t)
			}
		}
		drop := vault.NormalizeTagList(p.RemoveTags)
		pl.wantTags = slices.DeleteFunc(want, func(t string) bool { return slices.Contains(drop, t) })
	}
	if !slices.Equal(cur, pl.wantTags) {
		pl.put("tags", renderTags(vault.Field(m, "tags"), pl.wantTags, pl.commentOf("tags")))
	}
}

// stampUpdated sets updated to today, unless the patch sets or unsets it itself.
func (pl *patchPlan) stampUpdated(today string) {
	if pl.touched["updated"] || slices.Contains(pl.patch.Unset, "updated") {
		return
	}
	day := strNode(today)
	if s, ok := pl.spans["updated"]; !ok || !nodeEqual(s.val, day) {
		pl.put("updated", renderKey("updated", day, pl.commentOf("updated")))
	}
}

// rebuild writes the frontmatter block with the planned edits: changed keys in
// place, dropped keys gone, new keys before the closing fence, and every other
// line byte for byte.
func (pl *patchPlan) rebuild(lines []string, eol string) string {
	var block strings.Builder
	emit := func(rendered []string) {
		for _, l := range rendered {
			block.WriteString(l)
			block.WriteString(eol)
		}
	}
	byStart := make(map[int]string, len(pl.edits))
	for k, s := range pl.spans {
		if _, ok := pl.edits[k]; ok {
			byStart[s.from] = k
		}
	}
	for i := 0; i < len(lines); i++ {
		k, ok := byStart[i]
		if !ok {
			block.WriteString(lines[i])
			continue
		}
		if e := pl.edits[k]; !e.isDropped {
			emit(e.replace)
		}
		i = pl.spans[k].to - 1
	}
	emit(pl.appended)
	return block.String()
}

func setKeyList(set *yaml.Node) ([]string, error) {
	if set == nil {
		return nil, nil
	}
	if set.Kind != yaml.MappingNode {
		return nil, errors.New("set must be an object of key: value pairs")
	}
	keys := make([]string, 0, len(set.Content)/2)
	for k := range vault.Pairs(set) {
		keys = append(keys, k.Value)
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
	spans := make(map[string]span, len(m.Content)/2)
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
	seq := &yaml.Node{
		Kind:    yaml.SequenceNode,
		Tag:     "!!seq",
		Style:   yaml.FlowStyle,
		Content: make([]*yaml.Node, 0, len(tags)),
	}
	for _, t := range tags {
		seq.Content = append(seq.Content, strNode(t))
	}
	return seq
}

func encode(n *yaml.Node) string {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	// Encoding fails only for an invalid node tree, and every tree here is
	// built by this package: that is a bug, which guard turns into an error result.
	if err := enc.Encode(n); err != nil {
		panic(fmt.Sprintf("yaml encode: %v", err))
	}
	if err := enc.Close(); err != nil {
		panic(fmt.Sprintf("yaml encode: %v", err))
	}
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
// with every untouched key and the body unchanged. It returns what it re-read.
func verifyPatch(src, out []byte, old *vault.Frontmatter, pl *patchPlan) (*vault.Frontmatter, error) {
	const failed = "internal error: the patched frontmatter did not re-read as intended (%w); " +
		"nothing was written. Report this as a bug and use replace_doc instead"
	bug := func(format string, args ...any) error {
		return fmt.Errorf(failed, fmt.Errorf(format, args...))
	}
	re, err := reread(out)
	if err != nil {
		return nil, fmt.Errorf(failed, err)
	}
	p, touched := pl.patch, pl.touched
	if !bytes.Equal(out[re.BodyOffset:], src[old.BodyOffset:]) {
		return nil, bug("the body changed")
	}
	var oldOrder, newOrder []string
	for key, val := range vault.Pairs(old.Map) {
		k := key.Value
		if touched[k] {
			continue
		}
		oldOrder = append(oldOrder, k)
		if !nodeEqual(val, vault.Field(re.Map, k)) {
			return nil, bug("%s changed", k)
		}
	}
	for key := range vault.Pairs(re.Map) {
		if k := key.Value; !touched[k] && slices.Contains(oldOrder, k) {
			newOrder = append(newOrder, k)
		}
	}
	if !slices.Equal(oldOrder, newOrder) {
		return nil, bug("key order changed")
	}
	for _, k := range pl.setKeys {
		if k == "tags" {
			continue
		}
		if !nodeEqual(vault.Field(p.Set, k), vault.Field(re.Map, k)) {
			return nil, bug("%s was not written as given", k)
		}
	}
	for _, k := range p.Unset {
		if vault.Field(re.Map, k) != nil {
			return nil, bug("%s is still present", k)
		}
	}
	if pl.hasTagChange && !slices.Equal(vault.NormalizeTags(vault.Field(re.Map, "tags")), pl.wantTags) {
		return nil, bug("tags differ")
	}
	return re, nil
}
