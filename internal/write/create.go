package write

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

type Draft struct {
	Type        string     // a configured type
	Frontmatter *yaml.Node // a mapping in the order to write it; nil for none
	Body        string
	Slug        string // fills {slug} in the filename; empty means the title field
}

// NewCreateOp returns the op that makes a new document of a configured type.
// The path comes from the type's folder and filename pattern.
func NewCreateOp(d Draft) Op {
	return Op{tool: ToolCreate, target: d.Type, apply: func(p *batchPlan) (*file, []byte, []string, error) {
		return p.create(d)
	}}
}

// create builds a new document's path and content. It fails if the file
// exists on disk or was created earlier in the batch.
func (p *batchPlan) create(d Draft) (*file, []byte, []string, error) {
	w := p.w
	spec, ok := w.cfg.Types[d.Type]
	if !ok {
		return nil, nil, nil, fmt.Errorf("unknown type %q; %s", d.Type, p.suggestTypes())
	}
	day := w.now()
	fm, err := newFrontmatter(d.Type, d.Frontmatter, day.Format(time.DateOnly))
	if err != nil {
		return nil, nil, nil, err
	}
	body := d.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	data := []byte("---\n" + vault.Encode(fm) + "---\n" + body)
	// Before the file name, which may need a field the rules find missing: one
	// error then names every missing field.
	doc, warnings, err := p.check(createRules, parsed{}, parse(data))
	if err != nil {
		return nil, nil, nil, err
	}

	name, err := spec.Filename.Render(doc.Frontmatter.Map, d.Slug, day)
	if err != nil {
		return nil, nil, nil, err
	}
	rel, err := w.v.Resolve(path.Join(spec.Folder, name), vault.AccessWrite)
	if err != nil {
		return nil, nil, nil, err
	}
	f, err := p.loadFile(rel)
	if err != nil {
		return nil, nil, nil, err
	}
	if f.exists {
		return nil, nil, nil, fmt.Errorf("file exists: %s (use append_section or update_frontmatter)", rel)
	}
	return f, data, warnings, nil
}

// newFrontmatter builds a new document's frontmatter: type first, then the
// caller's keys in their order with tags normalised, then created (today)
// unless the caller set it.
func newFrontmatter(typ string, fm *yaml.Node, today string) (*yaml.Node, error) {
	if fm != nil && fm.Kind != yaml.MappingNode {
		return nil, errors.New("frontmatter must be an object of key: value pairs")
	}

	doc := &yaml.Node{
		Kind:    yaml.MappingNode,
		Tag:     "!!map",
		Content: []*yaml.Node{vault.NewKeyNode(vault.FieldType), vault.NewStrNode(typ)},
	}
	hasCreated := false
	for key, val := range vault.Pairs(fm) {
		switch key.Value {
		case vault.FieldType:
			if got := vault.Text(fm, vault.FieldType); got != typ {
				return nil, fmt.Errorf(
					"frontmatter type %q conflicts with type %q; drop it from frontmatter or make them match",
					got,
					typ,
				)
			}
			continue
		case vault.FieldCreated:
			hasCreated = true
		case vault.FieldTags:
			val = vault.NewTagsNode(vault.Values(fm, vault.FieldTags))
		}
		doc.Content = append(doc.Content, key, val)
	}

	if !hasCreated {
		doc.Content = append(doc.Content, vault.NewKeyNode(vault.FieldCreated), vault.NewStrNode(today))
	}
	return doc, nil
}
