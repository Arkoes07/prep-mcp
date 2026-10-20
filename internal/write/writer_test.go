package write_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
	"github.com/arkoes07/prep-mcp/internal/write"
)

var ctx = context.Background()

// unknownTagErr is the error for adding issue/buries-headlin, one edit away
// from a tag in the example vocabulary.
const unknownTagErr = `unknown tag "issue/buries-headlin"; closest known tag: "issue/buries-headline". ` +
	"Use a known tag (list_tags shows them all), or add it to tags.md first"

func hasWarning(res *write.Result, fragment string) bool {
	for _, w := range res.Warnings {
		if strings.Contains(w, fragment) {
			return true
		}
	}
	return false
}

func TestWriterCreate(t *testing.T) {
	deleteTags := func(e *env) {
		if err := os.Remove(e.abs("tags.md")); err != nil {
			t.Fatal(err)
		}
	}
	const head = "---\ntype: learning\n"
	created := "created: " + today + "\n---\n"

	ok := []struct {
		name, typ, fm, body, slug string
		setup                     func(*env)
		wantPath, wantBytes       string
		newFolder                 bool // the target folder must not exist before the call
	}{
		{
			name: "happy", typ: "learning", fm: `{"title":"DDIA ch5","track":"ddia"}`, body: "x",
			wantPath: "learning/ddia-ch5.md", newFolder: true,
			wantBytes: "---\ntype: learning\ntitle: DDIA ch5\ntrack: ddia\ncreated: 2026-01-14\n---\nx\n",
		},
		{
			name: "slug_arg", typ: "learning", fm: `{"title":"DDIA ch5","track":"ddia"}`, body: "x", slug: "Custom!",
			wantPath: "learning/custom.md", wantBytes: head + "title: DDIA ch5\ntrack: ddia\n" + created + "x\n",
		},
		{
			name: "slug_folder", typ: "learning", fm: `{"title":"T","track":"k"}`, slug: "DDIA/Ch05 Replication",
			wantPath: "learning/ddia/ch05-replication.md", newFolder: true,
			wantBytes: head + "title: T\ntrack: k\n" + created,
		},
		{
			name: "title_slash_flattens", typ: "learning", fm: `{"title":"A/B","track":"k"}`,
			wantPath: "learning/a-b.md", wantBytes: head + "title: A/B\ntrack: k\n" + created,
		},
		{
			name: "key_order", typ: "learning", fm: `{"track":"ddia","title":"T"}`,
			wantPath: "learning/t.md", wantBytes: head + "track: ddia\ntitle: T\n" + created,
		},
		{
			name: "type_same", typ: "learning", fm: `{"type":"learning","title":"T","track":"k"}`,
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\n" + created,
		},
		{
			name: "created_kept", typ: "learning", fm: `{"title":"T","track":"k","created":"2020-01-01"}`,
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\ncreated: 2020-01-01\n---\n",
		},
		{
			name: "date_timestamp", typ: "learning", fm: `{"title":"T","track":"k","date":"2026-01-12T09:00"}`,
			setup: func(e *env) {
				c, err := config.Parse([]byte(`types: {dated: {folder: f, filename: "{date}-{slug}.md"}}`))
				if err != nil {
					panic(err)
				}
				spec := e.cfg.Types["learning"]
				spec.Filename = c.Types["dated"].Filename
				e.cfg.Types["learning"] = spec
			},
			wantPath:  "learning/2026-01-12-t.md",
			wantBytes: head + "title: T\ntrack: k\ndate: 2026-01-12T09:00\n" + created,
		},
		{
			name: "week_param", typ: "week", fm: `{"week":"2026-W04"}`,
			wantPath: "weeks/2026-w04.md", wantBytes: "---\ntype: week\nweek: 2026-W04\n" + created,
		},
		{
			name: "tags_normalised", typ: "learning", fm: `{"title":"T","track":"k","tags":"Q/Failure,  Meta/Plan"}`,
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\ntags: [q/failure, meta/plan]\n" + created,
		},
		{
			name: "known_tag", typ: "learning", fm: `{"title":"T","track":"k","tags":["issue/buries-headline"]}`,
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\ntags: [issue/buries-headline]\n" + created,
		},
		{
			name: "no_tags_file", typ: "learning", fm: `{"title":"T","track":"k","tags":["issue/anything"]}`, setup: deleteTags,
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\ntags: [issue/anything]\n" + created,
		},
		{
			name: "empty_body", typ: "learning", fm: `{"title":"T","track":"k"}`, body: "",
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\n" + created,
		},
		{
			name: "body_no_newline", typ: "learning", fm: `{"title":"T","track":"k"}`, body: "x",
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\n" + created + "x\n",
		},
	}
	for _, tc := range ok {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			if tc.setup != nil {
				tc.setup(e)
			}
			if tc.newFolder {
				// The example vault may hold an empty folder here (git does not track it).
				if err := os.Remove(e.abs(path.Dir(tc.wantPath))); err != nil && !os.IsNotExist(err) {
					t.Fatalf("test setup: %v", err)
				}
			}
			d := write.Draft{Type: tc.typ, Frontmatter: node(t, tc.fm), Body: tc.body, Slug: tc.slug}
			res, err := e.w.Apply(ctx, write.CreateOp(d))
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			if res.Path != tc.wantPath {
				t.Errorf("path = %q, want %q", res.Path, tc.wantPath)
			}
			if got := e.read(t, tc.wantPath); got != tc.wantBytes {
				t.Errorf("file:\n%q\nwant:\n%q", got, tc.wantBytes)
			}
			if want := vault.Hash([]byte(tc.wantBytes)); res.Hash != want {
				t.Errorf("hash = %s, want %s", res.Hash, want)
			}
			if len(res.Warnings) > 0 {
				t.Errorf("warnings = %q, want none", res.Warnings)
			}
			if left := leftovers(t, e.root); len(left) > 0 {
				t.Errorf("temp files left behind: %v", left)
			}
		})
	}

	fail := []struct {
		name, typ, fm, body, slug string
		setup                     func(*env)
		wantErr                   []string
		wantExists                string // the error must say this file exists
	}{
		{name: "type_conflict", typ: "learning", fm: `{"type":"week","title":"T","track":"k"}`, wantErr: []string{"type", "week", "learning"}},
		{name: "exists", typ: "improvement", fm: `{"status":"open","exit_criteria":"x"}`, slug: "buries-headline", wantExists: "improvements/buries-headline.md"},
		{name: "week_exists", typ: "week", fm: `{"week":"2026-W03"}`, wantExists: "weeks/2026-w03.md"},
		{
			name: "nested_exists", typ: "improvement", fm: `{"status":"open","exit_criteria":"x"}`,
			slug: "Improvements/Buries Headline", wantExists: "improvements/improvements/buries-headline.md",
			setup: func(e *env) {
				testvault.Write(t, e.root, "improvements/improvements/buries-headline.md", "x\n")
			},
		},
		{
			name: "slug_traversal", typ: "learning", fm: `{"title":"T","track":"k"}`, slug: "../../x",
			wantErr: []string{"pass folder/name"},
		},
		{
			name: "slug_backslash", typ: "learning", fm: `{"title":"T","track":"k"}`, slug: `..\\x`,
			wantErr: []string{"pass folder/name"},
		},
		{
			name: "slug_folder_other_pattern", typ: "week", fm: `{"week":"2026-W05"}`, slug: "a/b",
			wantErr: []string{`"{week}.md"`, "pass a plain name"},
		},
	}
	for _, tc := range fail {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			if tc.setup != nil {
				tc.setup(e)
			}
			before := snapshot(t, e.root)
			d := write.Draft{Type: tc.typ, Frontmatter: node(t, tc.fm), Body: tc.body, Slug: tc.slug}
			res, err := e.w.Apply(ctx, write.CreateOp(d))
			if err == nil {
				t.Fatalf("create succeeded (%+v), want an error", res)
			}
			mustContain(t, err, tc.wantErr...)
			if tc.wantExists != "" {
				want := fmt.Sprintf("file exists: %s (use append_section or update_frontmatter)", tc.wantExists)
				if err.Error() != want {
					t.Errorf("message = %q, want %q", err, want)
				}
			}
			sameSnapshot(t, before, snapshot(t, e.root))
		})
	}
}

