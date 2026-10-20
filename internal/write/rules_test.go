package write_test

import (
	"os"
	"strings"
	"testing"

	"github.com/arkoes07/prep-mcp/internal/testvault"
	"github.com/arkoes07/prep-mcp/internal/vault"
	"github.com/arkoes07/prep-mcp/internal/write"
)

// opFor builds an op once a test's files are in place, so replace_doc can
// take the hash of the file as it is then.
type opFor func(t *testing.T, e *env) write.Op

func replaceOf(path, content string) opFor {
	return func(t *testing.T, e *env) write.Op {
		return write.ReplaceOp(path, content, vault.Hash([]byte(e.read(t, path))))
	}
}

func editOf(path, old, replacement string) opFor {
	return func(*testing.T, *env) write.Op { return write.EditOp(path, old, replacement) }
}

func createOf(typ, fm string) opFor {
	return func(t *testing.T, _ *env) write.Op {
		return write.CreateOp(write.Draft{Type: typ, Frontmatter: node(t, fm), Slug: "x"})
	}
}

// patchOf sets the keys of set, a JSON object ("" for none), and unsets unset.
func patchOf(path, set string, unset ...string) opFor {
	return func(t *testing.T, _ *env) write.Op {
		p := write.Patch{Unset: unset}
		if set != "" {
			p.Set = node(t, set)
		}
		return write.PatchOp(path, p)
	}
}

func addTagsOf(path string, tags ...string) opFor {
	return func(*testing.T, *env) write.Op { return write.PatchOp(path, write.Patch{AddTags: tags}) }
}

func appendOf(path string) opFor {
	return func(*testing.T, *env) write.Op { return write.AppendOp(path, "Log", "- Tue") }
}

