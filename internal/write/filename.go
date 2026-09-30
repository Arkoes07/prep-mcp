package write

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Slugify turns free text into a lowercase kebab-case file name part. Letters
// and digits of any script are kept; every other run of characters becomes one
// hyphen. It returns "" when nothing usable is left.
func Slugify(s string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
			continue
		}
		pendingHyphen = true
	}
	return b.String()
}

var placeholderRE = regexp.MustCompile(`\{([^{}]+)\}`)

// RenderFilename fills a config filename pattern. {slug} comes from the slug
// argument, else the title field; {date} from the date field, else today; any
// other placeholder from the field of that name. Every value is slugified, so
// the result can never contain a path separator.
func RenderFilename(pattern string, fm *yaml.Node, slug string, today time.Time) (string, error) {
	var firstErr error
	out := placeholderRE.ReplaceAllStringFunc(pattern, func(m string) string {
		name := m[1 : len(m)-1]
		val, err := placeholderValue(name, fm, slug, today)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		return val
	})
	if firstErr != nil {
		return "", firstErr
	}
	return out, nil
}

func placeholderValue(name string, fm *yaml.Node, slug string, today time.Time) (string, error) {
	switch name {
	case "slug":
		if strings.TrimSpace(slug) != "" {
			if s := Slugify(slug); s != "" {
				return s, nil
			}
			return "", fmt.Errorf("slug %q has no letters or digits to build a filename from; pass a different slug", slug)
		}
		if title := fieldText(fm, "title"); title != "" {
			if s := Slugify(title); s != "" {
				return s, nil
			}
		}
		return "", errors.New("no name for the file: pass slug or set title")
	case "date":
		if d := fieldText(fm, "date"); d != "" {
			if s := Slugify(d); s != "" {
				return s, nil
			}
		}
		return today.Format("2006-01-02"), nil
	}
	n := mapGet(fm, name)
	switch {
	case isMissing(n):
		return "", fmt.Errorf("%s is required to build the filename; set %s in the frontmatter", name, name)
	case n.Kind != yaml.ScalarNode:
		return "", fmt.Errorf("%s must be a single value to build the filename, not a list or mapping", name)
	}
	s := Slugify(n.Value)
	if s == "" {
		return "", fmt.Errorf("%s (%q) has no letters or digits to build a filename from; use a different value", name, n.Value)
	}
	return s, nil
}

// fieldText returns the scalar text of a frontmatter field, or "".
func fieldText(fm *yaml.Node, key string) string {
	n := mapGet(fm, key)
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return ""
	}
	return strings.TrimSpace(n.Value)
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

// isMissing is the "required field" test: absent, null, "", [] and {} all count as missing.
func isMissing(n *yaml.Node) bool {
	if n == nil {
		return true
	}
	if n.Kind == yaml.AliasNode && n.Alias != nil {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Tag == "!!null" || strings.TrimSpace(n.Value) == ""
	case yaml.SequenceNode, yaml.MappingNode:
		return len(n.Content) == 0
	}
	return false
}
