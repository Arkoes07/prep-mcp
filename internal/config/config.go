// Package config loads and validates prep.config.yaml.
package config

import (
	"bytes"
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
)

const (
	// DefaultAuthor is used for commits when the config sets none.
	DefaultAuthor = "Claude <claude@local>"
	// DefaultQueryLimit is the limit of a context query that sets none.
	DefaultQueryLimit = 20
)

// Config is a validated prep.config.yaml with its defaults applied.
type Config struct {
	TagsFile      string
	CommitOnWrite bool
	Author        string
	Types         map[string]TypeSpec
	Context       ContextSpec
}

// TypeSpec is one document type: where its files live, how they are named
// and which frontmatter fields they must have.
type TypeSpec struct {
	Folder, Filename string
	Required         []string
}

// ContextSpec says what get_context returns besides the working rules.
type ContextSpec struct {
	CurrentWeekType, CurrentWeekField string
	OpenItems, Recent                 []Query
}

// Query is one configured get_context query. Limit is never 0 after Parse.
type Query struct {
	Type          string
	Where         map[string][]string
	Tags, AnyTags []string
	Limit         int
}

// scalarOrList decodes a YAML scalar as one value and a sequence as many.
type scalarOrList []string

func (s *scalarOrList) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*s = []string{n.Value}
	case yaml.SequenceNode:
		for _, c := range n.Content {
			if c.Kind != yaml.ScalarNode {
				return fmt.Errorf("line %d: a where value must be a scalar or a list of scalars", c.Line)
			}
			*s = append(*s, c.Value)
		}
	default:
		return fmt.Errorf("line %d: a where value must be a scalar or a list of scalars", n.Line)
	}
	return nil
}

type rawConfig struct {
	TagsFile      string             `yaml:"tags_file"`
	CommitOnWrite *bool              `yaml:"commit_on_write"`
	Author        string             `yaml:"author"`
	Types         map[string]rawType `yaml:"types"`
	Context       rawContext         `yaml:"context"`
}

type rawType struct {
	Folder   string   `yaml:"folder"`
	Filename string   `yaml:"filename"`
	Required []string `yaml:"required"`
}

type rawContext struct {
	CurrentWeekType  string     `yaml:"current_week_type"`
	CurrentWeekField string     `yaml:"current_week_field"`
	OpenItems        []rawQuery `yaml:"open_items"`
	Recent           []rawQuery `yaml:"recent"`
}

type rawQuery struct {
	Type    string                  `yaml:"type"`
	Where   map[string]scalarOrList `yaml:"where"`
	Tags    []string                `yaml:"tags"`
	AnyTags []string                `yaml:"any_tags"`
	Limit   *int                    `yaml:"limit"`
}

// Parse reads a config, applies defaults and validates it. Unknown keys are
// errors. All validation problems are reported together.
func Parse(data []byte) (*Config, error) {
	var raw rawConfig
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid YAML: %w (fix the config file and restart)", err)
	}

	c := &Config{
		TagsFile:      raw.TagsFile,
		CommitOnWrite: raw.CommitOnWrite == nil || *raw.CommitOnWrite,
		Author:        raw.Author,
		Types:         make(map[string]TypeSpec, len(raw.Types)),
		Context: ContextSpec{
			CurrentWeekType:  raw.Context.CurrentWeekType,
			CurrentWeekField: raw.Context.CurrentWeekField,
			OpenItems:        convertQueries(raw.Context.OpenItems),
			Recent:           convertQueries(raw.Context.Recent),
		},
	}
	if c.Author == "" {
		c.Author = DefaultAuthor
	}
	for name, t := range raw.Types {
		c.Types[name] = TypeSpec(t)
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func convertQueries(in []rawQuery) []Query {
	out := make([]Query, len(in))
	for i, q := range in {
		out[i] = Query{
			Type:    q.Type,
			Tags:    q.Tags,
			AnyTags: q.AnyTags,
			Limit:   DefaultQueryLimit,
		}
		if q.Limit != nil && *q.Limit != 0 {
			out[i].Limit = *q.Limit
		}
		if len(q.Where) > 0 {
			out[i].Where = make(map[string][]string, len(q.Where))
			for k, v := range q.Where {
				out[i].Where[k] = v
			}
		}
	}
	return out
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
	add := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if _, _, ok := ParseAuthor(c.Author); c.Author != "" && !ok {
		add(`author %q must look like "Name <email>"; fix it in the config`, c.Author)
	}
	if c.TagsFile != "" {
		if err := checkRelPath(c.TagsFile); err != nil {
			add("tags_file %q: %v", c.TagsFile, err)
		}
	}

	for _, name := range c.TypeNames() {
		t := c.Types[name]
		if t.Folder == "" {
			add("type %s: folder is required (for example folder: notes)", name)
		} else if err := checkFolder(t.Folder); err != nil {
			add("type %s: folder %q: %v", name, t.Folder, err)
		}
		if t.Filename == "" {
			add(`type %s: filename is required (for example filename: "{slug}.md")`, name)
		} else {
			errs = append(errs, checkFilename(name, t)...)
		}
	}

	ctx := c.Context
	switch {
	case ctx.CurrentWeekType == "" && ctx.CurrentWeekField != "":
		add("context.current_week_field is set but context.current_week_type is not; set both or neither")
	case ctx.CurrentWeekType != "":
		t, ok := c.Types[ctx.CurrentWeekType]
		switch {
		case !ok:
			add("context.current_week_type %q is not a defined type; defined types: %s",
				ctx.CurrentWeekType, c.TypeList())
		case ctx.CurrentWeekField == "":
			add("context.current_week_type is set; " +
				"set context.current_week_field to the field that holds the week (for example week)")
		case !slices.Contains(t.Required, ctx.CurrentWeekField):
			add("type %s: add %s to required (context.current_week_field uses it)",
				ctx.CurrentWeekType, ctx.CurrentWeekField)
		}
	}

	for _, group := range []struct {
		name string
		qs   []Query
	}{
		{name: "open_items", qs: ctx.OpenItems},
		{name: "recent", qs: ctx.Recent},
	} {
		for i, q := range group.qs {
			if _, ok := c.Types[q.Type]; !ok {
				add("context.%s[%d]: type %q is not a defined type; defined types: %s",
					group.name, i, q.Type, c.TypeList())
			}
			if q.Limit < 0 {
				add("context.%s[%d]: limit must be positive (or leave it out for the default %d)",
					group.name, i, DefaultQueryLimit)
			}
		}
	}
	return errors.Join(errs...)
}

// checkRelPath accepts a clean, relative, slash-separated path that stays inside the vault.
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

// checkFolder additionally refuses hidden folders and the read-only templates folder.
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

// checkFilename validates one type's filename pattern.
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

// Type returns the named type.
func (c *Config) Type(name string) (TypeSpec, bool) {
	t, ok := c.Types[name]
	return t, ok
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