// TestWriterCreateHostileSlug proves a slug with folders cannot leave the
// type folder: each slug either fails or lands under learning/.
func TestWriterCreateHostileSlug(t *testing.T) {
	slugs := []string{
		"../../etc/passwd",
		"a/../../b",
		"/etc/x",
		`..\..\x`,
		"a/%2e%2e/b",
		"a/…/b",
		"C:/x",
		"a/.git/config",
		"~/x",
	}
	for _, slug := range slugs {
		t.Run(slug, func(t *testing.T) {
			e := newEnv(t)
			d := write.Draft{Type: "learning", Frontmatter: node(t, `{"title":"T","track":"k"}`), Slug: slug}
			res, err := e.w.Apply(ctx, write.CreateOp(d))
			if err != nil {
				return
			}
			if !strings.HasPrefix(res.Path, "learning/") || strings.Contains(res.Path, "..") {
				t.Errorf("slug %q wrote %q, want a path under learning/", slug, res.Path)
			}
		})
	}
}

func TestWriterAppend(t *testing.T) {
	const week = "weeks/2026-w03.md"

	t.Run("existing_section", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, week)
		res, err := e.w.Apply(ctx, write.AppendOp(week, "Log", "- Tue: x"))
		if err != nil {
			t.Fatalf("AppendSection: %v", err)
		}
		got := e.read(t, week)
		if want := orig + "- Tue: x\n"; got != want {
			t.Errorf("file:\n%q\nwant:\n%q", got, want)
		}
		if n := countLines(got, "## Log"); n != 1 {
			t.Errorf("%d '## Log' headings, want exactly one", n)
		}
		fmBefore, _ := vault.SplitFrontmatter([]byte(orig))
		if n := len(orig) - len(fmBefore.Body()); got[:n] != orig[:n] {
			t.Errorf("frontmatter changed:\n%q\nwant:\n%q", got[:n], orig[:n])
		}
		if res.Path != week || res.Hash != vault.Hash([]byte(got)) {
			t.Errorf("result = %+v, want path %s and the hash of the new file", res, week)
		}
	})

	t.Run("new_section", func(t *testing.T) {
		e := newEnv(t)
		const p = "improvements/missing-tradeoff.md"
		orig := e.read(t, p)
		if _, err := e.w.Apply(ctx, write.AppendOp(p, "Countermeasure", "State the cost first.")); err != nil {
			t.Fatalf("AppendSection: %v", err)
		}
		if want := orig + "\n## Countermeasure\n\nState the cost first.\n"; e.read(t, p) != want {
			t.Errorf("file:\n%q\nwant:\n%q", e.read(t, p), want)
		}
	})

	t.Run("missing_file", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		_, err := e.w.Apply(ctx, write.AppendOp("weeks/none.md", "Log", "x"))
		mustContain(t, err, "not found", "create_doc")
		sameSnapshot(t, before, snapshot(t, e.root))
	})
}

