// Package write changes files in a vault: create, append, patch frontmatter,
// replace. Byte transforms are pure functions; Writer applies them safely.
package write

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// Writer applies safe changes to one vault. Writes are serialised, and every
// file is replaced atomically. A nil Git means no commits and no commit warnings.
type Writer struct {
	mu  sync.Mutex
	v   *vault.Vault
	cfg *config.Config
	git *Git
	now func() time.Time
}

// New builds a Writer. now is the clock for created and updated dates.
func New(v *vault.Vault, cfg *config.Config, g *Git, now func() time.Time) *Writer {
	return &Writer{v: v, cfg: cfg, git: g, now: now}
}

// Result is what every write returns.
type Result struct {
	Path     string   `json:"path"`
	Hash     string   `json:"hash"`
	Warnings []string `json:"warnings"`
	Commit   string   `json:"commit,omitempty"`
}

// ExistsError means create_doc found the target file already there.
type ExistsError struct{ Path string }

var _ error = (*ExistsError)(nil)

// Error names the file and says which tool to use instead.
func (e *ExistsError) Error() string {
	return fmt.Sprintf("file exists: %s (use append_section or update_frontmatter)", e.Path)
}

// ConflictError means replace_doc saw a file that changed since it was read.
type ConflictError struct{ Path, Current string }

var _ error = (*ConflictError)(nil)

// Error gives the current hash and says how to retry.
func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s changed since you read it; its current hash is %s. Nothing was written: "+
		"re-read with read_doc, merge, retry with expected_hash set to the current hash", e.Path, e.Current)
}

// Draft is a document for Create to make.
type Draft struct {
	Type        string     // a configured type
	Frontmatter *yaml.Node // a mapping in the order to write it; nil for none
	Body        string
	Slug        string // fills {slug} in the filename; empty means the title field
}

// Create makes a new document of a configured type. The path comes from the
// type's folder and filename pattern.
func (w *Writer) Create(ctx context.Context, d Draft) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	spec, ok := w.cfg.Types[d.Type]
	if !ok {
		return nil, fmt.Errorf("unknown type %q; configured types: %s", d.Type, w.cfg.TypeList())
	}
	day := w.now()
	doc, err := newFrontmatter(d.Type, d.Frontmatter, day.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	if missing := missingFields(spec.Required, doc); len(missing) > 0 {
		return nil, fmt.Errorf("missing required fields for %s: %s; add them to frontmatter and retry",
			d.Type, strings.Join(missing, ", "))
	}

	name, err := RenderFilename(spec.Filename, doc, d.Slug, day)
	if err != nil {
		return nil, err
	}
	rel, err := w.v.Resolve(path.Join(spec.Folder, name), vault.AccessWrite)
	if err != nil {
		return nil, err
	}

	body := d.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	data := []byte("---\n" + encode(doc) + "---\n" + body)
	if _, err := reread(data); err != nil {
		return nil, fmt.Errorf("internal error: the new frontmatter did not re-read (%w); nothing was written. "+
			"Report this as a bug", err)
	}

	if err := w.createFile(rel, data); err != nil {
		return nil, err
	}
	res := &Result{
		Path:     rel,
		Hash:     vault.Hash(data),
		Warnings: w.tagWarnings(vault.NormalizeTags(vault.Field(doc, "tags"))),
	}
	w.commit(ctx, "create_doc", res)
	return res, nil
}

// newFrontmatter builds a new document's frontmatter: type first, then the
// caller's keys in their order with tags normalised, then created (today)
// unless the caller set it.
func newFrontmatter(typ string, fm *yaml.Node, today string) (*yaml.Node, error) {
	if fm != nil && fm.Kind != yaml.MappingNode {
		return nil, errors.New("frontmatter must be an object of key: value pairs")
	}
	doc := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{keyNode("type"), strNode(typ)}}
	hasCreated := false
	for key, val := range vault.Pairs(fm) {
		switch key.Value {
		case "type":
			if val.Kind != yaml.ScalarNode || val.Value != typ {
				return nil, fmt.Errorf("frontmatter type %q conflicts with type %q; "+
					"drop it from frontmatter or make them match", val.Value, typ)
			}
			continue
		case "created":
			hasCreated = true
		case "tags":
			val = tagsNode(vault.NormalizeTags(val))
		}
		doc.Content = append(doc.Content, key, val)
	}
	if !hasCreated {
		doc.Content = append(doc.Content, keyNode("created"), strNode(today))
	}
	return doc, nil
}

// AppendSection adds content to the end of a "## heading" section, or adds
// the section at the end of the file.
func (w *Writer) AppendSection(ctx context.Context, p, heading, content string) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rel, err := w.v.Resolve(p, vault.AccessWrite)
	if err != nil {
		return nil, err
	}
	return w.modify(ctx, "append_section", rel, func(old []byte) ([]byte, []string, error) {
		next, err := InsertSection(old, heading, content)
		return next, nil, err
	})
}

