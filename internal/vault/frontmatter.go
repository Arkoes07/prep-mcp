package vault

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"

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

// yamlPrefix strips yaml.v3's "yaml: " and "line N: " from an error, keeping
// the reason. The line is dropped because yaml.v3 counts it from 0 for some
// errors and from 1 for others; badLine finds the real one.
var yamlPrefix = regexp.MustCompile(`^yaml: (line \d+: )?`)

func parseBlock(src []byte, fm *Frontmatter) (*Frontmatter, error) {
	// Block line 1 is file line 2 (line 1 is the opening fence).
	const lineShift = 1
	block := src[fm.Start:fm.End]
	var doc yaml.Node
	if err := yaml.Unmarshal(block, &doc); err != nil {
		reason := yamlPrefix.ReplaceAllString(err.Error(), "")
		return nil, fmt.Errorf("invalid frontmatter at line %d: %s; fix that line in the file, then retry", badLine(block)+lineShift, reason)
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

// maxPrefixParses bounds the work badLine does on a huge block.
const maxPrefixParses = 1000

// badLine returns the 1-based line of block where its YAML stops being valid:
// the smallest n such that no prefix of n or more lines parses. A prefix that
// ends inside a legal multi-line value fails too, but a longer prefix then
// parses again, so only the failure that lasts to the end counts. For an
// unterminated [, { or " that is the line that opens it.
func badLine(block []byte) int {
	var ends []int // ends[i] is the offset just after line i+1
	for pos := 0; pos < len(block); {
		_, next := nextLine(block, pos)
		ends = append(ends, next)
		pos = next
	}
	valid := func(n int) bool {
		var d yaml.Node
		return yaml.Unmarshal(block[:ends[n-1]], &d) == nil
	}
	n := len(ends)
	for tries := 0; n > 1 && !valid(n-1); n-- {
		if tries++; tries > maxPrefixParses {
			break
		}
	}
	return max(n, 1)
}

// Field returns the value node of key in a mapping node, or nil.
func Field(m *yaml.Node, key string) *yaml.Node {
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

// FieldText returns the trimmed text of a scalar field of a mapping node, or
// "" if the field is missing, null or not a scalar.
func FieldText(m *yaml.Node, key string) string {
	return strings.TrimSpace(scalar(Field(m, key)))
}
