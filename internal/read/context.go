package read

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/config"
	"github.com/Arkoes07/prep-mcp/internal/vault"
)

const (
	// ContextTagDays is the window of the tag summary in Context.
	ContextTagDays = 30
	// ContextTopTags is how many tags the summary names.
	ContextTopTags = 10
	rulesFile      = "CLAUDE.md"
)

// Context builds the start-of-conversation bundle for day: the working rules,
// the week document for day's ISO week, the configured open-item and recent
// queries as metadata lines, and the most used recent tags. Sections are
// introduced by "==== name ====" lines. It is deterministic for a given day
// and vault content.
func (r *Reader) Context(day time.Time) (string, error) {
	docs, problems, err := r.Scan()
	if err != nil {
		return "", err
	}
	var sections []string

	rules, err := r.rulesSection()
	if err != nil {
		return "", err
	}
	if rules != "" {
		sections = append(sections, rules)
	}

	if r.cfg.Context.CurrentWeekType != "" {
		sec, weekProblems := r.weekSection(docs, day)
		sections = append(sections, sec)
		problems = append(problems, weekProblems...)
	}

	queries, err := r.querySections(docs)
	if err != nil {
		return "", err
	}
	sections = append(sections, queries...)

	voc, err := r.v.Vocabulary(r.cfg.TagsFile)
	if err != nil {
		return "", err
	}
	since := day.AddDate(0, 0, -ContextTagDays)
	sections = append(sections, "==== Tags ====\n"+tagLine(tagStats(docs, voc, "", &since), since)+"\n")

	problems = append(problems, r.gitLockProblems()...)
	if len(problems) > 0 {
		sections = append(sections, problemsSection(problems))
	}
	return strings.Join(sections, "\n"), nil
}

// gitLockMsg is the problem reported for a git lock file in the vault's repository.
const gitLockMsg = "git's lock file is there, so AI writes are saved but not committed. " +
	"Another git process may be running, or one crashed and left it behind. " +
	"Once no git process is running in the vault, delete it; prep-mcp never deletes it"

// gitLockProblems lists the git lock files in the vault's .git folder that
// would stop every AI commit. It only looks, and only when commits are on.
func (r *Reader) gitLockProblems() []Problem {
	if !r.cfg.CommitOnWrite {
		return nil
	}
	var problems []Problem
	for _, name := range []string{".git/HEAD.lock", ".git/index.lock"} {
		if _, err := os.Lstat(filepath.Join(r.v.Dir(), name)); err == nil {
			problems = append(problems, Problem{Path: name, Msg: gitLockMsg})
		}
	}
	return problems
}

// rulesSection renders the working rules file, or "" when there is none.
func (r *Reader) rulesSection() (string, error) {
	rules, err := r.v.Root().ReadFile(rulesFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w", rulesFile, err)
	}
	return section(rulesFile, string(rules)), nil
}

