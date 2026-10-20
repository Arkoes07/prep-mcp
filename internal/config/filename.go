package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

var placeholderRE = regexp.MustCompile(`\{([^{}]*)\}`)

// Filename is a type's filename pattern, such as "{date}-{kind}-{slug}.md",
// parsed when the config loads.
type Filename struct {
	raw       string
	parts     []string // literal text at even indexes, placeholder names at odd ones
	dateField string   // the type's date field, which fills {date}
}

var _ yaml.Unmarshaler = (*Filename)(nil)

// UnmarshalYAML only keeps the text. Parse parses it, so that a bad pattern is
// reported together with every other problem in the config.
func (f *Filename) UnmarshalYAML(n *yaml.Node) error { return n.Decode(&f.raw) }

func (f Filename) String() string { return f.raw }

// Describe is the pattern as create_doc's description shows it, naming the
// field that fills {date} when it is not the default.
func (f Filename) Describe() string {
	if f.dateField == DefaultDateField || !strings.Contains(f.raw, "{date}") {
		return f.raw
	}
	return fmt.Sprintf("%s ({date}: %s)", f.raw, f.dateField)
}

// parse parses the pattern of type typ, whose required fields and date field
// are given, and reports what is wrong with it.
func (f *Filename) parse(typ string, required []string, dateField string) []error {
	if f.raw == "" {
		return []error{fmt.Errorf(`type %s: filename is required (for example filename: "{slug}.md")`, typ)}
	}
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("type %s: filename %q: "+format, append([]any{typ, f.raw}, args...)...))
	}
	if strings.ContainsAny(f.raw, "/\\") {
		add(`must not contain "/"; the folder setting holds directories`)
	}
	if !strings.HasSuffix(strings.ToLower(f.raw), ".md") {
		add("must end in .md")
	}
	f.dateField = dateField
	f.parts = nil
	last := 0
	for _, m := range placeholderRE.FindAllStringSubmatchIndex(f.raw, -1) {
		f.parts = append(f.parts, f.raw[last:m[0]], f.raw[m[2]:m[3]])
		last = m[1]
	}
	f.parts = append(f.parts, f.raw[last:])
	if strings.ContainsAny(placeholderRE.ReplaceAllString(f.raw, ""), "{}") {
		add("has an unbalanced { or }; write placeholders like {slug}")
	}
	for i := 1; i < len(f.parts); i += 2 {
		switch field := f.parts[i]; {
		case field == "":
			add("has an empty {} placeholder")
		case field == "slug" || field == "date":
		case !slices.Contains(required, field):
			// So the document rules, which run first, report it missing.
			add("uses {%s}, so add %s to required", field, field)
		}
	}
	return errs
}

// FilenameRules says how Render fills a pattern, for create_doc's description.
const FilenameRules = "In a pattern, {slug} is the slug argument, else the title field; " +
	"{date} is the day the type's date field holds (a timestamp is cut to its date), else today; " +
	"any other {name} is the frontmatter field of that name. " +
	"Every value becomes lowercase kebab-case. " +
	"For a type named {slug}.md, slug may hold folders (folder/name); each part is kebab-cased."

// Render fills the pattern for a new document with frontmatter fm, as
// FilenameRules says. A slug's folders are slugified part by part, so the
// result stays below the type folder; no other value can hold a path
// separator. On a case-sensitive file system an existing uppercase file of
// the same name is not detected.
//
// It takes the frontmatter, not a document.Doc, because document imports
// config.
func (f Filename) Render(fm *yaml.Node, slug string, today time.Time) (string, error) {
	if strings.ContainsAny(slug, `/\`) {
		return f.folderName(slug)
	}
	// A date field holding something else is the date rule's to report.
	day, err := vault.Day(fm, f.dateField)
	if err != nil || day.IsZero() {
		day = today
	}
	var b strings.Builder
	for i, part := range f.parts {
		if i%2 == 0 {
			b.WriteString(part)
			continue
		}
		val, err := placeholderValue(part, fm, slug, day)
		if err != nil {
			return "", err
		}
		b.WriteString(val)
	}
	return b.String(), nil
}

// placeholderValue is the value of the {name} placeholder for frontmatter fm;
// day is the already resolved {date}.
func placeholderValue(name string, fm *yaml.Node, slug string, day time.Time) (string, error) {
	switch name {
	case "slug":
		if strings.TrimSpace(slug) != "" {
			if s := slugify(slug); s != "" {
				return s, nil
			}
			return "", fmt.Errorf("slug %q has no letters or digits to build a filename from; pass a different slug",
				slug)
		}
		if s := slugify(vault.Text(fm, vault.FieldTitle)); s != "" {
			return s, nil
		}
		return "", errors.New("no name for the file: pass slug or set title")
	case "date":
		return day.Format(time.DateOnly), nil
	}
	// A field with no value fails here too, but never reaches it: parse makes
	// it required, and the document rules check that first.
	vals := vault.Values(fm, name)
	if len(vals) != 1 {
		return "", fmt.Errorf("%s must be a single value to build the filename, not a list or mapping", name)
	}
	s := slugify(vals[0])
	if s == "" {
		return "", fmt.Errorf("%s (%q) has no letters or digits to build a filename from; use a different value",
			name, vals[0])
	}
	return s, nil
}

// folderName builds "folder/name.md" from a slug that holds folders,
// slugifying each "/"-separated part. A part that slugifies to nothing ("",
// ".", "..") fails, and so does "\\", which would otherwise slugify to "-".
func (f Filename) folderName(slug string) (string, error) {
	if f.raw != "{slug}.md" {
		return "", fmt.Errorf("this type builds names from %q, so slug cannot contain \"/\" or \"\\\"; "+
			"pass a plain name", f.raw)
	}
	parts := strings.Split(slug, "/")
	for i, part := range parts {
		s := slugify(part)
		if s == "" || strings.Contains(part, `\`) {
			return "", fmt.Errorf("slug %q has a part %q with no letters or digits, or with \"\\\"; "+
				"pass folder/name with letters and digits in each part", slug, part)
		}
		parts[i] = s
	}
	return strings.Join(parts, "/") + ".md", nil
}

// slugify turns free text into a lowercase kebab-case file name part. Letters
// and digits of any script are kept; every other run of characters becomes one
// hyphen. It returns "" when nothing usable is left.
func slugify(s string) string {
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
