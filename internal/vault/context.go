package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/config"
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
func (v *Vault) Context(day time.Time) (string, error) {
	docs, problems, err := v.Scan()
	if err != nil {
		return "", err
	}
	var sections []string

	rules, err := v.root.ReadFile(rulesFile)
	switch {
	case err == nil:
		sections = append(sections, section(rulesFile, string(rules)))
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("read %s: %w", rulesFile, err)
	}

	if cs := v.cfg.Context; cs.CurrentWeekType != "" {
		sec, weekProblems := v.weekSection(docs, day)
		sections = append(sections, sec)
		problems = append(problems, weekProblems...)
	}

	for _, group := range []struct {
		name   string
		qs     []config.Query
		recent bool
	}{
		{name: "Open items", qs: v.cfg.Context.OpenItems},
		{name: "Recent", qs: v.cfg.Context.Recent, recent: true},
	} {
		if len(group.qs) == 0 {
			continue
		}
		blocks := make([]string, 0, len(group.qs))
		for _, q := range group.qs {
			block, err := queryBlock(docs, q, group.recent)
			if err != nil {
				return "", err
			}
			blocks = append(blocks, block)
		}
		sections = append(sections, "==== "+group.name+" ====\n"+strings.Join(blocks, "\n"))
	}

	voc, err := v.Vocabulary()
	if err != nil {
		return "", err
	}
	since := day.AddDate(0, 0, -ContextTagDays)
	sections = append(sections, "==== Tags ====\n"+tagLine(tagStats(docs, voc, "", &since), since)+"\n")

	if len(problems) > 0 {
		slices.SortStableFunc(problems, func(a, b Problem) int { return strings.Compare(a.Path, b.Path) })
		var b strings.Builder
		b.WriteString("==== Problems ====\n")
		for _, p := range problems {
			fmt.Fprintf(&b, "- %s: %s\n", p.Path, oneLine(p.Msg))
		}
		sections = append(sections, b.String())
	}
	return strings.Join(sections, "\n"), nil
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
func (v *Vault) weekSection(docs []*Doc, day time.Time) (string, []Problem) {
	cs := v.cfg.Context
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
	body := fmt.Sprintf("path: %s\nhash: %s\n\n%s", doc.Path, Hash(doc.src), doc.src)
	return section(title, body), problems
}

// queryBlock renders one configured query: a label line and one line per document.
func queryBlock(docs []*Doc, q config.Query, recent bool) (string, error) {
	got, err := Apply(docs, Filter{
		Type:    q.Type,
		Tags:    q.Tags,
		AnyTags: q.AnyTags,
		Where:   q.Where,
		Limit:   q.Limit,
	})
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
	if recent {
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
		b.WriteString(metaLine(d))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// metaLine is "- path | title | attributes | tags". The date is shown only
// when the document has a date field; file modification time is for ordering.
func metaLine(d *Doc) string {
	var attrs []string
	if !d.DateFromMtime {
		attrs = append(attrs, "date="+d.Date.Format(time.DateOnly))
	}
	for _, k := range []string{"kind", "stage", "status"} {
		if val := d.Text(k); val != "" {
			attrs = append(attrs, k+"="+oneLine(val))
		}
	}
	a, tags := strings.Join(attrs, " "), strings.Join(d.Tags, ", ")
	if a == "" {
		a = "-"
	}
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
