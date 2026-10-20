// Package config loads and validates prep.config.yaml, and renders file names
// from each type's filename pattern.
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
	Filename Filename `yaml:"filename"`
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
	Type    string      `yaml:"type"`
	Where   vault.Where `yaml:"where"`
	Tags    []string    `yaml:"tags"`
	AnyTags []string    `yaml:"any_tags"`
	Limit   int         `yaml:"limit"`
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
	var errs []error
	for _, name := range c.TypeNames() {
		t := c.Types[name]
		t.DateField = cmp.Or(t.DateField, DefaultDateField)
		errs = append(errs, t.Filename.parse(name, t.Required, t.DateField)...)
		c.Types[name] = t
	}
	for _, qs := range [][]Query{c.Context.OpenItems, c.Context.Recent} {
		for i := range qs {
			qs[i].Limit = cmp.Or(qs[i].Limit, DefaultQueryLimit)
		}
	}
	if err := errors.Join(append(errs, c.Validate())...); err != nil {
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

// Validate reports every problem in the config, joined into one error, but
// those in filename patterns: Parse reports them as it parses the patterns.
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
	errs = append(errs, fieldListErrors("summary_fields", c.SummaryFields, AlwaysShown, DefaultDateField)...)
	errs = append(errs, c.typeErrors()...)
	errs = append(errs, c.weekErrors()...)
	errs = append(errs, c.queryErrors()...)
	return errors.Join(errs...)
}

// AlwaysShown are the keys every listing shows for a document (read.Meta's,
// but path and fields), so no list of fields to show can name them.
var AlwaysShown = []string{"type", "title", "tags", "date", "date_source", "updated"}

// fieldListErrors checks a list of fields to show, named label in the
// messages, for empty names, names in shown and repeats. dateField dates the
// documents the list is for.
func fieldListErrors(label string, fields, shown []string, dateField string) []error {
	var errs []error
	seen := map[string]bool{}
	for i, f := range fields {
		switch {
		case strings.TrimSpace(f) == "":
			errs = append(errs, fmt.Errorf("%s[%d] is empty; name a frontmatter field or remove the entry", label, i))
		case f == DefaultDateField && dateField != DefaultDateField:
			// The field would collide with the date key, which holds dateField.
			errs = append(errs, fmt.Errorf("%s: date is get_context's date attribute (this type is dated by %s); "+
				`remove it, and pass fields: ["date"] to list_docs when you need it`, label, dateField))
		case slices.Contains(shown, f):
			errs = append(errs, fmt.Errorf("%s: %s is always shown; remove it from the list", label, f))
		case seen[f]:
			errs = append(errs, fmt.Errorf("%s: %s is listed twice; remove one", label, f))
		}
		seen[f] = true
	}
	return errs
}

// notDates are the frontmatter fields that can never date a document.
var notDates = []string{vault.FieldType, vault.FieldTitle, vault.FieldTags}

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
		shown := append(slices.Clone(AlwaysShown), t.DateField)
		errs = append(errs, fieldListErrors("type "+name+": show", t.Show, shown, t.DateField)...)
		if t.Folder == "" {
			errs = append(errs, fmt.Errorf("type %s: folder is required (for example folder: notes)", name))
		} else if err := checkFolder(t.Folder); err != nil {
			errs = append(errs, fmt.Errorf("type %s: folder %q: %w", name, t.Folder, err))
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
	if vault.InTemplates(clean) {
		return errors.New("templates/ holds templates, not documents; choose another folder")
	}
	return nil
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
// not have that form. It is the one definition of a valid author.
func ParseAuthor(a string) (name, email string, ok bool) {
	m := authorRE.FindStringSubmatch(strings.TrimSpace(a))
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}
