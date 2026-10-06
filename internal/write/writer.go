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
		return nil, nil, nil, fmt.Errorf("unknown type %q; configured types: %s", d.Type, w.cfg.TypeList())
	}
	day := w.now()
	doc, err := newFrontmatter(d.Type, d.Frontmatter, day.Format(time.DateOnly))
	if err != nil {
		return nil, nil, nil, err
	}
	if missing := missingFields(spec.Required, doc); len(missing) > 0 {
		return nil, nil, nil, fmt.Errorf("missing required fields for %s: %s; add them to frontmatter and retry",
			d.Type, strings.Join(missing, ", "))
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

	body := d.Body
	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	data := []byte("---\n" + encode(doc) + "---\n" + body)
	if _, err := reread(data); err != nil {
		return nil, nil, nil, fmt.Errorf("internal error: the new frontmatter did not re-read (%w); "+
			"nothing was written. Report this as a bug", err)
	}

	f, err := p.file(rel)
	if err != nil {
		return nil, nil, nil, err
	}
	if f.exists {
		return nil, nil, nil, fmt.Errorf("file exists: %s (use append_section or update_frontmatter)", rel)
	}
	return f, data, nil, nil
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
	oldTags := tagsOf(old)
	for _, t := range vault.NormalizeTagList(patch.RemoveTags) {
		if !slices.Contains(oldTags, t) {
			warnings = append(warnings,
				fmt.Sprintf("tag %q is not on this document, so there was nothing to remove", t))
		}
	}
	next, fm, err := ApplyPatch(old, patch, w.now().Format(time.DateOnly))
	if err != nil || fm == nil {
		return old, warnings, err
	}
	// Checked on every write, not only create, so no write can leave a document
	// that create_doc would reject.
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
	return next, warnings, nil
}

// replace checks a full rewrite of docPath against the file as the batch sees
// it. expectedHash is of the file as the caller read it, so a path an earlier
// op in the batch changed cannot be replaced. A type that is not configured
// gets no required-field check, so a typo in type is accepted silently.
func (p *batchPlan) replace(docPath, content, expectedHash string) (*file, []byte, []string, error) {
	expected := strings.ToLower(strings.TrimSpace(expectedHash))
	if expected == "" {
		return nil, nil, nil, errors.New("expected_hash is required; get it from read_doc")
	}
	f, err := p.existing(docPath)
	if err != nil {
		return nil, nil, nil, err
	}
	if f.changed() {
		return nil, nil, nil, fmt.Errorf("an earlier op in this batch changed %s; "+
			"put replace_doc first for %s, or fold that change into its content", f.rel, f.rel)
	}
	if cur := vault.Hash(f.cur); cur != expected {
		return nil, nil, nil, &ConflictError{Path: f.rel, Current: cur}
	}

	data := []byte(content)
	warnings, err := p.w.checkRewrite(data)
	if err != nil {
		return nil, nil, nil, err
	}
	return f, data, warnings, nil
}

// editDoc replaces the one occurrence of old in the document at docPath,
// whose bytes are src, with replacement. It needs no hash: if the text changed
// under the caller, old does not match.
func (w *Writer) editDoc(src []byte, docPath, old, replacement string) ([]byte, []string, error) {
	if old == "" {
		return nil, nil, errors.New("old is empty; pass the exact text to replace, copied from read_doc")
	}
	switch n := bytes.Count(src, []byte(old)); n {
	case 0:
		return nil, nil, fmt.Errorf("%q not found in %s; read_doc it and copy the text exactly", old, docPath)
	case 1:
	default:
		return nil, nil, fmt.Errorf(
			"%q appears %d times in %s; include more surrounding text so it matches once",
			old,
			n,
			docPath,
		)
	}
	next := bytes.Replace(src, []byte(old), []byte(replacement), 1)
	before, srcErr := typeOf(src)
	after, err := typeOf(next)
	if err != nil {
		var fe *vault.FrontmatterError
		if srcErr == nil && errors.As(err, &fe) {
			return nil, nil, fmt.Errorf("this edit breaks the frontmatter: %s; "+
				"change new so the frontmatter stays valid", fe.Reason)
		}
		// It was broken before the edit, so the parser's advice to fix it stands.
		return nil, nil, err
	}
	// Unlike replace_doc, an edit is a small change: one that alters the type is
	// a mistake. Frontmatter that did not parse has no type to keep: the edit repairs it.
	if srcErr == nil && before != after {
		return nil, nil, fmt.Errorf("this edit changes the type %q to %q; keep the frontmatter's type line as it is",
			before, after)
	}
	warnings, err := w.checkRewrite(next)
	if err != nil {
		return nil, nil, err
	}
	return next, warnings, nil
}

// checkRewrite checks the new content of an existing file: valid frontmatter
// and the type's required fields. Content without a typed frontmatter passes
// with a warning.
func (w *Writer) checkRewrite(data []byte) ([]string, error) {
	fm, err := vault.SplitFrontmatter(data)
	if err != nil {
		return nil, err
	}
	var warnings []string
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
	return warnings, nil
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

// checkTags rejects each added tag that is not in the vocabulary, naming the
// closest known one. Only added tags: rejecting tags a human already had would
// block every write to that document. Without a vocabulary there is nothing to
// check against.
func (w *Writer) checkTags(added []string) error {
	if len(added) == 0 {
		return nil
	}
	voc, err := w.v.Vocabulary(w.cfg.TagsFile)
	if err != nil {
		return err
	}
	if len(voc.Tags()) == 0 {
		return nil
	}
	var errs []error
	for _, t := range added {
		if voc.Known(t) {
			continue
		}
		errs = append(errs, fmt.Errorf(
			"unknown tag %q; closest known tag: %q. Use a known tag (list_tags shows them all), or add it to %s first",
			t,
			voc.Closest(t),
			w.cfg.TagsFile,
		))
	}
	return errors.Join(errs...)
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

// typeOf returns the type in src's frontmatter, or "" if it has none.
func typeOf(src []byte) (string, error) {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil || fm == nil {
		return "", err
	}
	return vault.FieldText(fm.Map, "type"), nil
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
