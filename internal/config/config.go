// Package config loads and validates prep.config.yaml.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

const (
	DefaultAuthor     = "Claude <claude@local>"
	DefaultQueryLimit = 20
	DefaultDateField  = "date"
)

// Config is a validated prep.config.yaml with its defaults applied.
type Config struct {
	TagsFile      string `yaml:"tags_file"`
	CommitOnWrite bool   `yaml:"commit_on_write"`
	Author        string `yaml:"author"`
	// SummaryFields are the frontmatter fields shown, in this order, next to
	// each document in list_docs and get_context, unless its type has a show list.
	SummaryFields []string            `yaml:"summary_fields"`
	Types         map[string]TypeSpec `yaml:"types"`
	Context       ContextSpec         `yaml:"context"`
}

type TypeSpec struct {
	Folder   string   `yaml:"folder"`
	Filename string   `yaml:"filename"`
	Required []string `yaml:"required"`
	// DateField is the frontmatter field that dates documents of this type:
	// since/until, date sorting and {date} in the filename use it. Never
	// empty after Parse.
	DateField string `yaml:"date_field"`
	// Show replaces SummaryFields for documents of this type. nil means not
	// set; an empty list shows no fields.
	Show []string `yaml:"show"`
}

type ContextSpec struct {
	CurrentWeekType  string  `yaml:"current_week_type"`
	CurrentWeekField string  `yaml:"current_week_field"`
	OpenItems        []Query `yaml:"open_items"`
	Recent           []Query `yaml:"recent"`
}

// Query is one configured get_context query. Limit is never 0 after Parse.
type Query struct {
	Type    string     `yaml:"type"`
	Where   Conditions `yaml:"where"`
	Tags    []string   `yaml:"tags"`
	AnyTags []string   `yaml:"any_tags"`
	Limit   int        `yaml:"limit"`
}

// Conditions maps a frontmatter field to the values it may equal. In YAML
// each value is one scalar or a list of them, read as a field's values are
// (vault.Values), so tags compare normalised.
type Conditions map[string][]string

var _ yaml.Unmarshaler = (*Conditions)(nil)

func (c *Conditions) UnmarshalYAML(n *yaml.Node) error {
	// Decoding into a map first rejects a non-mapping and duplicate keys.
	var raw map[string]yaml.Node
	if err := n.Decode(&raw); err != nil {
		return err
	}
	*c = make(Conditions, len(raw))
	for k, v := range raw {
		if !vault.IsScalarOrList(&v) {
			return fmt.Errorf("line %d: a where value must be a scalar or a list of scalars", v.Line)
		}
		(*c)[k] = vault.Values(n, k)
	}
	return nil
}

