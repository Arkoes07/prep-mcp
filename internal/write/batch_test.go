package write_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Arkoes07/prep-mcp/internal/vault"
	"github.com/Arkoes07/prep-mcp/internal/write"
)

const (
	headline = "improvements/buries-headline.md"
	tradeoff = "improvements/missing-tradeoff.md"
	week     = "weeks/2026-w03.md"
	newNote  = "learning/ddia-ch5.md"
)

func statusOp(t *testing.T, p, status string) write.Op {
	t.Helper()
	return write.PatchOp(p, write.Patch{Set: node(t, `{"status":"`+status+`"}`)})
}

func noteOp(t *testing.T, fm string) write.Op {
	t.Helper()
	return write.CreateOp(write.Draft{Type: "learning", Frontmatter: node(t, fm), Body: "x"})
}

// Every op is checked before anything is written. One bad op among good ones
// writes nothing, and every failure is reported, not just the first.
func TestBatchInvalidOpsWriteNothing(t *testing.T) {
	e := newEnv(t)
	before := snapshot(t, e.root)
	ops := []write.Op{
		statusOp(t, headline, "improving"),
		write.AppendOp("../x.md", "Log", "- x"),
		write.AppendOp(week, "Log", "- Tue: x"),
		write.AppendOp("templates/week.md", "Log", "- x"),
		noteOp(t, `{"title":"DDIA ch5","track":"ddia"}`),
		noteOp(t, `{"title":"No track"}`),
	}

	_, err := e.w.Batch(ctx, ops, "")

	mustContain(
		t,
		err,
		"nothing was written",
		"3 of 6 ops failed",
		`op 2 (append_section ../x.md): "../x.md" is outside the vault`,
		"op 4 (append_section templates/week.md): templates/ is read-only",
		"op 6 (create_doc learning): missing required fields for learning: track",
	)
	for _, good := range []string{"op 1 ", "op 3 ", "op 5 "} {
		if strings.Contains(err.Error(), good) {
			t.Errorf("error names a valid op (%q): %v", good, err)
		}
	}
	sameSnapshot(t, before, snapshot(t, e.root))
}

// An unknown tag fails its op, so the whole batch writes nothing.
func TestBatchUnknownTagRejectsAll(t *testing.T) {
	e := newEnv(t)
	before := snapshot(t, e.root)
	ops := []write.Op{
		statusOp(t, headline, "improving"),
		noteOp(t, `{"title":"DDIA ch5","track":"ddia","tags":["issue/buries-headlin"]}`),
	}

	_, err := e.w.Batch(ctx, ops, "")

	mustContain(t, err, "1 of 2 ops failed", "op 2 (create_doc learning): "+unknownTagErr)
	sameSnapshot(t, before, snapshot(t, e.root))
}

// A failed op leaves the plan as it was: the next op does not see its change.
func TestBatchFailedOpLeavesPlanAsItWas(t *testing.T) {
	e := newEnv(t)
	ops := []write.Op{
		write.EditOp(week, "tags: [meta/plan]", "tags: [meta/plan, issue/buries-headlin]"),
		write.EditOp(week, "issue/buries-headlin", "issue/buries-headline"),
	}

	_, err := e.w.Batch(ctx, ops, "")

	mustContain(
		t,
		err,
		"2 of 2 ops failed",
		"op 1 (edit_text "+week+"): "+unknownTagErr,
		`op 2 (edit_text `+week+`): "issue/buries-headlin" not found`,
	)
}

// An op the caller could not build (for example arguments that did not
// decode) is reported in the same list as the ops that failed their checks.
func TestBatchFailedOpReportedWithOthers(t *testing.T) {
	e := newEnv(t)
	before := snapshot(t, e.root)
	ops := []write.Op{
		write.FailedOp("update_frontmatter", errors.New("set must be an object")),
		write.AppendOp("nope.md", "Log", "- x"),
	}

	_, err := e.w.Batch(ctx, ops, "")

	mustContain(
		t,
		err,
		"2 of 2 ops failed",
		"op 1 (update_frontmatter): set must be an object",
		"op 2 (append_section nope.md): not found: nope.md",
	)
	sameSnapshot(t, before, snapshot(t, e.root))
}

// A stale hash on one replace_doc rejects the whole batch, with the current
// hash so the caller can re-read and retry.
func TestBatchStaleReplaceRejectsAll(t *testing.T) {
	e := newEnv(t)
	before := snapshot(t, e.root)
	current := vault.Hash([]byte(e.read(t, week)))
	ops := []write.Op{
		statusOp(t, headline, "improving"),
		write.ReplaceOp(week, "---\ntype: week\nweek: 2026-W03\n---\n", vault.Hash([]byte("an older version"))),
	}

	_, err := e.w.Batch(ctx, ops, "")

	mustContain(t, err, "nothing was written", "op 2 (replace_doc "+week+")", "its current hash is "+current)
	var ce *write.ConflictError
	if !errors.As(err, &ce) || ce.Current != current {
		t.Errorf("error %v does not carry a *ConflictError with the current hash", err)
	}
	sameSnapshot(t, before, snapshot(t, e.root))
}

