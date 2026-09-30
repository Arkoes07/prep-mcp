package write_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/testvault"
	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

var ctx = context.Background()

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
		wantWarn                  string // a warning containing this; "" means no warnings at all
		newFolder                 bool   // the target folder must not exist before the call
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
			name: "slug_traversal", typ: "learning", fm: `{"title":"T","track":"k"}`, slug: "../../x",
			wantPath: "learning/x.md", wantBytes: head + "title: T\ntrack: k\n" + created,
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
			name: "week_param", typ: "week", fm: `{"week":"2026-W04"}`,
			wantPath: "weeks/2026-w04.md", wantBytes: "---\ntype: week\nweek: 2026-W04\n" + created,
		},
		{
			name: "tags_normalised", typ: "learning", fm: `{"title":"T","track":"k","tags":"Q/Failure,  Meta/Plan"}`,
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\ntags: [q/failure, meta/plan]\n" + created,
		},
		{
			name: "unknown_tag", typ: "learning", fm: `{"title":"T","track":"k","tags":["issue/buries-headlin"]}`,
			wantPath: "learning/t.md", wantBytes: head + "title: T\ntrack: k\ntags: [issue/buries-headlin]\n" + created,
			wantWarn: "issue/buries-headline",
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
			res, err := e.w.Create(ctx, tc.typ, node(t, tc.fm), tc.body, tc.slug)
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
			switch {
			case tc.wantWarn == "" && len(res.Warnings) > 0:
				t.Errorf("warnings = %q, want none", res.Warnings)
			case tc.wantWarn != "" && !hasWarning(res, tc.wantWarn):
				t.Errorf("warnings = %q, want one containing %q", res.Warnings, tc.wantWarn)
			}
			if left := leftovers(t, e.root); len(left) > 0 {
				t.Errorf("temp files left behind: %v", left)
			}
		})
	}

	fail := []struct {
		name, typ, fm, body, slug string
		wantErr                   []string
		listTypes                 bool   // the error must name every configured type
		wantExists                string // the error must be an *ExistsError for this path
	}{
		{name: "missing_required", typ: "learning", fm: `{"title":"T"}`, wantErr: []string{"missing required fields for learning: track"}},
		{name: "empty_required_string", typ: "learning", fm: `{"title":"T","track":""}`, wantErr: []string{"missing required fields for learning: track"}},
		{name: "empty_required_list", typ: "learning", fm: `{"title":"T","track":[]}`, wantErr: []string{"missing required fields for learning: track"}},
		{name: "unknown_type", typ: "nope", fm: `{"title":"T"}`, wantErr: []string{"nope"}, listTypes: true},
		{name: "type_conflict", typ: "learning", fm: `{"type":"week","title":"T","track":"k"}`, wantErr: []string{"type", "week", "learning"}},
		{name: "exists", typ: "improvement", fm: `{"status":"open","exit_criteria":"x"}`, slug: "buries-headline", wantExists: "improvements/buries-headline.md"},
		{name: "week_exists", typ: "week", fm: `{"week":"2026-W03"}`, wantExists: "weeks/2026-w03.md"},
	}
	for _, tc := range fail {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			before := snapshot(t, e.root)
			res, err := e.w.Create(ctx, tc.typ, node(t, tc.fm), tc.body, tc.slug)
			if err == nil {
				t.Fatalf("Create succeeded (%+v), want an error", res)
			}
			mustContain(t, err, tc.wantErr...)
			if tc.listTypes {
				mustContain(t, err, e.cfg.TypeNames()...)
			}
			if tc.wantExists != "" {
				var ee *write.ExistsError
				if !errors.As(err, &ee) {
					t.Fatalf("error %T (%v), want *write.ExistsError", err, err)
				}
				if ee.Path != tc.wantExists {
					t.Errorf("ExistsError.Path = %q, want %q", ee.Path, tc.wantExists)
				}
				want := fmt.Sprintf("file exists: %s (use append_section or update_frontmatter)", tc.wantExists)
				if err.Error() != want {
					t.Errorf("message = %q, want %q", err, want)
				}
			}
			sameSnapshot(t, before, snapshot(t, e.root))
		})
	}
}