// Parse reads a config, applies defaults and validates it. Unknown keys are
// errors. All validation problems are reported together.
func Parse(data []byte) (*Config, error) {
	// Decoding leaves fields the file does not mention as they are, so the
	// defaults that are not zero values are set first.
	c := &Config{CommitOnWrite: true, Types: map[string]TypeSpec{}}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid YAML: %w (fix the config file and restart)", err)
	}

	c.Author = cmp.Or(c.Author, DefaultAuthor)
	for name, t := range c.Types {
		t.DateField = cmp.Or(t.DateField, DefaultDateField)
		c.Types[name] = t
	}
	for _, qs := range [][]Query{c.Context.OpenItems, c.Context.Recent} {
		for i := range qs {
			qs[i].Limit = cmp.Or(qs[i].Limit, DefaultQueryLimit)
		}
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Load reads and parses the config file at path. The returned error wraps
// fs.ErrNotExist when the file is missing.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	c, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

var authorRE = regexp.MustCompile(`^(.+?)\s*<([^<>\s]+@[^<>\s]+)>$`)

// Validate reports every problem in the config, joined into one error.
func (c *Config) Validate() error {
	var errs []error
	if _, _, ok := ParseAuthor(c.Author); c.Author != "" && !ok {
		errs = append(errs, fmt.Errorf(`author %q must look like "Name <email>"; fix it in the config`, c.Author))
	}
	if c.TagsFile != "" {
		if err := checkRelPath(c.TagsFile); err != nil {
			errs = append(errs, fmt.Errorf("tags_file %q: %w", c.TagsFile, err))
		}
	}
	errs = append(errs, fieldListErrors("summary_fields", c.SummaryFields, alwaysShown)...)
	errs = append(errs, c.typeErrors()...)
	errs = append(errs, c.weekErrors()...)
	errs = append(errs, c.queryErrors()...)
	return errors.Join(errs...)
}

// alwaysShown are the fields every document's metadata already carries, so
// they cannot be summary fields as well.
var alwaysShown = []string{"type", "title", "tags", "date", "updated"}

// fieldListErrors checks a list of fields to show, named label in the
// messages, for empty names, names in shown and repeats.
func fieldListErrors(label string, fields, shown []string) []error {
	var errs []error
	seen := map[string]bool{}
	for i, f := range fields {
		switch {
		case strings.TrimSpace(f) == "":
			errs = append(errs, fmt.Errorf("%s[%d] is empty; name a frontmatter field or remove the entry", label, i))
		case slices.Contains(shown, f):
			errs = append(errs, fmt.Errorf("%s: %s is always shown; remove it from the list", label, f))
		case seen[f]:
			errs = append(errs, fmt.Errorf("%s: %s is listed twice; remove one", label, f))
		}
		seen[f] = true
	}
	return errs
}

// notDates are the always-shown fields that can never date a document.
var notDates = []string{"type", "title", "tags"}

func (c *Config) typeErrors() []error {
	var errs []error
	for _, name := range c.TypeNames() {
		t := c.Types[name]
		if strings.TrimSpace(t.DateField) == "" || slices.Contains(notDates, t.DateField) {
			errs = append(errs, fmt.Errorf(
				"type %s: date_field %q cannot hold a date; "+
					"name the field that holds YYYY-MM-DD, or remove date_field to use %s",
				name,
				t.DateField,
				DefaultDateField,
			))
		}
		// The type's date field is shown as date already.
		shown := append(slices.Clone(alwaysShown), t.DateField)
		errs = append(errs, fieldListErrors("type "+name+": show", t.Show, shown)...)
		if t.Folder == "" {
			errs = append(errs, fmt.Errorf("type %s: folder is required (for example folder: notes)", name))
		} else if err := checkFolder(t.Folder); err != nil {
			errs = append(errs, fmt.Errorf("type %s: folder %q: %w", name, t.Folder, err))
		}
		if t.Filename == "" {
			errs = append(errs, fmt.Errorf(`type %s: filename is required (for example filename: "{slug}.md")`, name))
		} else {
			errs = append(errs, checkFilename(name, t)...)
		}
	}
	return errs
}

// weekErrors checks that the current-week settings name a type and one of
// its required fields, or are both left out.
func (c *Config) weekErrors() []error {
	ctx := c.Context
	if ctx.CurrentWeekType == "" {
		if ctx.CurrentWeekField != "" {
			return []error{errors.New(
				"context.current_week_field is set but context.current_week_type is not; set both or neither")}
		}
		return nil
	}
	t, ok := c.Types[ctx.CurrentWeekType]
	switch {
	case !ok:
		return []error{fmt.Errorf("context.current_week_type %q is not a defined type; defined types: %s",
			ctx.CurrentWeekType, c.TypeList())}
	case ctx.CurrentWeekField == "":
		return []error{errors.New("context.current_week_type is set; " +
			"set context.current_week_field to the field that holds the week (for example week)")}
	case !slices.Contains(t.Required, ctx.CurrentWeekField):
		return []error{fmt.Errorf("type %s: add %s to required (context.current_week_field uses it)",
			ctx.CurrentWeekType, ctx.CurrentWeekField)}
	}
	return nil
}

func (c *Config) queryErrors() []error {
	var errs []error
	for _, group := range []struct {
		name string
		qs   []Query
	}{
		{name: "open_items", qs: c.Context.OpenItems},
		{name: "recent", qs: c.Context.Recent},
	} {
		for i, q := range group.qs {
			if _, ok := c.Types[q.Type]; !ok {
				errs = append(errs, fmt.Errorf("context.%s[%d]: type %q is not a defined type; defined types: %s",
					group.name, i, q.Type, c.TypeList()))
			}
			if q.Limit < 0 {
				errs = append(errs, fmt.Errorf(
					"context.%s[%d]: limit must be positive (or leave it out for the default %d)",
					group.name, i, DefaultQueryLimit))
			}
		}
	}
	return errs
}

func checkRelPath(p string) error {
	switch {
	case strings.ContainsAny(p, "\\\x00"):
		return errors.New(`use "/" separators and no NUL`)
	case strings.HasPrefix(p, "/"):
		return errors.New("must be relative to the vault root")
	}
	if slices.Contains(strings.Split(path.Clean(p), "/"), "..") {
		return errors.New("must stay inside the vault")
	}
	return nil
}

func checkFolder(p string) error {
	if err := checkRelPath(p); err != nil {
		return err
	}
	clean := path.Clean(p)
	if clean == "." {
		return errors.New("name a sub-folder such as notes")
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".") {
			return errors.New("hidden folders (starting with a dot) are not allowed")
		}
	}
	if strings.EqualFold(parts[0], "templates") {
		return errors.New("templates/ is read-only; choose another folder")
	}
	return nil
}