// Ops run in order, and a later op sees what earlier ops did to the same path.
func TestBatchSamePathInOrder(t *testing.T) {
	t.Run("update_then_append", func(t *testing.T) {
		e := newEnv(t)
		ops := []write.Op{
			statusOp(t, headline, "improving"),
			write.AppendOp(headline, "Notes", "- tried headline first"),
		}

		res, err := e.w.Batch(ctx, ops, "")
		if err != nil {
			t.Fatal(err)
		}

		got := e.read(t, headline)
		wants := []string{"status: improving\n", "updated: " + today + "\n", "## Notes\n\n- tried headline first\n"}
		for _, want := range wants {
			if !strings.Contains(got, want) {
				t.Errorf("file lacks %q:\n%s", want, got)
			}
		}
		for i, r := range res.Results {
			if r.Path != headline || r.Hash != vault.Hash([]byte(got)) {
				t.Errorf("result %d = %s %s, want %s with the hash of the file on disk", i, r.Path, r.Hash, headline)
			}
		}
	})

	t.Run("create_then_append", func(t *testing.T) {
		e := newEnv(t)
		ops := []write.Op{
			noteOp(t, `{"title":"DDIA ch5","track":"ddia"}`),
			write.AppendOp(newNote, "Notes", "- y"),
		}

		if _, err := e.w.Batch(ctx, ops, ""); err != nil {
			t.Fatal(err)
		}

		if got := e.read(t, newNote); !strings.HasSuffix(got, "x\n\n## Notes\n\n- y\n") {
			t.Errorf("file:\n%s\nwant the created body, then the appended section", got)
		}
	})

	t.Run("append_then_edit", func(t *testing.T) {
		e := newEnv(t)
		ops := []write.Op{
			write.AppendOp(week, "Log", "- Tue: x"),
			write.EditOp(week, "- Tue: x", "- Tue: mock, headline first"),
		}

		if _, err := e.w.Batch(ctx, ops, ""); err != nil {
			t.Fatal(err)
		}

		if got := e.read(t, week); !strings.HasSuffix(got, "headline late\n- Tue: mock, headline first\n") {
			t.Errorf("file:\n%s\nwant the appended line, then edited", got)
		}
	})

	t.Run("replace_must_come_first", func(t *testing.T) {
		e := newEnv(t)
		before := snapshot(t, e.root)
		old := e.read(t, week)
		ops := []write.Op{
			write.AppendOp(week, "Log", "- Tue: x"),
			write.ReplaceOp(week, old+"more\n", vault.Hash([]byte(old))),
		}

		_, err := e.w.Batch(ctx, ops, "")

		mustContain(t, err, "op 2 (replace_doc "+week+")", "put replace_doc first for "+week)
		sameSnapshot(t, before, snapshot(t, e.root))
	})

	t.Run("create_twice", func(t *testing.T) {
		e := newEnv(t)
		ops := []write.Op{
			noteOp(t, `{"title":"DDIA ch5","track":"ddia"}`),
			noteOp(t, `{"title":"DDIA ch5","track":"other"}`),
		}

		_, err := e.w.Batch(ctx, ops, "")

		mustContain(t, err, "op 2 (create_doc learning): file exists: "+newNote)
		if e.exists(newNote) {
			t.Error("the first create was written although the batch failed")
		}
	})
}

func TestBatchLimits(t *testing.T) {
	e := newEnv(t)
	tooMany := make([]write.Op, write.MaxBatchOps+1)
	for i := range tooMany {
		tooMany[i] = write.AppendOp(week, "Log", fmt.Sprintf("- line %d", i))
	}

	_, err := e.w.Batch(ctx, nil, "")
	mustContain(t, err, "pass at least one op")

	_, err = e.w.Batch(ctx, tooMany, "")
	mustContain(t, err, "201 ops", "at most 200 per call")

	res, err := e.w.Batch(ctx, tooMany[:write.MaxBatchOps], "")
	if err != nil {
		t.Fatalf("a batch of %d ops: %v", write.MaxBatchOps, err)
	}
	if len(res.Results) != write.MaxBatchOps {
		t.Errorf("results = %d, want %d", len(res.Results), write.MaxBatchOps)
	}
	if got := e.read(t, week); !strings.Contains(got, "- line 199\n") {
		t.Errorf("the last op's line is missing:\n%s", got)
	}
}

// Each op gets its own result: the path, the hash on disk after the batch,
// and the warnings that op caused.
func TestBatchResults(t *testing.T) {
	e := newEnv(t)
	ops := []write.Op{
		statusOp(t, headline, "improving"),
		noteOp(t, `{"title":"DDIA ch5","track":"ddia","tags":["issue/missing-tradeoff"]}`),
		// Nothing to write, and a warning that the tag was not there.
		write.PatchOp(tradeoff, write.Patch{RemoveTags: []string{"meta/plan"}}),
	}

	res, err := e.w.Batch(ctx, ops, "")
	if err != nil {
		t.Fatal(err)
	}

	if len(res.Results) != len(ops) {
		t.Fatalf("results = %d, want %d", len(res.Results), len(ops))
	}
	for i, want := range []string{headline, newNote, tradeoff} {
		r := res.Results[i]
		if r.Path != want || r.Hash != vault.Hash([]byte(e.read(t, want))) {
			t.Errorf("result %d = %s %s, want %s with the hash of the file on disk", i, r.Path, r.Hash, want)
		}
	}
	quiet := len(res.Results[0].Warnings) == 0 && len(res.Results[1].Warnings) == 0
	if !quiet || !hasWarning(&res.Results[2], `tag "meta/plan" is not on this document`) {
		t.Errorf("warnings = %q / %q / %q, want only the third op's warning",
			res.Results[0].Warnings, res.Results[1].Warnings, res.Results[2].Warnings)
	}
	if res.Commit != "" || len(res.Warnings) != 0 {
		t.Errorf("commit = %q, warnings = %q; want neither without git", res.Commit, res.Warnings)
	}
}
