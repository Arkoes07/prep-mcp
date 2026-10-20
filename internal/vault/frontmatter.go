package vault

import (
	"bytes"
	"fmt"
	"iter"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the parsed leading YAML block of a file. Read its values
// with Values and Text; Map is not to be changed. Change it with Edit.
type Frontmatter struct {
	Map *yaml.Node // the mapping node; empty (not nil) for an empty block

	// Byte offsets into src (a BOM counts).
	src        []byte
	start, end int  // the YAML text is src[start:end]: after the opening fence, up to the closing fence line
	bodyOffset int  // first byte after the closing fence line
	bodyLine   int  // 1-based file line where the body starts
	crlf       bool // the file uses CRLF line endings
}

// lineShift turns a line yaml.v3 gives a node, counted from 1 in the block,
// into a file line: line 1 is the opening fence.
const lineShift = 1

// Body is the file after the closing fence line.
func (fm *Frontmatter) Body() []byte { return fm.src[fm.bodyOffset:] }

// BodyLine is the 1-based file line where the body starts.
func (fm *Frontmatter) BodyLine() int { return fm.bodyLine }

// Line is the 1-based file line of the top-level key, or 0 when it is missing.
func (fm *Frontmatter) Line(key string) int {
	for k := range Pairs(fm.Map) {
		if k.Value == key {
			return k.Line + lineShift
		}
	}
	return 0
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// FrontmatterError is frontmatter that does not parse: what is wrong, and how
// to fix it in the file. A caller with better advice uses Reason alone.
type FrontmatterError struct {
	Reason string // names the line in the FILE, not in the block
	Fix    string
}

func (e *FrontmatterError) Error() string { return e.Reason + "; " + e.Fix }

// SplitFrontmatter parses the frontmatter of src. It returns nil, nil when
// the file has none: the first line must be a "---" fence. Its errors are
// *FrontmatterError.
func SplitFrontmatter(src []byte) (*Frontmatter, error) {
	pos := 0
	if bytes.HasPrefix(src, bom) {
		pos = len(bom)
	}
	first, next := nextLine(src, pos)
	if !isFence(first) {
		return nil, nil
	}
	fm := &Frontmatter{src: src, start: next, crlf: bytes.HasSuffix(src[pos:next], []byte("\r\n"))}

	line := 2
	for pos = next; pos < len(src); {
		l, n := nextLine(src, pos)
		if isFence(l) {
			fm.end, fm.bodyOffset, fm.bodyLine = pos, n, line+1
			return parseBlock(fm)
		}
		pos = n
		line++
	}
	return nil, &FrontmatterError{
		Reason: "frontmatter not closed",
		Fix:    "add a closing --- line after the last key, or remove the opening ---",
	}
}

// nextLine returns the line starting at pos without its line ending, and the
// offset of the following line.
func nextLine(src []byte, pos int) (line []byte, next int) {
	i := bytes.IndexByte(src[pos:], '\n')
	if i < 0 {
		return src[pos:], len(src)
	}
	return src[pos : pos+i], pos + i + 1
}

// isFence reports whether a line is "---", allowing trailing spaces and a CR.
func isFence(line []byte) bool {
	return string(bytes.TrimRight(line, " \t\r")) == "---"
}

// yamlLine is the "line N: " that yaml.v3 puts before most reasons. N is a
// line of the block, sometimes counted from 0, so it is reported as "near".
var yamlLine = regexp.MustCompile(`^line (\d+): `)

func parseBlock(fm *Frontmatter) (*Frontmatter, error) {
	block := fm.src[fm.start:fm.end]
	var doc yaml.Node
	if err := yaml.Unmarshal(block, &doc); err != nil {
		reason, where := strings.TrimPrefix(err.Error(), "yaml: "), ""
		if m := yamlLine.FindStringSubmatch(reason); m != nil {
			n, _ := strconv.Atoi(m[1]) // the pattern only matches digits
			reason, where = reason[len(m[0]):], fmt.Sprintf(" near line %d", n+lineShift)
		}
		return nil, &FrontmatterError{
			Reason: fmt.Sprintf("invalid frontmatter%s: %s", where, reason),
			Fix:    "fix it in the file, then retry",
		}
	}
	switch {
	case doc.Kind == 0 || len(doc.Content) == 0: // empty, or only comments
		fm.Map = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	case doc.Content[0].Kind != yaml.MappingNode:
		return nil, &FrontmatterError{
			Reason: fmt.Sprintf("frontmatter must be a mapping of key: value lines (line %d is not)",
				doc.Content[0].Line+lineShift),
			Fix: "fix it, or move the content below the closing ---",
		}
	default:
		fm.Map = doc.Content[0]
	}
	// yaml.v3 does not reject duplicate keys when decoding into a Node.
	seen := make(map[string]bool, len(fm.Map.Content)/2)
	for k := range Pairs(fm.Map) {
		if seen[k.Value] {
			return nil, &FrontmatterError{
				Reason: fmt.Sprintf("invalid frontmatter: duplicate key %q at line %d", k.Value, k.Line+lineShift),
				Fix:    "remove one of them",
			}
		}
		seen[k.Value] = true
	}
	return fm, nil
}

// Pairs yields the key and value nodes of a mapping node, in order. It yields
// nothing for nil or for a node that is not a mapping.
func Pairs(m *yaml.Node) iter.Seq2[*yaml.Node, *yaml.Node] {
	return func(yield func(key, val *yaml.Node) bool) {
		if m == nil || m.Kind != yaml.MappingNode {
			return
		}
		for i := 0; i+1 < len(m.Content); i += 2 {
			if !yield(m.Content[i], m.Content[i+1]) {
				return
			}
		}
	}
}

// Field returns the value node of key in a mapping node, or nil.
func Field(m *yaml.Node, key string) *yaml.Node {
	for k, val := range Pairs(m) {
		if k.Value == key {
			return val
		}
	}
	return nil
}
