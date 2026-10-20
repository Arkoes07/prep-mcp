package write

import (
	"errors"
	"iter"
	"regexp"
	"strings"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

var (
	headingRE  = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t]*$`)
	listItemRE = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)
)

// AppendOp adds content to the end of a "## heading" section, or adds the
// section at the end of the file.
func AppendOp(path, heading, content string) Op {
	return Op{tool: ToolAppend, target: path, apply: func(p *batchPlan) (*file, []byte, []string, error) {
		return p.modify(path, contentRules, func(f *file, _ parsed) ([]byte, *vault.Frontmatter, []string, error) {
			next, err := insertSection(f.cur, heading, content)
			return next, nil, nil, err
		})
	}}
}

// insertSection adds content to the end of the level-2 "## heading" section of
// src, just before the next # or ## heading, or as a new section at the end of
// the file. Only insertions are made: no existing byte changes. Frontmatter is
// never searched. The last of several equal headings is used; headings inside
// code fences do not count.
func insertSection(src []byte, heading, content string) ([]byte, error) {
	heading, content, err := cleanSectionArgs(heading, content)
	if err != nil {
		return nil, err
	}
	text := string(src)
	eol := "\n"
	if i := strings.IndexByte(text, '\n'); i > 0 && text[i-1] == '\r' {
		eol = "\r\n"
	}
	block := strings.ReplaceAll(content, "\n", eol) + eol

	lines := bodyLines(src)
	match := findSection(lines, heading)
	if match < 0 {
		return []byte(text + newSection(text, heading, block, eol)), nil
	}

	// Insert after the last non-blank line of the section, so blank lines before
	// the next heading stay where they were.
	end := sectionEnd(lines, match)
	last := match
	for i := match + 1; i < end; i++ {
		if strings.TrimSpace(lines[i].text) != "" {
			last = i
		}
	}
	at := lines[last].end
	var b strings.Builder
	if !strings.HasSuffix(text[:at], "\n") { // the section's last line ends the file without a newline
		b.WriteString(eol)
	}
	firstNew, _, _ := strings.Cut(content, "\n")
	sectionIsEmpty := last == match
	continuesList := listItemRE.MatchString(lines[last].text) && listItemRE.MatchString(firstNew)
	if sectionIsEmpty || !continuesList {
		b.WriteString(eol)
	}
	b.WriteString(block)
	return []byte(text[:at] + b.String() + text[at:]), nil
}

// cleanSectionArgs trims the heading (a leading "## " is allowed) and the
// content, normalises the content to LF, and rejects empty or multi-line input.
func cleanSectionArgs(heading, content string) (string, string, error) {
	heading = strings.TrimSpace(heading)
	if strings.ContainsAny(heading, "\r\n") {
		return "", "", errors.New("heading must be a single line; pass one line of text such as Log")
	}
	heading = strings.TrimSpace(strings.TrimPrefix(heading, "## "))
	if heading == "" {
		return "", "", errors.New("heading is empty; pass the section title, for example Log")
	}
	content = strings.TrimRight(strings.ReplaceAll(content, "\r\n", "\n"), " \t\r\n")
	if strings.TrimSpace(content) == "" {
		return "", "", errors.New("content is empty; pass the text to add")
	}
	return heading, content, nil
}

type line struct {
	text string // without the line ending
	end  int    // offset just after the line ending
}

// bodyLines splits src into lines, starting after the frontmatter if it has
// valid frontmatter.
func bodyLines(src []byte) []line {
	text := string(src)
	start := 0
	if fm, err := vault.SplitFrontmatter(src); err == nil && fm != nil {
		start = len(src) - len(fm.Body())
	}
	var lines []line
	end := start
	for l := range strings.Lines(text[start:]) {
		end += len(l)
		lines = append(lines, line{text: strings.TrimRight(l, "\r\n"), end: end})
	}
	return lines
}

// unfenced yields the lines from index from on that are outside code fences,
// with their index. The fence lines themselves are not yielded.
func unfenced(lines []line, from int) iter.Seq2[int, line] {
	return func(yield func(int, line) bool) {
		inFence := false
		for i := from; i < len(lines); i++ {
			if isCodeFence(lines[i].text) {
				inFence = !inFence
				continue
			}
			if !inFence && !yield(i, lines[i]) {
				return
			}
		}
	}
}

// headingLevel returns the level and title of a markdown heading line, or 0
// when the line is not a heading.
func headingLevel(l string) (level int, title string) {
	m := headingRE.FindStringSubmatch(l)
	if m == nil {
		return 0, ""
	}
	return len(m[1]), m[2]
}

// findSection returns the index of the last "## heading" line, or -1.
func findSection(lines []line, heading string) int {
	match := -1
	for i, l := range unfenced(lines, 0) {
		if level, title := headingLevel(l.text); level == 2 && title == heading {
			match = i
		}
	}
	return match
}

// sectionEnd returns the index of the next level 1 or 2 heading after the
// heading at match, or len(lines) when the section runs to the end.
func sectionEnd(lines []line, match int) int {
	for i, l := range unfenced(lines, match+1) {
		if level, _ := headingLevel(l.text); level > 0 && level <= 2 {
			return i
		}
	}
	return len(lines)
}

// newSection is the text that adds "## heading" with block at the end of text,
// separated from what is there by one blank line.
func newSection(text, heading, block, eol string) string {
	var b strings.Builder
	if len(text) > 0 {
		if !strings.HasSuffix(text, "\n") {
			b.WriteString(eol)
		}
		if !endsWithBlankLine(text) {
			b.WriteString(eol)
		}
	}
	for _, part := range []string{"## ", heading, eol, eol, block} {
		b.WriteString(part)
	}
	return b.String()
}

func isCodeFence(l string) bool {
	t := strings.TrimLeft(l, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// endsWithBlankLine reports whether the text ends with an empty line after at
// least one other line, so a new section needs no extra separator.
func endsWithBlankLine(s string) bool {
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	i := strings.LastIndexByte(s, '\n')
	return i >= 0 && strings.TrimSpace(s[i+1:]) == ""
}
