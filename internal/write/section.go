package write

import (
	"errors"
	"regexp"
	"strings"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

var (
	headingRE  = regexp.MustCompile(`^(#{1,6})[ \t]+(.*?)[ \t]*$`)
	listItemRE = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s+`)
)

// InsertSection adds content to the end of the level-2 "## heading" section of
// src, just before the next # or ## heading, or as a new section at the end of
// the file. Only insertions are made: no existing byte changes. Frontmatter is
// never searched. The last of several equal headings is used; headings inside
// code fences do not count.
func InsertSection(src []byte, heading, content string) ([]byte, error) {
	heading = strings.TrimSpace(heading)
	if strings.ContainsAny(heading, "\r\n") {
		return nil, errors.New("heading must be a single line; pass one line of text such as Log")
	}
	heading = strings.TrimSpace(strings.TrimPrefix(heading, "## "))
	if heading == "" {
		return nil, errors.New("heading is empty; pass the section title, for example Log")
	}
	content = strings.TrimRight(strings.ReplaceAll(content, "\r\n", "\n"), " \t\r\n")
	if strings.TrimSpace(content) == "" {
		return nil, errors.New("content is empty; pass the text to add")
	}

	eol := "\n"
	if i := strings.IndexByte(string(src), '\n'); i > 0 && src[i-1] == '\r' {
		eol = "\r\n"
	}
	text := string(src)

	// Scan the body line by line, remembering where the last matching section is.
	start := 0
	if fm, err := vault.SplitFrontmatter(src); err == nil && fm != nil {
		start = fm.BodyOffset
	}
	type line struct {
		text       string // without the line ending
		begin, end int    // src[begin:end] includes the line ending
	}
	var lines []line
	for pos := start; pos < len(text); {
		end := len(text)
		if i := strings.IndexByte(text[pos:], '\n'); i >= 0 {
			end = pos + i + 1
		}
		lines = append(lines, line{strings.TrimRight(text[pos:end], "\r\n"), pos, end})
		pos = end
	}

	match, inFence := -1, false
	for i, l := range lines {
		if isCodeFence(l.text) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRE.FindStringSubmatch(l.text); m != nil && len(m[1]) == 2 && m[2] == heading {
			match = i
		}
	}

	block := strings.ReplaceAll(content, "\n", eol) + eol

	if match < 0 {
		var b strings.Builder
		if len(text) > 0 {
			if !strings.HasSuffix(text, "\n") {
				b.WriteString(eol)
			}
			if !endsWithBlankLine(text) {
				b.WriteString(eol)
			}
		}
		b.WriteString("## " + heading + eol + eol + block)
		return []byte(text + b.String()), nil
	}

	// The section runs to the next level 1 or 2 heading outside a code fence.
	end := match + 1
	inFence = false
	for ; end < len(lines); end++ {
		l := lines[end]
		if isCodeFence(l.text) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if m := headingRE.FindStringSubmatch(l.text); m != nil && len(m[1]) <= 2 {
			break
		}
	}
	// Insert after the last non-blank line of the section.
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
	firstNew := strings.SplitN(content, "\n", 2)[0]
	if last == match || !(listItemRE.MatchString(lines[last].text) && listItemRE.MatchString(firstNew)) {
		b.WriteString(eol)
	}
	b.WriteString(block)
	return []byte(text[:at] + b.String() + text[at:]), nil
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