// TestDocumentRules holds every write tool to the document rules, alone and
// in a batch: an op fails on a rule it breaks, and a rule the file
// already broke is a warning.
func TestDocumentRules(t *testing.T) {
	// brokenWeek's frontmatter does not parse, and it has a tag the vocabulary lacks.
	const brokenWeek = "---\ntype: week\nweek: 2026-W03\ntags: [issue/buries-headlin]\nfocus: [x\n---\nbody\n"
	const repairedWeek = "---\ntype: week\nweek: 2026-W03\ntags: [issue/buries-headlin]\nfocus: [x]\n---\nbody\n"
	// lackingWeek already misses its required field, has a date field that
	// holds no date, and has a tag the vocabulary lacks.
	const lackingWeek = "---\ntype: week\ndate: next week\ntags: [issue/buries-headlin]\n---\n## Log\n- Mon\n"
	// oldType's type is not configured.
	const oldType = "---\ntype: old\n---\n## Log\n- Mon\n"
	const (
		unknownTypeErr = `unknown type "nope"; use one of the configured types: `
		removedTypeErr = `this write removes the type "week", but every document needs one: ` +
			"keep it, or set it to one of the configured types: "
		unknownTagWarn = `unknown tag "issue/buries-headlin"; closest known tag: "issue/buries-headline". ` +
			"Replace it with a known tag, or add it to tags.md"
		missingWeekWarn = "missing required fields for week: week; give them values with update_frontmatter"
		notADateErr     = `date "next week" is not a date; use YYYY-MM-DD`
		notADateWarn    = notADateErr + "; set it with update_frontmatter"
		oldTypeWarn     = `unknown type "old"; use one of the configured types: `
		brokenWarn      = "fix the frontmatter with edit_text"
	)

	tests := []struct {
		name      string
		files     map[string]string // written before the op
		op        opFor
		wantErr   []string // empty: the op succeeds
		wantWarn  []string // one fragment per warning, in order
		listTypes bool     // the error or the one warning names every configured type
	}{
		{
			name:      "type_unknown/create",
			op:        createOf("nope", `{"title":"T"}`),
			wantErr:   []string{unknownTypeErr},
			listTypes: true,
		},
		{
			name:      "type_unknown/patch",
			op:        patchOf(week, `{"type":"nope"}`),
			wantErr:   []string{unknownTypeErr},
			listTypes: true,
		},
		{
			name:      "type_unknown/replace",
			op:        replaceOf(week, "---\ntype: nope\nweek: 2026-W03\n---\nbody\n"),
			wantErr:   []string{unknownTypeErr},
			listTypes: true,
		},
		{
			name:    "type_unknown/edit",
			op:      editOf(week, "type: week", "type: nope"),
			wantErr: []string{`this edit changes the type "week" to "nope"; keep the frontmatter's type line`},
		},
		// A changed type is checked like a new document.
		{
			name:    "type_changed/patch",
			op:      patchOf(week, `{"type":"improvement"}`),
			wantErr: []string{"missing required fields for improvement: status, exit_criteria; give them values"},
		},
		{
			name:    "type_changed/edit",
			op:      editOf(week, "type: week", "type: learning"),
			wantErr: []string{`this edit changes the type "week" to "learning"`},
		},

		{
			name:      "type_removed/patch_unset",
			op:        patchOf(week, "", "type"),
			wantErr:   []string{removedTypeErr},
			listTypes: true,
		},
		{
			name:      "type_removed/patch_empty",
			op:        patchOf(week, `{"type":""}`),
			wantErr:   []string{removedTypeErr},
			listTypes: true,
		},
		{
			name:      "type_removed/replace",
			op:        replaceOf(week, "# Just notes\n"),
			wantErr:   []string{removedTypeErr},
			listTypes: true,
		},
		{
			name:    "type_removed/edit",
			op:      editOf(week, "---\ntype: week", "type: week"),
			wantErr: []string{`this edit changes the type "week" to ""`},
		},

		{
			name:    "required/create",
			op:      createOf("learning", `{"title":"T","track":[]}`),
			wantErr: []string{"missing required fields for learning: track; add them to frontmatter and retry"},
		},
		// The rules come before the file name, which needs the week field.
		{
			name:    "required/create_before_filename",
			op:      createOf("week", `{"week":""}`),
			wantErr: []string{"missing required fields for week: week; add them to frontmatter and retry"},
		},
		{
			name: "required/patch_empty",
			op:   patchOf(headline, `{"exit_criteria":""}`),
			wantErr: []string{
				"missing required fields for improvement: exit_criteria; " +
					"give them values with set instead of removing or emptying them",
			},
		},
		{
			name:    "required/patch_unset",
			op:      patchOf(headline, "", "exit_criteria"),
			wantErr: []string{"missing required fields for improvement: exit_criteria; give them values with set"},
		},
		{
			name:    "required/replace",
			op:      replaceOf(week, "---\ntype: week\nweek: []\n---\nbody\n"),
			wantErr: []string{"missing required fields for week: week; keep them in the frontmatter"},
		},
		{
			name:    "required/edit",
			op:      editOf(week, "week: 2026-W03\n", ""),
			wantErr: []string{"missing required fields for week: week; keep them in the frontmatter"},
		},

		{
			name:    "tag_added/create",
			op:      createOf("learning", `{"title":"T","track":"k","tags":["issue/buries-headlin"]}`),
			wantErr: []string{unknownTagErr},
		},
		{name: "tag_added/patch", op: addTagsOf(week, "issue/buries-headlin"), wantErr: []string{unknownTagErr}},
		{
			name:    "tag_added/replace",
			op:      replaceOf(week, "---\ntype: week\nweek: 2026-W03\ntags: [issue/buries-headlin]\n---\nbody\n"),
			wantErr: []string{unknownTagErr},
		},
		{
			name:    "tag_added/edit",
			op:      editOf(week, "tags: [meta/plan]", "tags: [meta/plan, issue/buries-headlin]"),
			wantErr: []string{unknownTagErr},
		},

		{
			name:    "date/create",
			op:      createOf("learning", `{"title":"T","track":"k","date":"next week"}`),
			wantErr: []string{notADateErr},
		},
		{name: "date/patch", op: patchOf(week, `{"date":"next week"}`), wantErr: []string{notADateErr}},
		{
			name:    "date/replace",
			op:      replaceOf(week, "---\ntype: week\nweek: 2026-W03\ndate: next week\n---\nbody\n"),
			wantErr: []string{notADateErr},
		},
		{
			name:    "date/edit",
			op:      editOf(week, "week: 2026-W03\n", "week: 2026-W03\ndate: next week\n"),
			wantErr: []string{notADateErr},
		},

		{
			name: "broken_by_op/replace",
			op:   replaceOf(week, "---\ntype: [x\n---\n"),
			wantErr: []string{
				"this write breaks the frontmatter: invalid frontmatter",
				"; change content so the frontmatter stays valid",
			},
		},
		{
			name: "broken_by_op/edit",
			op:   editOf(week, "focus: [Headline first, State the cost]", "focus: [x"),
			wantErr: []string{
				"this write breaks the frontmatter: invalid frontmatter",
				"; change new so the frontmatter stays valid",
			},
		},

		{
			name:     "already_broken/append",
			files:    map[string]string{week: lackingWeek},
			op:       appendOf(week),
			wantWarn: []string{missingWeekWarn, notADateWarn, unknownTagWarn},
		},
		{
			name:     "already_broken/patch",
			files:    map[string]string{week: lackingWeek},
			op:       patchOf(week, `{"start":"2026-01-12"}`),
			wantWarn: []string{missingWeekWarn, notADateWarn, unknownTagWarn},
		},
		{
			name:     "already_broken/replace",
			files:    map[string]string{week: lackingWeek},
			op:       replaceOf(week, lackingWeek+"- Tue\n"),
			wantWarn: []string{missingWeekWarn, notADateWarn, unknownTagWarn},
		},
		{
			name:     "already_broken/edit",
			files:    map[string]string{week: lackingWeek},
			op:       editOf(week, "- Mon", "- Tue"),
			wantWarn: []string{missingWeekWarn, notADateWarn, unknownTagWarn},
		},
		{
			name:  "already_broken/patch_cannot_empty_another",
			files: map[string]string{headline: "---\ntype: improvement\nstatus: open\n---\n"},
			op:    patchOf(headline, "", "status"),
			wantErr: []string{
				"missing required fields for improvement: status; give them values with set",
			},
		},
		{
			name:    "already_broken/patch_cannot_set_another_non_date",
			files:   map[string]string{week: lackingWeek},
			op:      patchOf(week, `{"date":"later"}`),
			wantErr: []string{`date "later" is not a date; use YYYY-MM-DD`},
		},
		{
			name:      "already_unknown_type/append",
			files:     map[string]string{week: oldType},
			op:        appendOf(week),
			wantWarn:  []string{oldTypeWarn},
			listTypes: true,
		},
		{
			name:      "already_unknown_type/patch",
			files:     map[string]string{week: oldType},
			op:        patchOf(week, `{"start":"2026-01-12"}`),
			wantWarn:  []string{oldTypeWarn},
			listTypes: true,
		},
		{
			name:      "already_unknown_type/replace",
			files:     map[string]string{week: oldType},
			op:        replaceOf(week, oldType+"- Tue\n"),
			wantWarn:  []string{oldTypeWarn},
			listTypes: true,
		},
		{
			name:      "already_unknown_type/edit",
			files:     map[string]string{week: oldType},
			op:        editOf(week, "- Mon", "- Tue"),
			wantWarn:  []string{oldTypeWarn},
			listTypes: true,
		},

		// A file that never had a type is a plain file: no rule to warn about.
		{
			name:  "plain_file/edit_tags_file",
			files: map[string]string{"tags.md": "Allowed: `meta/plan` `issue/buries-headline`\n"},
			op:    editOf("tags.md", "Allowed:", "Tags:"),
		},
		{name: "plain_file/replace", op: replaceOf("CLAUDE.md", "# Rules\n")},
		{name: "plain_file/append", op: appendOf("CLAUDE.md")},

		// Nothing to compare against: what the rules find is a warning.
		{
			name:     "broken_before/edit_repairs",
			files:    map[string]string{week: brokenWeek},
			op:       editOf(week, "focus: [x\n", "focus: [x]\n"),
			wantWarn: []string{unknownTagWarn},
		},
		{
			name:     "broken_before/replace_repairs",
			files:    map[string]string{week: brokenWeek},
			op:       replaceOf(week, repairedWeek),
			wantWarn: []string{unknownTagWarn},
		},
		{
			name:     "broken_before/replace_repairs_without_required",
			files:    map[string]string{week: brokenWeek},
			op:       replaceOf(week, "---\ntype: week\n---\nbody\n"),
			wantWarn: []string{missingWeekWarn},
		},
		{
			name:     "broken_before/edit_elsewhere",
			files:    map[string]string{week: brokenWeek},
			op:       editOf(week, "body", "more body"),
			wantWarn: []string{"invalid frontmatter near line 4: did not find expected ',' or ']'; " + brokenWarn},
		},
		{
			name:     "broken_before/append",
			files:    map[string]string{week: brokenWeek},
			op:       appendOf(week),
			wantWarn: []string{"invalid frontmatter near line 4: did not find expected ',' or ']'; " + brokenWarn},
		},
		{
			name:    "broken_before/patch_cannot_parse",
			files:   map[string]string{week: brokenWeek},
			op:      patchOf(week, `{"start":"2026-01-12"}`),
			wantErr: []string{"invalid frontmatter near line 4"},
		},
	}
	for _, tc := range tests {
		for _, batched := range []bool{false, true} {
			name := tc.name + "/apply"
			if batched {
				name = tc.name + "/batch"
			}
			t.Run(name, func(t *testing.T) {
				e := newEnv(t)
				for p, content := range tc.files {
					testvault.Write(t, e.root, p, content)
				}
				before := snapshot(t, e.root)
				warnings, err := runOne(t, e, tc.op(t, e), batched)

				if len(tc.wantErr) > 0 {
					if batched {
						mustContain(t, err, "1 of 1 ops failed", "op 1 (")
					}
					mustContain(t, err, tc.wantErr...)
					if tc.listTypes {
						mustContain(t, err, e.cfg.TypeNames()...)
					}
					sameSnapshot(t, before, snapshot(t, e.root))
					return
				}
				if err != nil {
					t.Fatalf("got error %v, want success", err)
				}
				if len(warnings) != len(tc.wantWarn) {
					t.Fatalf("warnings = %q, want %d containing %q", warnings, len(tc.wantWarn), tc.wantWarn)
				}
				for i, w := range tc.wantWarn {
					if !strings.Contains(warnings[i], w) {
						t.Errorf("warning %d = %q, want it to contain %q", i, warnings[i], w)
					}
				}
				if tc.listTypes {
					for _, typ := range e.cfg.TypeNames() {
						if !strings.Contains(warnings[0], typ) {
							t.Errorf("warning %q does not name type %s", warnings[0], typ)
						}
					}
				}
			})
		}
	}
}

