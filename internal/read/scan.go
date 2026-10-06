package read

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Arkoes07/prep-mcp/internal/vault"
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
// anywhere, and templates/ at the root.
func skipDir(p, name string) bool {
	isRootTemplates := path.Dir(p) == "." && strings.EqualFold(name, "templates")
	return strings.HasPrefix(name, ".") || isRootTemplates
}

// isMarkdownFile reports whether d is a regular, visible .md file. Symlinks are
// never followed.
func isMarkdownFile(d fs.DirEntry) bool {
	name := d.Name()
	return d.Type().IsRegular() && !strings.HasPrefix(name, ".") && strings.EqualFold(path.Ext(name), ".md")
}

// parseDoc turns the file at p into a Doc. It returns a nil Doc for a file
// without frontmatter or without a type, and problems for what it could not
// use: broken frontmatter (no Doc) or an unparseable date (a Doc dated by
// mtime). The date comes from the type's date field.
func (r *Reader) parseDoc(p string, d fs.DirEntry, src []byte) (*Doc, []Problem) {
	fm, err := vault.SplitFrontmatter(src)
	if err != nil {
		return nil, []Problem{{Path: p, Msg: err.Error()}}
	}
	if fm == nil {
		return nil, nil
	}
	typ := vault.Text(fm.Map, "type")
	if typ == "" {
		return nil, nil
	}
	info, err := d.Info()
	if err != nil {
		return nil, []Problem{{
			Path: p,
			Msg:  fmt.Sprintf("cannot read the file's details: %v; check the permissions", err),
		}}
	}

	name := d.Name()
	doc := &Doc{
		Path:          p,
		Type:          typ,
		Title:         strings.TrimSuffix(name, path.Ext(name)),
		Frontmatter:   fm.Map,
		Body:          src[fm.BodyOffset:],
		src:           src,
		BodyLine:      fm.BodyLine,
		Tags:          vault.Values(fm.Map, "tags"),
		Date:          info.ModTime(),
		DateFromMtime: true,
	}
	if t := vault.Text(fm.Map, "title"); t != "" {
		doc.Title = t
		doc.titleLine = vault.Field(fm.Map, "title").Line + 1
	}
	field := r.cfg.DateField(typ)
	day := vault.Text(fm.Map, field)
	if day == "" {
		return doc, nil
	}
	t, err := ParseDate(day)
	if err != nil {
		return doc, []Problem{{
			Path: p,
			Msg: fmt.Sprintf(
				"line %d: %s %q is not a date; use YYYY-MM-DD (using the file's modification time meanwhile)",
				vault.Field(fm.Map, field).Line+1,
				field,
				day,
			),
		}}
	}
	doc.Date, doc.DateFromMtime = t, false
	return doc, nil
}

// DateSource says where Date came from: "field" for the type's date field,
// "mtime" for the file's modification time.
func (d *Doc) DateSource() string {
	if d.DateFromMtime {
		return "mtime"
	}
	return "field"
}
