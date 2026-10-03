package write

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/Arkoes07/prep-mcp/internal/vault"
)

// Every write, single or batched, goes through one engine. All ops are
// checked first against the vault as the earlier ops leave it, without
// writing. Then each changed file is written once, with its final content,
// and if a write fails the files already written are put back. A file that
// changes under the engine between its check and its rename makes it put
// back what it wrote and start over once, so a human's save in Obsidian at that
// moment is kept.

// MaxBatchOps is the most ops one Batch takes.
const MaxBatchOps = 200

// The write tools an Op stands for, as named in commit messages, errors and
// the server's tool list.
const (
	ToolCreate  = "create_doc"
	ToolAppend  = "append_section"
	ToolPatch   = "update_frontmatter"
	ToolReplace = "replace_doc"
)

// Op is one write: the tool it stands for, what it targets (a path, or the
// type for create_doc) and how it changes a plan. Build it with CreateOp,
// AppendOp, PatchOp, ReplaceOp or FailedOp, and run it with Apply or Batch.
type Op struct {
	tool, target string
	apply        func(p *batchPlan) (*file, []string, error)
}

// CreateOp makes a new document of a configured type. The path comes from the
// type's folder and filename pattern.
func CreateOp(d Draft) Op {
	return Op{tool: ToolCreate, target: d.Type, apply: func(p *batchPlan) (*file, []string, error) {
		return p.create(d)
	}}
}

// AppendOp adds content to the end of a "## heading" section, or adds the
// section at the end of the file.
func AppendOp(path, heading, content string) Op {
	return Op{tool: ToolAppend, target: path, apply: func(p *batchPlan) (*file, []string, error) {
		return p.modify(path, func(old []byte) ([]byte, []string, error) {
			next, err := InsertSection(old, heading, content)
			return next, nil, err
		})
	}}
}

// PatchOp changes frontmatter keys and tags, leaving the body's bytes alone.
func PatchOp(path string, patch Patch) Op {
	return Op{tool: ToolPatch, target: path, apply: func(p *batchPlan) (*file, []string, error) {
		return p.modify(path, func(old []byte) ([]byte, []string, error) {
			return p.w.patchDoc(old, patch)
		})
	}}
}

// ReplaceOp rewrites a whole file, but only if it still has the content whose
// hash the caller read.
func ReplaceOp(path, content, expectedHash string) Op {
	return Op{tool: ToolReplace, target: path, apply: func(p *batchPlan) (*file, []string, error) {
		return p.replace(path, content, expectedHash)
	}}
}

// FailedOp stands for an op of tool the caller could not build, for example
// because its arguments did not decode. Apply and Batch report err as its failure.
func FailedOp(tool string, err error) Op {
	return Op{tool: tool, apply: func(*batchPlan) (*file, []string, error) { return nil, nil, err }}
}

// label names op number i (0-based) in an error: its position, tool and target.
func (o Op) label(i int) string {
	if o.target == "" {
		return fmt.Sprintf("op %d (%s)", i+1, o.tool)
	}
	return fmt.Sprintf(
		"op %d (%s %s)",
		i+1,
		o.tool,
		o.target,
	)
}

// BatchResult is what Batch returns: a Result per op, in op order, and the one
// commit with its warnings. A Result's Hash is its file's hash after the batch.
type BatchResult struct {
	Results  []Result `json:"results"`
	Commit   string   `json:"commit,omitempty"`
	Warnings []string `json:"warnings"`
}