// UpdateFrontmatter changes frontmatter keys and tags, leaving the body's bytes alone.
func (w *Writer) UpdateFrontmatter(ctx context.Context, p string, patch Patch) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rel, err := w.v.Resolve(p, vault.AccessWrite)
	if err != nil {
		return nil, err
	}
	return w.modify(ctx, "update_frontmatter", rel, func(old []byte) ([]byte, []string, error) {
		var warnings []string
		oldTags := tagsOf(old)
		for _, t := range vault.NormalizeTagList(patch.RemoveTags) {
			if !slices.Contains(oldTags, t) {
				warnings = append(warnings,
					fmt.Sprintf("tag %q is not on this document, so there was nothing to remove", t))
			}
		}
		next, changed, err := ApplyPatch(old, patch, w.now().Format(time.DateOnly))
		if err != nil || !changed {
			return old, warnings, err
		}
		fm, err := reread(next)
		if err != nil {
			return nil, nil, fmt.Errorf("internal error: the patched file did not re-read (%w); nothing was written. "+
				"Report this as a bug", err)
		}
		if spec, ok := w.cfg.Types[vault.FieldText(fm.Map, "type")]; ok {
			if missing := missingFields(spec.Required, fm.Map); len(missing) > 0 {
				parts := make([]string, len(missing))
				for i, f := range missing {
					parts[i] = fmt.Sprintf("%s is required for %s", f, vault.FieldText(fm.Map, "type"))
				}
				return nil, nil, fmt.Errorf("%s; give the field a value with set instead of removing or emptying it",
					strings.Join(parts, "; "))
			}
		}
		return next, append(warnings, w.tagWarnings(newTags(oldTags, tagsOf(next)))...), nil
	})
}

// Replace rewrites a whole file, but only if it still has the content whose
// hash the caller read.
func (w *Writer) Replace(ctx context.Context, p, content, expectedHash string) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	expected := strings.ToLower(strings.TrimSpace(expectedHash))
	if expected == "" {
		return nil, errors.New("expected_hash is required; get it from read_doc")
	}
	rel, err := w.v.Resolve(p, vault.AccessWrite)
	if err != nil {
		return nil, err
	}
	old, err := w.readExisting(rel)
	if err != nil {
		return nil, err
	}
	if cur := vault.Hash(old); cur != expected {
		return nil, &ConflictError{Path: rel, Current: cur}
	}

	data := []byte(content)
	var warnings []string
	fm, err := vault.SplitFrontmatter(data)
	if err != nil {
		return nil, err
	}
	typ := ""
	if fm != nil {
		typ = vault.FieldText(fm.Map, "type")
	}
	if typ == "" {
		warnings = append(warnings, "the new content has no frontmatter with a type, "+
			"so this document will no longer appear in queries (list_docs, search_text, get_context)")
	} else if spec, ok := w.cfg.Types[typ]; ok {
		if missing := missingFields(spec.Required, fm.Map); len(missing) > 0 {
			return nil, fmt.Errorf("missing required fields for %s: %s; keep them in the frontmatter",
				typ, strings.Join(missing, ", "))
		}
	}
	warnings = append(warnings, w.tagWarnings(newTags(tagsOf(old), tagsOf(data)))...)

	if err := w.replaceFile(rel, data, expected); err != nil {
		return nil, err
	}
	res := &Result{Path: rel, Hash: vault.Hash(data), Warnings: warnings}
	w.commit(ctx, "replace_doc", res)
	return res, nil
}

// ---- shared steps ----

// modify reads rel, transforms it and replaces it atomically. If the file
// changes between the read and the rename it starts over once from the new
// content, so a human's save is not lost. A transform returning its input
// unchanged means there is nothing to write.
func (w *Writer) modify(
	ctx context.Context,
	tool, rel string,
	transform func(old []byte) (next []byte, warnings []string, err error),
) (*Result, error) {
	for range 2 {
		old, err := w.readExisting(rel)
		if err != nil {
			return nil, err
		}
		next, warnings, err := transform(old)
		if err != nil {
			return nil, err
		}
		if bytes.Equal(next, old) {
			return &Result{Path: rel, Hash: vault.Hash(old), Warnings: warnings}, nil
		}
		var conflict *ConflictError
		if err := w.replaceFile(rel, next, vault.Hash(old)); errors.As(err, &conflict) {
			continue
		} else if err != nil {
			return nil, err
		}
		res := &Result{Path: rel, Hash: vault.Hash(next), Warnings: warnings}
		w.commit(ctx, tool, res)
		return res, nil
	}
	return nil, fmt.Errorf("%s changed again while it was being edited, twice in a row. Nothing was written: "+
		"re-read it with read_doc and retry", rel)
}

func (w *Writer) commit(ctx context.Context, tool string, res *Result) {
	if w.git == nil || !w.cfg.CommitOnWrite {
		return
	}
	hash, warning := w.git.Commit(ctx, w.v.Dir(), res.Path, tool+": "+res.Path)
	res.Commit = hash
	if warning != "" {
		res.Warnings = append(res.Warnings, warning)
	}
}

// tagWarnings names the closest known tag for each added tag that is not in
// the vocabulary. Without a vocabulary there is nothing to check against.
func (w *Writer) tagWarnings(added []string) []string {
	voc, err := w.v.Vocabulary()
	if err != nil {
		return []string{fmt.Sprintf("could not read the tag vocabulary, so tags were not checked: %v", err)}
	}
	var out []string
	for _, t := range added {
		if voc.Known(t) {
			continue
		}
		if closest := voc.Closest(t); closest != "" {
			out = append(out, fmt.Sprintf(
				"unknown tag %q; closest known tag: %q (fix it with update_frontmatter add_tags/remove_tags)",
				t, closest))
		}
	}
	return out
}

// reread parses the frontmatter of a file this package built, which must have one.
func reread(src []byte) (*vault.Frontmatter, error) {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil {
		return nil, err
	}
	if fm == nil {
		return nil, errors.New("no frontmatter found")
	}
	return fm, nil
}

func tagsOf(src []byte) []string {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil || fm == nil {
		return nil
	}
	return vault.NormalizeTags(vault.Field(fm.Map, "tags"))
}

// newTags returns the tags in now that were not in before.
func newTags(before, now []string) []string {
	var out []string
	for _, t := range now {
		if !slices.Contains(before, t) {
			out = append(out, t)
		}
	}
	return out
}

func missingFields(required []string, m *yaml.Node) []string {
	var out []string
	for _, r := range required {
		if isMissing(vault.Field(m, r)) {
			out = append(out, r)
		}
	}
	return out
}