var placeholderRE = regexp.MustCompile(`\{[^{}]*\}`)

func checkFilename(name string, t TypeSpec) []error {
	var errs []error
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("type %s: filename %q: "+format, append([]any{name, t.Filename}, args...)...))
	}
	if strings.ContainsAny(t.Filename, "/\\") {
		add(`must not contain "/"; the folder setting holds directories`)
	}
	if !strings.HasSuffix(strings.ToLower(t.Filename), ".md") {
		add("must end in .md")
	}
	rest := placeholderRE.ReplaceAllString(t.Filename, "")
	if strings.ContainsAny(rest, "{}") {
		add("has an unbalanced { or }; write placeholders like {slug}")
	}
	for _, m := range placeholderRE.FindAllString(t.Filename, -1) {
		field := m[1 : len(m)-1]
		switch {
		case field == "":
			add("has an empty {} placeholder")
		case field == "slug" || field == "date":
		case !slices.Contains(t.Required, field):
			add("uses {%s}, so add %s to required", field, field)
		}
	}
	return errs
}

// TypeList is the configured type names, sorted and comma-separated, or a
// hint to add a types: section when there are none. Error messages use it.
func (c *Config) TypeList() string {
	if len(c.Types) == 0 {
		return "none (add a types: section to prep.config.yaml and restart the server)"
	}
	return strings.Join(c.TypeNames(), ", ")
}

// TypeNames returns the configured type names, sorted.
func (c *Config) TypeNames() []string {
	return slices.Sorted(maps.Keys(c.Types))
}

// DateField is the field that dates documents of type typ: the type's
// date_field, or DefaultDateField for a type that is not configured.
func (c *Config) DateField(typ string) string {
	return cmp.Or(c.Types[typ].DateField, DefaultDateField)
}

// ShowFields are the fields shown next to documents of type typ: the type's
// show list when it sets one, else SummaryFields.
func (c *Config) ShowFields(typ string) []string {
	if show := c.Types[typ].Show; show != nil {
		return show
	}
	return c.SummaryFields
}

// ParseAuthor splits "Name <email>" into its parts. ok is false when a does
// not have that form. It is the one definition of a valid author, used by
// Validate and by the git commit step.
func ParseAuthor(a string) (name, email string, ok bool) {
	m := authorRE.FindStringSubmatch(strings.TrimSpace(a))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
