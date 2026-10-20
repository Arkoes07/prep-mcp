package read

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/arkoes07/prep-mcp/internal/config"
	"github.com/arkoes07/prep-mcp/internal/vault"
)

const (
	ContextTagDays = 30
	ContextTopTags = 10
	rulesFile      = "CLAUDE.md"
)

// BuildContext builds the start-of-conversation bundle for day: the working rules,
// the week document for day's ISO week, the configured open-item and recent
// queries as metadata lines, and the most used recent tags. Sections are
// introduced by "==== name ====" lines. It is deterministic for a given day
// and vault content.
//
// Plain text, not JSON: it costs fewer tokens and reads well in a chat, and a
// "====" line cannot be confused with the week document's "##" headings.
// testdata/context.golden is the specification of the format.
func (r *Reader) BuildContext(day time.Time) (string, error) {
	docs, problems, err := r.Scan()
	if err != nil {
		return "", err
	}
	var sections []string

	rules, err := r.renderRules()
	if err != nil {
		return "", err
	}
	if rules != "" {
		sections = append(sections, rules)
	}

	if r.cfg.Context.CurrentWeekType != "" {
		sec, weekProblems := r.renderWeek(docs, day)
		sections = append(sections, sec)
		problems = append(problems, weekProblems...)
	}

	queries, err := r.renderQueries(docs)
	if err != nil {
		return "", err
	}
	sections = append(sections, queries...)

	// No vocabulary: it only marks unknown tags, which renderTopTags does not show,
	// and a tags file that cannot be read would fail the whole bundle.
	since := day.AddDate(0, 0, -ContextTagDays)
	sections = append(sections, "==== Tags ====\n"+renderTopTags(countTags(docs, nil, "", &since), since)+"\n")

	if len(problems) > 0 {
		sections = append(sections, renderProblems(problems))
	}
	return strings.Join(sections, "\n"), nil
}

// renderRules renders the working rules file, or "" when there is none.
func (r *Reader) renderRules() (string, error) {
	rules, err := r.v.Root().ReadFile(rulesFile)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", nil
	case err != nil:
		return "", fmt.Errorf("read %s: %w; check that it is a file you can read", rulesFile, err)
	}
	return renderSection(rulesFile, string(rules)), nil
}

// renderQueries renders the configured open-item and recent queries, one
// section per group that has any.
func (r *Reader) renderQueries(docs []*Doc) ([]string, error) {
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
			block, err := r.renderQuery(docs, q, group.isRecent)
			if err != nil {
				return nil, err
			}
			blocks = append(blocks, block)
		}
		sections = append(sections, "==== "+group.name+" ====\n"+strings.Join(blocks, "\n"))
	}
	return sections, nil
}

func renderProblems(problems []Problem) string {
	slices.SortStableFunc(problems, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
	var b strings.Builder
	b.WriteString("==== Problems ====\n")
	for _, p := range problems {
		fmt.Fprintf(&b, "- %s: %s\n", p.Path, collapseLines(p.Msg))
	}
	return b.String()
}

// renderSection renders "==== name ====" followed by content, always ending in a newline.
func renderSection(name, content string) string {
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return "==== " + name + " ====\n" + content
}

// renderWeek finds the week document for day by field value (not by file
// name, so week files a human names by hand are found too) and renders it in
// full, as scanned, or says how to create it.
func (r *Reader) renderWeek(docs []*Doc, day time.Time) (string, []Problem) {
	cs := r.cfg.Context
	week := FormatISOWeek(day)
	title := "Current week: " + week

	var found []*Doc
	for _, d := range docs {
		if d.Type == cs.CurrentWeekType && strings.EqualFold(vault.Text(d.Frontmatter, cs.CurrentWeekField), week) {
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
		return renderSection(title, missing), nil
	}

	var problems []Problem
	for _, extra := range found[1:] {
		problems = append(problems, Problem{
			Path: extra.Path,
			Msg: fmt.Sprintf(
				"has the same %s (%s) as %s, which is the one used; change or remove one of them",
				cs.CurrentWeekField,
				week,
				found[0].Path,
			),
		})
	}
	doc := found[0]
	body := fmt.Sprintf("path: %s\nhash: %s\n\n%s", doc.Path, vault.Hash(doc.src), doc.src)
	return renderSection(title, body), problems
}

func (r *Reader) renderQuery(docs []*Doc, q config.Query, isRecent bool) (string, error) {
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
		fields := r.cfg.ShowFields(d.Type)
		b.WriteString(renderMetaLine(d.BuildMeta(fields), fields))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// renderMetaLine renders "- path | title | attributes | tags". The attributes
// are the date, followed by date_source=mtime when it is the file's modification time,
// updated when the document sets it, then each of fields, in order, that the
// document sets.
func renderMetaLine(m Meta, fields []string) string {
	attrs := []string{"date=" + m.Date}
	if m.DateSource == "mtime" {
		attrs = append(attrs, "date_source="+m.DateSource)
	}
	if m.Updated != "" {
		attrs = append(attrs, "updated="+collapseLines(m.Updated))
	}
	for _, k := range fields {
		if val := m.Fields[k]; val != "" {
			attrs = append(attrs, k+"="+collapseLines(val))
		}
	}
	a, tags := strings.Join(attrs, " "), strings.Join(m.Tags, ", ")
	if tags == "" {
		tags = "-"
	}
	return fmt.Sprintf("- %s | %s | %s | %s", m.Path, collapseLines(m.Title), a, tags)
}

func renderTopTags(stats []TagCount, since time.Time) string {
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

// collapseLines collapses line breaks so a value cannot break the line-based format.
func collapseLines(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
