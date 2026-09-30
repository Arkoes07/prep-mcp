package vault

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Frontmatter is the parsed leading YAML block of a file.
//
// All offsets are byte offsets into the original source (a BOM counts).
type Frontmatter struct {
	Map        *yaml.Node // the mapping node; empty (not nil) for an empty block
	Start, End int        // the YAML text is src[Start:End]: after the opening fence, up to the closing fence line
	BodyOffset int        // first byte after the closing fence line
	BodyLine   int        // 1-based file line where the body starts
	CRLF       bool       // the file uses CRLF line endings
}

var bom = []byte{0xEF, 0xBB, 0xBF}

// SplitFrontmatter parses the frontmatter of src. It returns nil, nil when
// the file has none: the first line must be a "---" fence. Errors name the
// line in the FILE, not in the block.
func SplitFrontmatter(src []byte) (*Frontmatter, error) {
	pos := 0
	if bytes.HasPrefix(src, bom) {
		pos = len(bom)
	}
	first, next := nextLine(src, pos)
	if !isFence(first) {
		return nil, nil
	}
	fm := &Frontmatter{Start: next, CRLF: bytes.HasSuffix(src[pos:next], []byte("\r\n"))}

	line := 2
	for pos = next; pos < len(src); {
		l, n := nextLine(src, pos)
		if isFence(l) {
			fm.End, fm.BodyOffset, fm.BodyLine = pos, n, line+1
			return parseBlock(src, fm)
		}
		pos = n
		line++
	}
	return nil, errors.New("frontmatter not closed: add a closing --- line after the last key, or remove the opening ---")
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

var yamlLineRE = regexp.MustCompile(`line (\d+)`)

func parseBlock(src []byte, fm *Frontmatter) (*Frontmatter, error) {
	// Block line 1 is file line 2 (line 1 is the opening fence).
	const lineShift = 1
	var doc yaml.Node
	if err := yaml.Unmarshal(src[fm.Start:fm.End], &doc); err != nil {
		msg := yamlLineRE.ReplaceAllStringFunc(err.Error(), func(m string) string {
			n, _ := strconv.Atoi(m[len("line "):])
			return "line " + strconv.Itoa(n+lineShift)
		})
		return nil, fmt.Errorf("invalid frontmatter: %s; fix that line in the file, then retry", msg)
	}
	switch {
	case doc.Kind == 0 || len(doc.Content) == 0: // empty, or only comments
		fm.Map = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	case doc.Content[0].Kind != yaml.MappingNode:
		return nil, fmt.Errorf("frontmatter must be a mapping of key: value lines (line %d is not); fix it, or move the content below the closing ---", doc.Content[0].Line+lineShift)
	default:
		fm.Map = doc.Content[0]
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(fm.Map.Content); i += 2 {
		k := fm.Map.Content[i]
		if seen[k.Value] {
			return nil, fmt.Errorf("invalid frontmatter: duplicate key %q at line %d; remove one of them", k.Value, k.Line+lineShift)
		}
		seen[k.Value] = true
	}
	return fm, nil
}

// mapGet returns the value node of key in a mapping node, or nil.
func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
