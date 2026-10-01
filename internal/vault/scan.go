package vault

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Doc is one typed document: a .md file whose frontmatter has a type.
type Doc struct {
	Path, Type, Title string
	Tags              []string
	Date              time.Time // the date field, else the file's modification time
	DateFromMtime     bool
	Fields            map[string]*yaml.Node
	Body              []byte
	BodyLine          int // 1-based file line where Body starts

	src       []byte // the whole file as scanned; Body is a slice of it
	titleLine int    // file line of the title key, 0 when there is no title field
}

// Problem is a file the vault could not fully use, with what to do about it.
type Problem struct{ Path, Msg string }

// Scan reads every typed document under the vault, sorted by path. It skips
// dot-folders and dot-files, the root templates/ folder, symlinks, and files
// without frontmatter or without a type. A file with broken frontmatter is
// skipped and reported as a Problem.
func (v *Vault) Scan() ([]*Doc, []Problem, error) {
	var docs []*Doc
	var problems []Problem

	err := fs.WalkDir(v.root.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == "." {
				return err
			}
			// One unreadable folder or file must not fail every query.
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
		if !isMarkdownFile(d) {
			return nil
		}
		src, err := v.root.ReadFile(p)
		if err != nil {
			problems = append(problems, Problem{
				Path: p,
				Msg:  fmt.Sprintf("cannot read: %v; check the file permissions", err),
			})
			return nil
		}
		doc, docProblems := parseDoc(p, d, src)
		problems = append(problems, docProblems...)
		if doc != nil {
			docs = append(docs, doc)
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("scan vault: %w", err)
	}
	slices.SortFunc(docs, func(a, b *Doc) int { return strings.Compare(a.Path, b.Path) })
	slices.SortFunc(problems, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
	return docs, problems, nil
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
// use: broken frontmatter (no Doc) or an unparseable date (a Doc dated by mtime).
func parseDoc(p string, d fs.DirEntry, src []byte) (*Doc, []Problem) {
	fm, err := SplitFrontmatter(src)
	if err != nil {
		return nil, []Problem{{Path: p, Msg: err.Error()}}
	}
	if fm == nil {
		return nil, nil
	}
	typ := FieldText(fm.Map, "type")
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
		Fields:        map[string]*yaml.Node{},
		Body:          src[fm.BodyOffset:],
		src:           src,
		BodyLine:      fm.BodyLine,
		Tags:          NormalizeTags(Field(fm.Map, "tags")),
		Date:          info.ModTime(),
		DateFromMtime: true,
	}
	for i := 0; i+1 < len(fm.Map.Content); i += 2 {
		doc.Fields[fm.Map.Content[i].Value] = fm.Map.Content[i+1]
	}
	if tn := Field(fm.Map, "title"); tn != nil {
		if t := strings.TrimSpace(scalar(tn)); t != "" {
			doc.Title = t
			doc.titleLine = tn.Line + 1
		}
	}
	dn := Field(fm.Map, "date")
	if dn == nil || strings.TrimSpace(scalar(dn)) == "" {
		return doc, nil
	}
	t, err := ParseDate(scalar(dn))
	if err != nil {
		return doc, []Problem{{
			Path: p,
			Msg: fmt.Sprintf("line %d: date %q is not a date; use YYYY-MM-DD (using the file's modification time meanwhile)",
				dn.Line+1, scalar(dn)),
		}}
	}
	doc.Date, doc.DateFromMtime = t, false
	return doc, nil
}

// scalar returns the text of a scalar node, or "" for anything else.
func scalar(n *yaml.Node) string {
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return ""
	}
	return n.Value
}

// Text returns the trimmed text of a scalar frontmatter field, or "" if the
// field is missing, null or not a scalar.
func (d *Doc) Text(key string) string {
	return strings.TrimSpace(scalar(d.Fields[key]))
}
