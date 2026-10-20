package read

import (
	"cmp"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/arkoes07/prep-mcp/internal/document"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

// Doc is one typed document: a .md file whose frontmatter has a type.
type Doc struct {
	Path, Type, Title string
	Tags              []string
	Date              time.Time // the type's date field, else the file's modification time
	DateFromMtime     bool
	Frontmatter       *yaml.Node // the mapping; read its fields with vault.Values
	Body              []byte
	BodyLine          int // 1-based file line where Body starts

	src       []byte // the whole file as scanned; Body is a slice of it
	titleLine int    // file line of the title key, 0 when there is no title field
}

// Problem is a file the vault could not fully use, with what to do about it.
type Problem struct {
	Path string `json:"path"`
	Msg  string `json:"msg"`
}

// Scan reads every typed document under the vault, sorted by path. It skips
// dot-folders and dot-files, the root templates/ folder, symlinks, and files
// without frontmatter or without a type. A file with broken frontmatter is
// skipped and reported as a Problem, so a typo neither hides it nor fails every query.
//
// Every call scans; there is no index. The server writes, so an index built at
// startup is stale after the first write, and one stored on disk is a second
// copy of the frontmatter that can drift. Add an mtime-keyed cache (#2) only
// if BenchmarkScan or a real vault measures slow.
func (r *Reader) Scan() ([]*Doc, []Problem, error) {
	var docs []*Doc
	var problems []Problem

	walkProblems, err := r.walkMarkdown(func(p string, d fs.DirEntry) {
		src, err := r.v.Root().ReadFile(p)
		if err != nil {
			problems = append(problems, Problem{
				Path: p,
				Msg:  fmt.Sprintf("cannot read: %v; check the file permissions", err),
			})
			return
		}
		doc, docProblems := r.parseDoc(p, d, src)
		problems = append(problems, docProblems...)
		if doc != nil {
			docs = append(docs, doc)
		}
	})
	problems = append(problems, walkProblems...)
	if err != nil {
		return nil, nil, fmt.Errorf("scan vault: %w; check that the vault folder exists and is readable", err)
	}
	slices.SortFunc(docs, func(a, b *Doc) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(problems, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
	return docs, problems, nil
}

// walkMarkdown calls visit for every visible .md file under the vault, in
// lexical order. It skips dot-folders, dot-files, the root templates/ folder
// and symlinks. Unreadable entries come back as problems, so one bad folder
// does not fail the walk.
func (r *Reader) walkMarkdown(visit func(p string, d fs.DirEntry)) ([]Problem, error) {
	var problems []Problem
	err := fs.WalkDir(r.v.Root().FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			problems = append(problems, Problem{
				Path: p,
				Msg:  fmt.Sprintf("cannot read: %v; check the permissions", err),
			})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != "." && skipDir(p, d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if isMarkdownFile(d) {
			visit(p, d)
		}
		return nil
	})
	return problems, err
}

// skipDir reports whether the scan leaves out the folder at p: dot-folders
// anywhere, and templates/ at the root. A template's frontmatter is real
// enough to match queries: an open status would make it an open item, and an
// empty date dates it by its modification time, so it would list as recent.
func skipDir(p, name string) bool {
	return strings.HasPrefix(name, ".") || vault.InTemplates(p)
}

// isMarkdownFile reports whether d is a regular, visible .md file. Symlinks are
// never followed.
func isMarkdownFile(d fs.DirEntry) bool {
	name := d.Name()
	return d.Type().IsRegular() && !strings.HasPrefix(name, ".") && strings.EqualFold(path.Ext(name), ".md")
}

// parseDoc turns the file at p into a Doc. It returns a nil Doc for a file
// without frontmatter or without a type, and problems for what it could not
// use: broken frontmatter (no Doc) or a date field that holds no date (a Doc
// dated by mtime).
func (r *Reader) parseDoc(p string, d fs.DirEntry, src []byte) (*Doc, []Problem) {
	// No vocabulary: of the rules, only the date is a Problem.
	doc, err := document.Parse(src, r.cfg, nil)
	if err != nil {
		return nil, []Problem{{Path: p, Msg: err.Error()}}
	}
	if doc == nil || doc.Type == "" {
		return nil, nil
	}
	info, err := d.Info()
	if err != nil {
		return nil, []Problem{{
			Path: p,
			Msg:  fmt.Sprintf("cannot read the file's details: %v; check the permissions", err),
		}}
	}

	name, fm := d.Name(), doc.Frontmatter
	out := &Doc{
		Path:          p,
		Type:          doc.Type,
		Title:         cmp.Or(doc.Title, strings.TrimSuffix(name, path.Ext(name))),
		Frontmatter:   fm.Map,
		Body:          src[fm.BodyOffset:],
		src:           src,
		BodyLine:      fm.BodyLine,
		Tags:          doc.Tags,
		Date:          doc.Date,
		DateFromMtime: doc.Date.IsZero(),
	}
	if out.DateFromMtime {
		out.Date = info.ModTime()
	}
	if doc.Title != "" {
		out.titleLine = vault.Field(fm.Map, vault.FieldTitle).Line + 1
	}
	var problems []Problem
	for _, f := range doc.Findings {
		if f.Rule == document.RuleDate {
			problems = append(problems, Problem{
				Path: p,
				Msg: fmt.Sprintf(
					"line %d: %s; set it with update_frontmatter (using the file's modification time meanwhile)",
					vault.Field(fm.Map, doc.DateField).Line+1,
					f.Reason,
				),
			})
		}
	}
	return out, problems
}

// DateSource says where Date came from: "field" for the type's date field,
// "mtime" for the file's modification time.
func (d *Doc) DateSource() string {
	if d.DateFromMtime {
		return "mtime"
	}
	return "field"
}

// Meta is a document's metadata as a listing shows it. Its keys, but path and
// fields, are config.AlwaysShown, so no field to show can collide with one.
type Meta struct {
	Path       string            `json:"path"`
	Type       string            `json:"type"`
	Title      string            `json:"title"`
	Tags       []string          `json:"tags"`
	Date       string            `json:"date"`
	DateSource string            `json:"date_source"` // field (the type's date field) or mtime
	Updated    string            `json:"updated,omitempty"`
	Fields     map[string]string `json:"fields,omitempty"` // the fields to show that the document sets
}

// Meta is d's metadata, with each of fields that d sets.
func (d *Doc) Meta(fields []string) Meta {
	m := Meta{
		Path:       d.Path,
		Type:       d.Type,
		Title:      d.Title,
		Tags:       append([]string{}, d.Tags...),
		Date:       d.Date.Format(time.DateOnly),
		DateSource: d.DateSource(),
		Updated:    vault.Text(d.Frontmatter, vault.FieldUpdated),
		Fields:     map[string]string{},
	}
	for _, k := range fields {
		if v := d.Shown(k); v != "" {
			m.Fields[k] = v
		}
	}
	return m
}

// Shown is field as a listing shows it: its values that are not "", joined
// by ", ", or "" when there are none.
func (d *Doc) Shown(field string) string {
	return strings.Join(d.values(field), ", ")
}

// values returns the values of field that are not "".
func (d *Doc) values(field string) []string {
	return slices.DeleteFunc(vault.Values(d.Frontmatter, field), func(v string) bool { return v == "" })
}