func TestWriterAppend(t *testing.T) {
	const week = "weeks/2026-w03.md"

	t.Run("existing_section", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, week)
		res, err := e.w.AppendSection(ctx, week, "Log", "- Tue: x")
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
		if got[:fmBefore.BodyOffset] != orig[:fmBefore.BodyOffset] {
			t.Errorf("frontmatter changed:\n%q\nwant:\n%q", got[:fmBefore.BodyOffset], orig[:fmBefore.BodyOffset])
		}
		if res.Path != week || res.Hash != vault.Hash([]byte(got)) {
			t.Errorf("result = %+v, want path %s and the hash of the new file", res, week)
		}
	})

	t.Run("new_section", func(t *testing.T) {
		e := newEnv(t)
		const p = "improvements/missing-tradeoff.md"
		orig := e.read(t, p)
		if _, err := e.w.AppendSection(ctx, p, "Countermeasure", "State the cost first."); err != nil {
			t.Fatalf("AppendSection: %v", err)
		}
		if want := orig + "\n## Countermeasure\n\nState the cost first.\n"; e.read(t, p) != want {
			t.Errorf("file:\n%q\nwant:\n%q", e.read(t, p), want)
		}
	})

	t.Run("missing_file", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		_, err := e.w.AppendSection(ctx, "weeks/none.md", "Log", "x")
		mustContain(t, err, "not found", "create_doc")
		sameSnapshot(t, before, snapshot(t, e.root))
	})

	t.Run("templates_refused", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		_, err := e.w.AppendSection(ctx, "templates/week.md", "Log", "x")
		mustContain(t, err, "templates/ is read-only")
		sameSnapshot(t, before, snapshot(t, e.root))
	})
}

func TestWriterUpdate(t *testing.T) {
	const p = "improvements/buries-headline.md"

	t.Run("set_status", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, p)
		res, err := e.w.UpdateFrontmatter(ctx, p, write.Patch{Set: node(t, `{"status":"improving"}`)})
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

	t.Run("unset_required", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		_, err := e.w.UpdateFrontmatter(ctx, p, write.Patch{Unset: []string{"exit_criteria"}})
		mustContain(t, err, "exit_criteria is required for improvement")
		sameSnapshot(t, before, snapshot(t, e.root))
	})

	t.Run("unknown_tag_names_closest", func(t *testing.T) {
		e := newEnv(t)
		res, err := e.w.UpdateFrontmatter(ctx, p, write.Patch{AddTags: []string{"issue/buries-headlin"}})
		if err != nil {
			t.Fatalf("UpdateFrontmatter: %v", err)
		}
		if !hasWarning(res, "issue/buries-headline") {
			t.Errorf("warnings = %q, want one naming issue/buries-headline", res.Warnings)
		}
		if got := e.read(t, p); !strings.Contains(got, "tags: [issue/buries-headlin]\n") {
			t.Errorf("tag not added:\n%s", got)
		}
	})

	t.Run("noop_writes_nothing", func(t *testing.T) {
		e := newEnv(t)
		orig := e.read(t, p)
		res, err := e.w.UpdateFrontmatter(ctx, p, write.Patch{Unset: []string{"nope"}})
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
		_, content, hash, err := e.v.Read(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(content), hash
	}

	t.Run("happy", func(t *testing.T) {
		e := newEnv(t)
		orig, h := readHash(t, e)
		next := orig + "- Tue: x\n"
		res, err := e.w.Replace(ctx, p, next, h)
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
		_, err := e.w.Replace(ctx, p, orig+"- ai edit\n", h)
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
		_, err := e.w.Replace(ctx, p, orig+"x\n", "abc")
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
		_, err := e.w.Replace(ctx, p, orig+"x\n", "")
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
		{name: "invalid_yaml", path: p, content: "---\ntype: [x\n---\n", wantErr: []string{"frontmatter"}},
		{name: "drops_required", path: p, content: "---\ntype: week\n---\nbody\n", wantErr: []string{"missing required fields for week: week"}},
		{name: "templates_path", path: "templates/week.md", content: "---\ntype: week\nweek: 2026-W09\n---\n", wantErr: []string{"templates/ is read-only"}},
	}
	for _, tc := range rejected {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			before := snapshot(t, e.root)
			hash := vault.Hash([]byte(before[tc.path]))
			_, err := e.w.Replace(ctx, tc.path, tc.content, hash)
			mustContain(t, err, tc.wantErr...)
			sameSnapshot(t, before, snapshot(t, e.root))
		})
	}

	accepted := []struct {
		name, content, wantWarn string
	}{
		{name: "unknown_tag", content: "---\ntype: week\nweek: 2026-W03\ntags: [issue/buries-headlin]\n---\nbody\n", wantWarn: "issue/buries-headline"},
		{name: "drops_frontmatter", content: "# Just notes\n", wantWarn: "will no longer appear in queries"},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			_, h := readHash(t, e)
			res, err := e.w.Replace(ctx, p, tc.content, h)
			if err != nil {
				t.Fatalf("Replace: %v", err)
			}
			if !hasWarning(res, tc.wantWarn) {
				t.Errorf("warnings = %q, want one containing %q", res.Warnings, tc.wantWarn)
			}
			if e.read(t, p) != tc.content {
				t.Errorf("file:\n%q\nwant:\n%q", e.read(t, p), tc.content)
			}
		})
	}
}
