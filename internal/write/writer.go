// Package write changes files in a vault: create, append, patch frontmatter,
// edit text, replace. Byte transforms are pure functions; Writer applies them safely.
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

// Writer applies safe changes to one vault. Writes are serialised, since tool
// calls may overlap; every write goes through the batch engine (batch.go, Apply
// and Batch), and every file is replaced atomically. A nil Git means no commits
// and no commit warnings, so tests do not depend on git state.
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

type Result struct {
	Path     string   `json:"path"`
	Hash     string   `json:"hash"`
	Warnings []string `json:"warnings"`
	Commit   string   `json:"commit,omitempty"`
}

// ConflictError means replace_doc saw a file that changed since it was read.
type ConflictError struct{ Path, Current string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s changed since you read it; its current hash is %s. Nothing was written: "+
		"re-read with read_doc, merge, retry with expected_hash set to the current hash", e.Path, e.Current)
}

type Draft struct {
	Type        string     // a configured type
	Frontmatter *yaml.Node // a mapping in the order to write it; nil for none
	Body        string
	Slug        string // fills {slug} in the filename; empty means the title field
}

// create builds a new document's path and content. It fails if the file
// exists on disk or was created earlier in the batch.
func (p *batchPlan) create(d Draft) (*file, []byte, []string, error) {
	w := p.w
	spec, ok := w.cfg.Types[d.Type]
	if !ok {
		return nil, nil, nil, errors.New(w.unknownType(d.Type))
	}
	day := w.now()
	doc, err := newFrontmatter(d.Type, d.Frontmatter, day.Format(time.DateOnly))
	if err != nil {
		return nil, nil, nil, err
	}
	body := d.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	data := []byte("---\n" + encode(doc) + "---\n" + body)
	// Before the file name, which may need a field the rules find missing: one
	// error then names every missing field.
	warnings, err := w.check(createRules, nil, data)
	if err != nil {
		return nil, nil, nil, err
	}

	name, err := RenderFilename(
		spec,
		doc,
		d.Slug,
		day,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	rel, err := w.v.Resolve(path.Join(spec.Folder, name), vault.AccessWrite)
	if err != nil {
		return nil, nil, nil, err
	}
	f, err := p.file(rel)
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

// patchDoc applies patch to a document's bytes. A patch that changes nothing
// returns old.
func (w *Writer) patchDoc(old []byte, patch Patch) ([]byte, []string, error) {
	var warnings []string
	oldTags := stateOf(old).tags
	for _, t := range vault.NormalizeTagList(patch.RemoveTags) {
		if !slices.Contains(oldTags, t) {
			warnings = append(warnings,
				fmt.Sprintf("tag %q is not on this document, so there was nothing to remove", t))
		}
	}
	next, _, err := ApplyPatch(old, patch, w.now().Format(time.DateOnly))
	if err != nil {
		return nil, nil, err
	}
	return next, warnings, nil
}

// replaceContent is content as f's new bytes, but only while f holds the file
// the caller read: expectedHash is of that file, so a path an earlier op in
// the batch changed cannot be replaced.
func replaceContent(f *file, content, expectedHash string) ([]byte, error) {
	expected := strings.ToLower(strings.TrimSpace(expectedHash))
	if expected == "" {
		return nil, errors.New("expected_hash is required; get it from read_doc")
	}
	if f.changed() {
		return nil, fmt.Errorf("an earlier op in this batch changed %s; "+
			"put replace_doc first for %s, or fold that change into its content", f.rel, f.rel)
	}
	if cur := vault.Hash(f.cur); cur != expected {
		return nil, &ConflictError{Path: f.rel, Current: cur}
	}
	return []byte(content), nil
}

// editText replaces the one occurrence of old in src, the file rel, with
// replacement. It needs no hash: if the text changed under the caller, old
// does not match.
func editText(src []byte, rel, old, replacement string) ([]byte, error) {
	if old == "" {
		return nil, errors.New("old is empty; pass the exact text to replace, copied from read_doc")
	}
	switch n := bytes.Count(src, []byte(old)); n {
	case 0:
		return nil, fmt.Errorf("%q not found in %s; read_doc it and copy the text exactly", old, rel)
	case 1:
	default:
		return nil, fmt.Errorf(
			"%q appears %d times in %s; include more surrounding text so it matches once",
			old,
			n,
			rel,
		)
	}
	return bytes.Replace(src, []byte(old), []byte(replacement), 1), nil
}

// commit commits paths with msg when commits are on and anything was written,
// and returns the commit's hash and any warning.
func (w *Writer) commit(ctx context.Context, paths []string, msg string) (string, []string) {
	if w.git == nil || !w.cfg.CommitOnWrite || len(paths) == 0 {
		return "", nil
	}
	hash, warning := w.git.Commit(
		ctx,
		w.v.Dir(),
		paths,
		msg,
	)
	if warning == "" {
		return hash, nil
	}
	return hash, []string{warning}
}
