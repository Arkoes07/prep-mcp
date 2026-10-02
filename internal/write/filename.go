package write

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/vault"
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

// RenderFilename fills the filename pattern of type spec. {slug} comes from
// the slug argument, else the title field; {date} from the type's date field,
// else today; any other placeholder from the field of that name. Every value
// is slugified, so the result can never contain a path separator.
func RenderFilename(spec config.TypeSpec, fm *yaml.Node, slug string, today time.Time) (string, error) {
	date := today.Format(time.DateOnly)
	if d := Slugify(vault.FieldText(fm, spec.DateField)); d != "" {
		date = d
	}
	var firstErr error
	out := placeholderRE.ReplaceAllStringFunc(spec.Filename, func(m string) string {
		name := m[1 : len(m)-1]
		val, err := placeholderValue(name, fm, slug, date)
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

// placeholderValue is the value of one {name} placeholder; date is the
// already resolved {date}.
func placeholderValue(name string, fm *yaml.Node, slug, date string) (string, error) {
	switch name {
	case "slug":
		if strings.TrimSpace(slug) != "" {
			if s := Slugify(slug); s != "" {
				return s, nil
			}
			return "", fmt.Errorf("slug %q has no letters or digits to build a filename from; pass a different slug",
				slug)
		}
		if title := vault.FieldText(fm, "title"); title != "" {
			if s := Slugify(title); s != "" {
				return s, nil
			}
		}
		return "", errors.New("no name for the file: pass slug or set title")
	case "date":
		return date, nil
	}
	n := vault.Field(fm, name)
	switch {
	case isMissing(n):
		return "", fmt.Errorf("%s is required to build the filename; set %s in the frontmatter", name, name)
	case n.Kind != yaml.ScalarNode:
		return "", fmt.Errorf("%s must be a single value to build the filename, not a list or mapping", name)
	}
	s := Slugify(n.Value)
	if s == "" {
		return "", fmt.Errorf("%s (%q) has no letters or digits to build a filename from; use a different value",
			name, n.Value)
	}
	return s, nil
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
