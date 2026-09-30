package write

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
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

// New builds a Writer. now is the clock for created/updated dates; nil means
// the system clock.
func New(v *vault.Vault, cfg *config.Config, g *Git, now func() time.Time) *Writer {
	if now == nil {
		now = time.Now
	}
	return &Writer{v: v, cfg: cfg, git: g, now: now}
}

// Result is what every write returns.
type Result struct {
	Path, Hash, Commit string
	Warnings           []string
}

// ExistsError means create_doc found the target file already there.
type ExistsError struct{ Path string }

func (e *ExistsError) Error() string {
	return fmt.Sprintf("file exists: %s (use append_section or update_frontmatter)", e.Path)
}

// ConflictError means replace_doc saw a file that changed since it was read.
type ConflictError struct{ Path, Current string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s changed since you read it; its current hash is %s. Nothing was written: re-read with read_doc, merge, retry with expected_hash set to the current hash", e.Path, e.Current)
}

// Create makes a new document of a configured type. The path comes from the
// type's folder and filename pattern.
func (w *Writer) Create(ctx context.Context, typ string, fm *yaml.Node, body, slug string) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	spec, ok := w.cfg.Type(typ)
	if !ok {
		return nil, w.unknownType(typ)
	}
	if fm != nil && fm.Kind != yaml.MappingNode {
		return nil, errors.New("frontmatter must be an object of key: value pairs")
	}
	day := w.now()

	// type first, then the caller's keys in their order, then created.
	doc := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map", Content: []*yaml.Node{keyNode("type"), strNode(typ)}}
	hasCreated := false
	if fm != nil {
		for i := 0; i+1 < len(fm.Content); i += 2 {
			k, v := fm.Content[i], fm.Content[i+1]
			switch k.Value {
			case "type":
				if v.Kind != yaml.ScalarNode || v.Value != typ {
					return nil, fmt.Errorf("frontmatter type %q conflicts with type %q; drop it from frontmatter or make them match", v.Value, typ)
				}
				continue
			case "created":
				hasCreated = true
			case "tags":
				v = tagsNode(vault.NormalizeTags(v))
			}
			doc.Content = append(doc.Content, k, v)
		}
	}
	if !hasCreated {
		doc.Content = append(doc.Content, keyNode("created"), strNode(day.Format("2006-01-02")))
	}
	if missing := missingFields(spec.Required, doc); len(missing) > 0 {
		return nil, fmt.Errorf("missing required fields for %s: %s; add them to frontmatter and retry", typ, strings.Join(missing, ", "))
	}

	name, err := RenderFilename(spec.Filename, doc, slug, day)
	if err != nil {
		return nil, err
	}
	rel, err := w.v.Resolve(path.Join(spec.Folder, name), vault.Write)
	if err != nil {
		return nil, err
	}

	if body != "" && !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	data := []byte("---\n" + encode(doc) + "---\n" + body)
	if re, err := vault.SplitFrontmatter(data); err != nil || re == nil {
		return nil, fmt.Errorf("internal error: the new frontmatter did not re-read (%v); nothing was written. Report this as a bug", err)
	}

	if err := w.createFile(rel, data); err != nil {
		return nil, err
	}
	res := &Result{Path: rel, Hash: vault.Hash(data), Warnings: w.tagWarnings(vault.NormalizeTags(mapGet(doc, "tags")))}
	w.commit(ctx, "create_doc", res)
	return res, nil
}