// Batch applies ops in order, all or nothing, as one commit. If any op fails
// its checks nothing is written and every failure is reported. message is the
// commit message; empty means one counting the ops per tool.
func (w *Writer) Batch(ctx context.Context, ops []Op, message string) (*BatchResult, error) {
	switch {
	case len(ops) == 0:
		return nil, errors.New("the batch is empty; pass at least one op")
	case len(ops) > MaxBatchOps:
		return nil, fmt.Errorf("the batch has %d ops; at most %d per call. "+
			"Split it into smaller batches (each is one commit)", len(ops), MaxBatchOps)
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	p, failed, err := w.run(ops)
	if len(failed) > 0 {
		return nil, checksFailed(ops, failed)
	}
	if err != nil {
		return nil, err
	}
	msg := cmp.Or(strings.TrimSpace(message), batchMessage(ops))
	hash, warnings := w.commit(ctx, p.written, msg)
	return &BatchResult{Results: p.results, Commit: hash, Warnings: append([]string{}, warnings...)}, nil
}

// Apply runs one op and commits it as "<tool>: <path>". Its errors are the
// op's own, unlabelled.
func (w *Writer) Apply(ctx context.Context, op Op) (*Result, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	p, failed, err := w.run([]Op{op})
	if len(failed) > 0 {
		return nil, failed[0].err
	}
	if err != nil {
		return nil, err
	}
	res := p.results[0]
	hash, warnings := w.commit(ctx, p.written, op.tool+": "+res.Path)
	res.Commit = hash
	res.Warnings = append(res.Warnings, warnings...)
	return &res, nil
}

// opError is op number op (0-based) failing its checks.
type opError struct {
	op  int
	err error
}

// checksFailed reports every op that failed its checks, labelled.
func checksFailed(ops []Op, failed []opError) error {
	errs := make([]error, len(failed))
	for i, f := range failed {
		errs[i] = fmt.Errorf("%s: %w", ops[f.op].label(f.op), f.err)
	}
	return fmt.Errorf(
		"nothing was written; %d of %d ops failed (fix them and send the whole batch again):\n%w",
		len(failed),
		len(ops),
		errors.Join(errs...),
	)
}

// batchMessage counts the ops per tool, tools sorted by name, for example
// "batch: 2 create_doc, 9 update_frontmatter".
func batchMessage(ops []Op) string {
	counts := map[string]int{}
	for _, op := range ops {
		counts[op.tool]++
	}
	parts := make([]string, 0, len(counts))
	for _, tool := range slices.Sorted(maps.Keys(counts)) {
		parts = append(parts, fmt.Sprintf("%d %s", counts[tool], tool))
	}
	return "batch: " + strings.Join(parts, ", ")
}

// file is one path as a batch sees it: as read from disk, and as the ops so
// far leave it.
type file struct {
	rel             string
	orig, cur       []byte
	existed, exists bool
}

// changed reports whether the batch has something to write to f.
func (f *file) changed() bool {
	return f.exists != f.existed || !bytes.Equal(f.cur, f.orig)
}

// batchPlan is ops checked against w's vault without writing anything.
type batchPlan struct {
	w       *Writer
	files   []*file // in the order the ops first touched them
	byPath  map[string]*file
	results []Result
	failed  []opError
	written []string // set once the plan is written
}

// file returns rel as the batch sees it, reading it the first time.
func (p *batchPlan) file(rel string) (*file, error) {
	if f, ok := p.byPath[rel]; ok {
		return f, nil
	}
	b, err := p.w.v.Root().ReadFile(rel)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", rel, err)
	}
	f := &file{rel: rel, orig: b, cur: b, existed: err == nil, exists: err == nil}
	p.byPath[rel] = f
	p.files = append(p.files, f)
	return f, nil
}

// existing resolves path for writing and returns it as the batch sees it,
// which must exist.
func (p *batchPlan) existing(path string) (*file, error) {
	rel, err := p.w.v.Resolve(path, vault.AccessWrite)
	if err != nil {
		return nil, err
	}
	f, err := p.file(rel)
	if err != nil {
		return nil, err
	}
	if !f.exists {
		return nil, fmt.Errorf("not found: %s (use create_doc to make a new document)", rel)
	}
	return f, nil
}

// run plans ops and writes them. failed lists the ops that did not pass their
// checks; then nothing was written. err is a write that failed, after the files
// already written were put back.
func (w *Writer) run(ops []Op) (*batchPlan, []opError, error) {
	for attempt := 1; ; attempt++ {
		p := w.plan(ops)
		if len(p.failed) > 0 {
			return nil, p.failed, nil
		}
		done, err := w.write(p)
		if err == nil {
			return p, nil, nil
		}
		problems := w.rollback(done)
		var ce *ConflictError
		if !errors.As(err, &ce) || len(problems) > 0 {
			return nil, nil, writeFailed(err, done, problems)
		}
		if attempt == 2 {
			return nil, nil, fmt.Errorf("%s changed again while it was being edited, twice in a row. "+
				"Nothing was written: re-read it with read_doc and retry", ce.Path)
		}
	}
}