func TestWriterUpdate(t *testing.T) {
	const p = "improvements/buries-headline.md"

	t.Run("set_status", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, p)
		res, err := e.w.Apply(ctx, write.PatchOp(p, write.Patch{Set: node(t, `{"status":"improving"}`)}))
		if err != nil {
			t.Fatalf("UpdateFrontmatter: %v", err)
		}
		// The only differences: the status line, and updated before the closing fence.
		want := strings.Replace(orig, "status: open\n", "status: improving\n", 1)
		want = strings.Replace(want, "tags: []\n---\n", "tags: []\nupdated: "+today+"\n---\n", 1)
		got := e.read(t, p)
		if got != want {
			t.Errorf("file:\n%q\nwant:\n%q", got, want)
		}
		if res.Hash != vault.Hash([]byte(got)) {
			t.Errorf("hash = %s, want the hash of the new file", res.Hash)
		}
	})

	t.Run("noop_writes_nothing", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, p)
		res, err := e.w.Apply(ctx, write.PatchOp(p, write.Patch{Unset: []string{"nope"}}))
		if err != nil {
			t.Fatalf("UpdateFrontmatter: %v", err)
		}
		if e.read(t, p) != orig {
			t.Error("file changed on a no-op")
		}
		fi, err := os.Stat(e.abs(p))
		if err != nil {
			t.Fatal(err)
		}
		if !fi.ModTime().Equal(testvault.Mtime) {
			t.Errorf("mtime = %v, want it untouched (%v)", fi.ModTime(), testvault.Mtime)
		}
		if res.Hash != vault.Hash([]byte(orig)) {
			t.Errorf("hash = %s, want the unchanged file's hash", res.Hash)
		}
	})
}