// AppendSection adds content to the end of a "## heading" section, or adds
// the section at the end of the file.
func (w *Writer) AppendSection(ctx context.Context, p, heading, content string) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	rel, err := w.v.Resolve(p, vault.Write)
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
	rel, err := w.v.Resolve(p, vault.Write)
	if err != nil {
		return nil, err
	}
	return w.modify(ctx, "update_frontmatter", rel, func(old []byte) ([]byte, []string, error) {
		var warnings []string
		oldTags := tagsOf(old)
		for _, t := range vault.NormalizeTagList(patch.RemoveTags) {
			if !slices.Contains(oldTags, t) {
				warnings = append(warnings, fmt.Sprintf("tag %q is not on this document, so there was nothing to remove", t))
			}
		}
		next, changed, err := ApplyPatch(old, patch, w.now().Format("2006-01-02"))
		if err != nil || !changed {
			return old, warnings, err
		}
		fm, err := vault.SplitFrontmatter(next)
		if err != nil || fm == nil {
			return nil, nil, fmt.Errorf("internal error: the patched file did not re-read (%v); nothing was written. Report this as a bug", err)
		}
		if spec, ok := w.cfg.Type(fieldText(fm.Map, "type")); ok {
			if missing := missingFields(spec.Required, fm.Map); len(missing) > 0 {
				parts := make([]string, len(missing))
				for i, f := range missing {
					parts[i] = fmt.Sprintf("%s is required for %s", f, fieldText(fm.Map, "type"))
				}
				return nil, nil, fmt.Errorf("%s; give the field a value with set instead of removing or emptying it", strings.Join(parts, "; "))
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
	rel, err := w.v.Resolve(p, vault.Write)
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
		typ = fieldText(fm.Map, "type")
	}
	if typ == "" {
		warnings = append(warnings, "the new content has no frontmatter with a type, so this document will no longer appear in queries (list_docs, search_text, get_context)")
	} else if spec, ok := w.cfg.Type(typ); ok {
		if missing := missingFields(spec.Required, fm.Map); len(missing) > 0 {
			return nil, fmt.Errorf("missing required fields for %s: %s; keep them in the frontmatter", typ, strings.Join(missing, ", "))
		}
	}
	warnings = append(warnings, w.tagWarnings(newTags(tagsOf(old), tagsOf(data)))...)

	stale, cur, err := w.replaceFile(rel, data, expected)
	if err != nil {
		return nil, err
	}
	if stale {
		return nil, &ConflictError{Path: rel, Current: cur}
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
func (w *Writer) modify(ctx context.Context, tool, rel string, transform func(old []byte) ([]byte, []string, error)) (*Result, error) {
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
		stale, _, err := w.replaceFile(rel, next, vault.Hash(old))
		if err != nil {
			return nil, err
		}
		if stale {
			continue
		}
		res := &Result{Path: rel, Hash: vault.Hash(next), Warnings: warnings}
		w.commit(ctx, tool, res)
		return res, nil
	}
	return nil, fmt.Errorf("%s changed again while it was being edited, twice in a row. Nothing was written: re-read it with read_doc and retry", rel)
}

func (w *Writer) readExisting(rel string) ([]byte, error) {
	b, err := w.v.Root().ReadFile(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("not found: %s (use create_doc to make a new document)", rel)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	return b, nil
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

func (w *Writer) unknownType(typ string) error {
	names := w.cfg.TypeNames()
	if len(names) == 0 {
		return fmt.Errorf("unknown type %q: this vault has no types configured; add a types: section to prep.config.yaml and restart the server", typ)
	}
	return fmt.Errorf("unknown type %q; configured types: %s", typ, strings.Join(names, ", "))
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
			out = append(out, fmt.Sprintf("unknown tag %q; closest known tag: %q (fix it with update_frontmatter add_tags/remove_tags)", t, closest))
		}
	}
	return out
}

func tagsOf(src []byte) []string {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil || fm == nil {
		return nil
	}
	return vault.NormalizeTags(mapGet(fm.Map, "tags"))
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
		if isMissing(mapGet(m, r)) {
			out = append(out, r)
		}
	}
	return out
}

// ---- atomic file writes ----

// beforeRename is a test seam: it runs after the temp file is written and
// before it is renamed into place. Production leaves it nil.
var beforeRename func(tmp string) error

// stage writes data to a new temp file next to rel and returns its name. The
// temp file gets the final mode, so nothing is ever group- or world-writable by accident.
func (w *Writer) stage(rel string, data []byte, mode fs.FileMode) (string, error) {
	root := w.v.Root()
	dir := path.Dir(rel)
	if dir != "." {
		if err := root.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("cannot create the folder for %s: %w; check permissions", rel, err)
		}
	}
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("cannot write %s: %w", rel, err)
	}
	tmp := path.Join(dir, ".prep-tmp-"+hex.EncodeToString(suffix[:]))
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("cannot write %s: %w; check the folder's permissions", rel, err)
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = root.Chmod(tmp, mode)
	}
	if werr != nil {
		root.Remove(tmp)
		return "", fmt.Errorf("cannot write %s: %w", rel, werr)
	}
	return tmp, nil
}

// finish runs the test seam, then renames tmp over rel. On any failure the
// temp file is removed.
func (w *Writer) finish(rel, tmp string, check func() (stale bool, cur string, err error)) (bool, string, error) {
	root := w.v.Root()
	if beforeRename != nil {
		if err := beforeRename(tmp); err != nil {
			root.Remove(tmp)
			return false, "", fmt.Errorf("cannot write %s: %w", rel, err)
		}
	}
	if stale, cur, err := check(); stale || err != nil {
		root.Remove(tmp)
		return stale, cur, err
	}
	if err := root.Rename(tmp, rel); err != nil {
		root.Remove(tmp)
		return false, "", fmt.Errorf("cannot write %s: %w", rel, err)
	}
	return false, "", nil
}

// replaceFile atomically replaces an existing file, provided it still has the
// content with hash oldHash. stale reports that it did not; cur is its new hash.
func (w *Writer) replaceFile(rel string, data []byte, oldHash string) (stale bool, cur string, err error) {
	mode := fs.FileMode(0o644)
	if fi, err := w.v.Root().Stat(rel); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := w.stage(rel, data, mode)
	if err != nil {
		return false, "", err
	}
	return w.finish(rel, tmp, func() (bool, string, error) {
		now, err := w.v.Root().ReadFile(rel)
		if err != nil {
			return false, "", fmt.Errorf("cannot write %s: it disappeared while it was being edited (%w); re-read it and retry", rel, err)
		}
		h := vault.Hash(now)
		return h != oldHash, h, nil
	})
}

// createFile writes a new file and refuses to replace an existing one.
func (w *Writer) createFile(rel string, data []byte) error {
	exists := func() (bool, error) {
		_, err := w.v.Root().Lstat(rel)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return err == nil, err
	}
	if ok, err := exists(); err != nil {
		return fmt.Errorf("cannot check %s: %w", rel, err)
	} else if ok {
		return &ExistsError{Path: rel}
	}
	tmp, err := w.stage(rel, data, 0o644)
	if err != nil {
		return err
	}
	var existsErr error
	_, _, err = w.finish(rel, tmp, func() (bool, string, error) {
		if ok, _ := exists(); ok {
			existsErr = &ExistsError{Path: rel}
		}
		return false, "", existsErr
	})
	if existsErr != nil {
		return existsErr
	}
	return err
}