// plan checks every op in order against the vault as the earlier ops leave
// it. A failed op leaves the plan as it was.
func (w *Writer) plan(ops []Op) *batchPlan {
	p := &batchPlan{w: w, byPath: map[string]*file{}, results: make([]Result, len(ops))}
	for i, op := range ops {
		f, warnings, err := op.apply(p)
		if err != nil {
			p.failed = append(p.failed, opError{op: i, err: err})
			continue
		}
		p.results[i] = Result{Path: f.rel, Warnings: append([]string{}, warnings...)}
	}
	for i := range p.results {
		if f := p.byPath[p.results[i].Path]; f != nil {
			p.results[i].Hash = vault.Hash(f.cur)
		}
	}
	return p
}

// modify runs transform on the current content of an existing file.
func (p *batchPlan) modify(
	path string,
	transform func(old []byte) (next []byte, warnings []string, err error),
) (*file, []string, error) {
	f, err := p.existing(path)
	if err != nil {
		return nil, nil, err
	}
	next, warnings, err := transform(f.cur)
	if err != nil {
		return nil, nil, err
	}
	f.cur = next
	return f, warnings, nil
}

// write writes every file the plan changed, in order, and returns the ones it
// wrote before any failure. The rename-time checks refuse a file that changed
// since it was read, or appeared since the check that it did not exist.
func (w *Writer) write(p *batchPlan) ([]*file, error) {
	var done []*file
	for _, f := range p.files {
		if !f.changed() {
			continue
		}
		var err error
		if f.existed {
			err = w.replaceFile(f.rel, f.cur, vault.Hash(f.orig))
		} else {
			err = w.createFile(f.rel, f.cur)
		}
		if err != nil {
			return done, err
		}
		done = append(done, f)
		p.written = append(p.written, f.rel)
	}
	return done, nil
}

// rollback puts back the files a failed batch wrote, newest first, and
// returns a problem for each it could not.
func (w *Writer) rollback(done []*file) []error {
	var problems []error
	for _, f := range slices.Backward(done) {
		if err := w.putBack(f); err != nil {
			problems = append(problems, err)
		}
	}
	return problems
}

// putBack restores f's bytes from before the batch, or removes it if the
// batch created it, but only while it still holds what the batch wrote: a
// human's later save is never overwritten.
func (w *Writer) putBack(f *file) error {
	ours := vault.Hash(f.cur)
	if f.existed {
		err := w.replaceFile(f.rel, f.orig, ours)
		var ce *ConflictError
		if errors.As(err, &ce) {
			return fmt.Errorf("%s was changed after this batch wrote it, so it was left as it is now; "+
				"check it by hand", f.rel)
		}
		if err != nil {
			return fmt.Errorf("%s could not be put back (%w); check it by hand", f.rel, err)
		}
		return nil
	}
	current, err := w.v.Root().ReadFile(f.rel)
	if err != nil {
		return fmt.Errorf("%s could not be removed (%w); delete it by hand", f.rel, err)
	}
	if vault.Hash(current) != ours {
		return fmt.Errorf("%s was changed after this batch created it, so it was left as it is now; "+
			"check it by hand", f.rel)
	}
	if err := w.v.Root().Remove(f.rel); err != nil {
		return fmt.Errorf("%s could not be removed (%w); delete it by hand", f.rel, err)
	}
	return nil
}

// writeFailed explains a write that failed after done were written and then
// put back, except for problems.
func writeFailed(err error, done []*file, problems []error) error {
	switch {
	case len(done) == 0:
		return err
	case len(problems) == 0:
		return fmt.Errorf("%w; nothing was written: the %d files this batch had already written were put back",
			err, len(done))
	}
	return fmt.Errorf("%w; the files this batch had already written were put back, except:\n%w",
		err, errors.Join(problems...))
}
