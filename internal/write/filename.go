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
// is slugified and always lowercase. Only a pattern of exactly "{slug}.md"
// lets a slug argument hold folders ("folder/name"); each part is slugified
// alone, so the result stays below the type folder. Every other value can
// never contain a path separator. On a case-sensitive file system an existing
// uppercase file of the same name is not detected.
func RenderFilename(spec config.TypeSpec, fm *yaml.Node, slug string, today time.Time) (string, error) {
	if strings.ContainsAny(slug, `/\`) {
		return folderFilename(spec.Filename, slug)
	}
	date := today.Format(time.DateOnly)
	if d := Slugify(vault.Text(fm, spec.DateField)); d != "" {
		date = d
	}
	var firstErr error
	out := placeholderRE.ReplaceAllStringFunc(spec.Filename, func(m string) string {
		name := m[1 : len(m)-1]
		val, err := placeholderValue(
			name,
			fm,
			slug,
			date,
		)
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
		if title := vault.Text(fm, "title"); title != "" {
			if s := Slugify(title); s != "" {
				return s, nil
			}
		}
		return "", errors.New("no name for the file: pass slug or set title")
	case "date":
		return date, nil
	}
	vals := vault.Values(fm, name)
	switch {
	case !vault.HasValue(fm, name):
		return "", fmt.Errorf("%s is required to build the filename; set %s in the frontmatter", name, name)
	case len(vals) != 1:
		return "", fmt.Errorf("%s must be a single value to build the filename, not a list or mapping", name)
	}
	s := Slugify(vals[0])
	if s == "" {
		return "", fmt.Errorf("%s (%q) has no letters or digits to build a filename from; use a different value",
			name, vals[0])
	}
	return s, nil
}

// folderFilename builds "folder/name.md" from a slug that holds folders,
// slugifying each "/"-separated part. A part that slugifies to nothing ("",
// ".", "..") fails, and so does "\\", which would otherwise slugify to "-".
func folderFilename(pattern, slug string) (string, error) {
	if pattern != "{slug}.md" {
		return "", fmt.Errorf("this type builds names from %q, so slug cannot contain \"/\" or \"\\\"; "+
			"pass a plain name", pattern)
	}
	parts := strings.Split(slug, "/")
	for i, part := range parts {
		s := Slugify(part)
		if s == "" || strings.Contains(part, `\`) {
			return "", fmt.Errorf("slug %q has a part %q with no letters or digits, or with \"\\\"; "+
				"pass folder/name with letters and digits in each part", slug, part)
		}
		parts[i] = s
	}
	return strings.Join(parts, "/") + ".md", nil
}