// Tags that normalise to none have no value, so a type that requires tags
// misses them.
func TestRequiredTagsWithoutATag(t *testing.T) {
	e := newEnv(t)
	spec := e.cfg.Types["week"]
	spec.Required = append(spec.Required, "tags")
	e.cfg.Types["week"] = spec

	_, err := e.w.Apply(ctx, write.EditOp(week, "tags: [meta/plan]", `tags: " , "`))
	mustContain(t, err, "missing required fields for week: tags; keep them in the frontmatter")
}

// Only the type's date field must hold a date: date is just another field
// once the type names a different one, and created is never checked.
func TestTypeDateField(t *testing.T) {
	e := newEnv(t)
	spec := e.cfg.Types["week"]
	spec.DateField = "start"
	e.cfg.Types["week"] = spec

	_, err := e.w.Apply(ctx, write.PatchOp(week, write.Patch{Set: node(t, `{"start":"soon"}`)}))
	mustContain(t, err, `start "soon" is not a date; use YYYY-MM-DD`)

	res, err := e.w.Apply(ctx, write.PatchOp(week, write.Patch{Set: node(t, `{"date":"soon","created":"soon"}`)}))
	if err != nil {
		t.Fatalf("set date and created to non-dates: %v", err)
	}
	if len(res.Warnings) > 0 {
		t.Errorf("warnings = %q, want none", res.Warnings)
	}
}