func TestWriterReplace(t *testing.T) {
	const p = "weeks/2026-w03.md"

	// readHash returns the current content and its hash, as read_doc would.
	readHash := func(t *testing.T, e *env) (string, string) {
		t.Helper()
		_, content, err := e.v.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(content), vault.Hash(content)
	}

	t.Run("happy", func(t *testing.T) {
		e := newEnv(t)
		orig, h := readHash(t, e)
		next := orig + "- Tue: x\n"
		res, err := e.w.Apply(ctx, write.ReplaceOp(p, next, h))
		if err != nil {
			t.Fatalf("Replace: %v", err)
		}
		if e.read(t, p) != next {
			t.Errorf("file:\n%q\nwant:\n%q", e.read(t, p), next)
		}
		if res.Hash != vault.Hash([]byte(next)) {
			t.Errorf("hash = %s, want sha256 of the new content", res.Hash)
		}
	})

	t.Run("conflict", func(t *testing.T) {
		e := newEnv(t)
		orig, h := readHash(t, e)
		human := orig + "- human edit\n"
		if err := os.WriteFile(e.abs(p), []byte(human), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := e.w.Apply(ctx, write.ReplaceOp(p, orig+"- ai edit\n", h))
		var ce *write.ConflictError
		if !errors.As(err, &ce) {
			t.Fatalf("error %T (%v), want *write.ConflictError", err, err)
		}
		if want := vault.Hash([]byte(human)); ce.Current != want {
			t.Errorf("ConflictError.Current = %s, want %s", ce.Current, want)
		}
		mustContain(t, err, ce.Current, "re-read with read_doc, merge, retry")
		if e.read(t, p) != human {
			t.Error("the human's version was overwritten")
		}
	})

	t.Run("bad_hash", func(t *testing.T) {
		e := newEnv(t)
		orig, _ := readHash(t, e)
		_, err := e.w.Apply(ctx, write.ReplaceOp(p, orig+"x\n", "abc"))
		var ce *write.ConflictError
		if !errors.As(err, &ce) {
			t.Fatalf("error %T (%v), want *write.ConflictError", err, err)
		}
		if e.read(t, p) != orig {
			t.Error("file changed despite the wrong hash")
		}
	})

	t.Run("empty_hash", func(t *testing.T) {
		e := newEnv(t)
		orig, _ := readHash(t, e)
		_, err := e.w.Apply(ctx, write.ReplaceOp(p, orig+"x\n", ""))
		mustContain(t, err, "expected_hash is required; get it from read_doc")
		if e.read(t, p) != orig {
			t.Error("file changed without a hash")
		}
	})

	// Every remaining row must leave the vault exactly as it was.
	rejected := []struct {
		name, path, content string
		wantErr             []string
	}{
		{name: "missing_file", path: "weeks/none.md", content: "---\ntype: week\nweek: 2026-W09\n---\n", wantErr: []string{"not found", "create_doc"}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			before := snapshot(t, e.root)
			hash := vault.Hash([]byte(before[tc.path]))
			_, err := e.w.Apply(ctx, write.ReplaceOp(tc.path, tc.content, hash))
			mustContain(t, err, tc.wantErr...)
			sameSnapshot(t, before, snapshot(t, e.root))
		})
	}
}