// querySections renders the configured open-item and recent queries, one
// section per group that has any.
func (r *Reader) querySections(docs []*Doc) ([]string, error) {
	var sections []string
	for _, group := range []struct {
		name     string
		qs       []config.Query
		isRecent bool
	}{
		{name: "Open items", qs: r.cfg.Context.OpenItems},
		{name: "Recent", qs: r.cfg.Context.Recent, isRecent: true},
	} {
		if len(group.qs) == 0 {
			continue
		}
		blocks := make([]string, 0, len(group.qs))
		for _, q := range group.qs {
			block, err := r.queryBlock(docs, q, group.isRecent)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
		sections = append(sections, "==== "+group.name+" ====\n"+strings.Join(blocks, "\n"))
	}
	return sections, nil
}

// problemsSection lists the files that could not be fully used, by path.
func problemsSection(problems []Problem) string {
	slices.SortStableFunc(problems, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
	var b strings.Builder
	b.WriteString("==== Problems ====\n")
	for _, p := range problems {
		fmt.Fprintf(&b, "- %s: %s\n", p.Path, oneLine(p.Msg))
	}
	return b.String()
}

// section renders "==== name ====" followed by content, always ending in a newline.
func section(name, content string) string {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return "==== " + name + " ====\n" + content
}

// weekSection finds the week document for day by field value (not by file
// name) and renders it in full, as scanned, or says how to create it.
func (r *Reader) weekSection(docs []*Doc, day time.Time) (string, []Problem) {
	cs := r.cfg.Context
	week := ISOWeek(day)
	title := "Current week: " + week

	var found []*Doc
	for _, d := range docs {
		if d.Type == cs.CurrentWeekType && strings.EqualFold(d.Text(cs.CurrentWeekField), week) {
			found = append(found, d)
		}
	}
	if len(found) == 0 {
		missing := fmt.Sprintf(
			"no week doc for %s (create_doc type=%s with %s: %s)\n",
			week,
			cs.CurrentWeekType,
			cs.CurrentWeekField,
			week,
		)
		return section(title, missing), nil
	}

	var problems []Problem
	for _, extra := range found[1:] {
		problems = append(problems, Problem{
			Path: extra.Path,
			Msg: fmt.Sprintf("has the same %s (%s) as %s, which is the one used; change or remove one of them",
				cs.CurrentWeekField, week, found[0].Path),
		})
	}
	doc := found[0]
	body := fmt.Sprintf("path: %s\nhash: %s\n\n%s", doc.Path, vault.Hash(doc.src), doc.src)
	return section(title, body), problems
}

// queryBlock renders one configured query: a label line and one line per document.
func (r *Reader) queryBlock(docs []*Doc, q config.Query, isRecent bool) (string, error) {
	f := Filter{
		Type:    q.Type,
		Tags:    q.Tags,
		AnyTags: q.AnyTags,
		Where:   q.Where,
		Limit:   q.Limit,
	}
	got, err := f.Apply(docs)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, k := range slices.Sorted(maps.Keys(q.Where)) {
		parts = append(parts, k+": "+strings.Join(q.Where[k], ", "))
	}
	if len(q.Tags) > 0 {
		parts = append(parts, "tags: "+strings.Join(q.Tags, ", "))
	}
	if len(q.AnyTags) > 0 {
		parts = append(parts, "any_tags: "+strings.Join(q.AnyTags, ", "))
	}
	if isRecent {
		parts = append(parts, fmt.Sprintf("newest %d", q.Limit))
	}
	label := q.Type
	if len(parts) > 0 {
		label += " (" + strings.Join(parts, "; ") + ")"
	}

	var b strings.Builder
	b.WriteString(label)
	b.WriteString(":\n")
	if len(got) == 0 {
		b.WriteString("(none)\n")
	}
	for _, d := range got {
		b.WriteString(metaLine(d, r.cfg.ShowFields(d.Type)))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// metaLine is "- path | title | attributes | tags". The attributes are the
// date, followed by date_source=mtime when it is the file's modification time,
// then each of fields that the document sets.
func metaLine(d *Doc, fields []string) string {
	attrs := []string{"date=" + d.Date.Format(time.DateOnly)}
	if d.DateFromMtime {
		attrs = append(attrs, "date_source="+d.DateSource())
	}
	for _, k := range fields {
		if val := d.Text(k); val != "" {
			attrs = append(attrs, k+"="+oneLine(val))
		}
	}
	a, tags := strings.Join(attrs, " "), strings.Join(d.Tags, ", ")
	if tags == "" {
		tags = "-"
	}
	return fmt.Sprintf("- %s | %s | %s | %s", d.Path, oneLine(d.Title), a, tags)
}

func tagLine(stats []TagCount, since time.Time) string {
	head := fmt.Sprintf("Top tags, last %d days (since %s): ", ContextTagDays, since.Format(time.DateOnly))
	if len(stats) == 0 {
		return head + "none"
	}
	stats = stats[:min(len(stats), ContextTopTags)]
	items := make([]string, len(stats))
	for i, s := range stats {
		items[i] = fmt.Sprintf("%s (%d)", s.Tag, s.Count)
	}
	return head + strings.Join(items, ", ")
}

// oneLine collapses line breaks so a value cannot break the line-based format.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