// A type the config does not define is dated by date, as list_docs dates it,
// so a write may not leave a non-date there either.
func TestUnconfiguredTypeIsDatedByDate(t *testing.T) {
	e := newEnv(t)
	testvault.Write(t, e.root, week, "---\ntype: old\n---\n")

	_, err := e.w.Apply(ctx, write.PatchOp(week, write.Patch{Set: node(t, `{"date":"soon"}`)}))
	mustContain(t, err, `date "soon" is not a date; use YYYY-MM-DD`)
}

// An edit may not change the type, and what the new type would miss is
// named too, as for any write.
func TestEditThatChangesTheType(t *testing.T) {
	e := newEnv(t)
	_, err := e.w.Apply(ctx, write.EditOp(week, "type: week", "type: improvement"))
	mustContain(t, err, `this edit changes the type "week" to "improvement"`,
		"missing required fields for improvement: status, exit_criteria; keep them in the frontmatter")
	if strings.Contains(err.Error(), "unknown type") {
		t.Errorf("error %q also names the type rule", err)
	}
}

// A file without a type is a plain file, so no rule checks its tags.
func TestPlainFileTagsAreNotChecked(t *testing.T) {
	e := newEnv(t)
	testvault.Write(t, e.root, "notes/plain.md", "---\ntitle: T\n---\n")

	res, err := e.w.Apply(ctx, write.PatchOp("notes/plain.md", write.Patch{AddTags: []string{"issue/buries-headlin"}}))
	if err != nil {
		t.Fatalf("add an unknown tag to a plain file: %v", err)
	}
	if len(res.Warnings) > 0 {
		t.Errorf("warnings = %q, want none", res.Warnings)
	}
}