// A template is not a document: every write tool can change one, its empty
// fields draw no warnings, and update_frontmatter stamps no updated date for
// new documents to copy. Its frontmatter must still parse.
func TestWriterTemplate(t *testing.T) {
	const p = "templates/week.md"
	tests := []struct {
		name string
		op   func(t *testing.T, e *env) write.Op
		want string // in the file after the write
	}{
		{"append_section", func(*testing.T, *env) write.Op {
			return write.AppendOp(p, "Log", "- Mon:")
		}, "## Log\n\n- Mon:\n"},
		{"update_frontmatter", func(t *testing.T, _ *env) write.Op {
			return write.PatchOp(p, write.Patch{Set: node(t, `{"energy":"low"}`)})
		}, "energy: low\n"},
		{"edit_text", func(*testing.T, *env) write.Op {
			return write.EditOp(p, "Weekly review", "Retro")
		}, "| Retro |"},
		{"replace_doc", func(t *testing.T, e *env) write.Op {
			return write.ReplaceOp(p, "---\ntype: week\nweek:\n---\n", vault.Hash([]byte(e.read(t, p))))
		}, "week:\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			res, err := e.w.Apply(ctx, tc.op(t, e))
			if err != nil {
				t.Fatal(err)
			}
			wantNoWarnings(t, res)
			got := e.read(t, p)
			if !strings.Contains(got, tc.want) {
				t.Errorf("file lacks %q:\n%s", tc.want, got)
			}
			if strings.Contains(got, "updated:") {
				t.Errorf("template got an updated date:\n%s", got)
			}
		})
	}

	t.Run("breaks_frontmatter", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		_, err := e.w.Apply(ctx, write.EditOp(p, "focus: []", "focus: ["))
		mustContain(t, err, "this write breaks the frontmatter", "change new so the frontmatter stays valid")
		sameSnapshot(t, before, snapshot(t, e.root))
	})

	t.Run("missing", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		_, err := e.w.Apply(ctx, write.EditOp("templates/new.md", "x", "y"))
		mustContain(t, err, "not found: templates/new.md", "cannot create a template", "ask the human to add it")
		sameSnapshot(t, before, snapshot(t, e.root))
	})
}

func TestWriterEdit(t *testing.T) {
	const p = "weeks/2026-w03.md"

	t.Run("one_match", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, p)
		res, err := e.w.Apply(ctx, write.EditOp(p, "headline late", "headline first"))
		if err != nil {
			t.Fatalf("Edit: %v", err)
		}
		want := strings.Replace(orig, "headline late", "headline first", 1)
		if got := e.read(t, p); got != want {
			t.Errorf("file:\n%q\nwant:\n%q", got, want)
		}
		if res.Hash != vault.Hash([]byte(want)) {
			t.Errorf("hash = %s, want sha256 of the new content", res.Hash)
		}
	})

	t.Run("empty_new_deletes", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, p)
		if _, err := e.w.Apply(ctx, write.EditOp(p, "- Mon: deep dive, headline late\n", "")); err != nil {
			t.Fatalf("Edit: %v", err)
		}
		if got, want := e.read(t, p), strings.TrimSuffix(orig, "- Mon: deep dive, headline late\n"); got != want {
			t.Errorf("file:\n%q\nwant:\n%q", got, want)
		}
	})

	t.Run("frontmatter_text", func(t *testing.T) {
		e := newEnv(t)
		op := write.EditOp(p, "focus: [Headline first,", "focus: [Lead with impact,")
		if _, err := e.w.Apply(ctx, op); err != nil {
			t.Fatalf("Edit: %v", err)
		}
		if got := e.read(t, p); !strings.Contains(got, "focus: [Lead with impact, State the cost]\n") {
			t.Errorf("file:\n%s", got)
		}
	})

	t.Run("error_names_the_resolved_path", func(t *testing.T) {
		e := newEnv(t)
		_, err := e.w.Apply(ctx, write.EditOp("./weeks//2026-w03.md", "nope", "x"))
		mustContain(t, err, `"nope" not found in `+p+`;`)
	})

	// Every row must leave the vault exactly as it was.
	rejected := []struct {
		name, old, replacement string
		wantErr                []string
	}{
		{
			"not_found", "headline early", "x",
			[]string{`"headline early" not found in ` + p + `; read_doc it and copy the text exactly`},
		},
		{
			"several", "Mon", "x",
			[]string{`"Mon" appears 2 times in ` + p + `; include more surrounding text so it matches once`},
		},
		{"empty_old", "", "x", []string{"old is empty"}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			before := snapshot(t, e.root)
			_, err := e.w.Apply(ctx, write.EditOp(p, tc.old, tc.replacement))
			mustContain(t, err, tc.wantErr...)
			sameSnapshot(t, before, snapshot(t, e.root))
		})
	}
}