// A plain file broke no rule, so giving it a type checks it like a new
// document: a tag it already had is the write's doing.
func TestTypingAPlainFileChecksItsTags(t *testing.T) {
	e := newEnv(t)
	testvault.Write(t, e.root, "notes/plain.md", "---\ntags: [issue/buries-headlin]\n---\n")

	_, err := e.w.Apply(ctx, write.PatchOp("notes/plain.md", write.Patch{Set: node(t, `{"type":"week","week":"2026-W05"}`)}))
	mustContain(t, err, `unknown tag "issue/buries-headlin"`, "Use a known tag")
}

// runOne runs op through Apply, or through Batch as a batch of one, and
// returns the op's warnings.
func runOne(t *testing.T, e *env, op write.Op, batched bool) ([]string, error) {
	t.Helper()
	if !batched {
		res, err := e.w.Apply(ctx, op)
		if err != nil {
			return nil, err
		}
		return res.Warnings, nil
	}
	res, err := e.w.Batch(ctx, []write.Op{op}, "")
	if err != nil {
		return nil, err
	}
	return res.Results[0].Warnings, nil
}

// A tags file that cannot be read cannot vouch for an added tag, but it does
// not block a write that adds none.
func TestUnreadableVocabulary(t *testing.T) {
	e := newEnv(t)
	if err := os.Chmod(e.abs("tags.md"), 0); err != nil {
		t.Fatal(err)
	}

	res, err := e.w.Apply(ctx, write.AppendOp(week, "Log", "- Tue"))
	if err != nil {
		t.Fatalf("append to a tagged document: %v", err)
	}
	if !hasWarning(res, "read tags file tags.md") {
		t.Errorf("warnings = %q, want one saying the tags file could not be read", res.Warnings)
	}

	_, err = e.w.Apply(ctx, write.PatchOp(week, write.Patch{AddTags: []string{"meta/plan2"}}))
	mustContain(t, err, "read tags file tags.md", "check its permissions")
}
