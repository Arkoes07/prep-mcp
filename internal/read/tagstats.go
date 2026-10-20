package read

import (
	"cmp"
	"slices"
	"strings"
	"time"

	"github.com/arkoes07/prep-mcp/internal/vault"
)

type TagCount struct {
	Tag     string
	Count   int       // documents carrying the tag
	Last    time.Time // newest document date; zero when Count is 0
	Unknown bool      // not in the tags file (never set without one)
}

// CountTags counts tags over typed documents, most used first, then by name,
// followed by the vocabulary's unused tags with count 0. prefix limits both to
// tags starting with it.
func (r *Reader) CountTags(prefix string) ([]TagCount, error) {
	docs, _, err := r.Scan() // problems are reported by List and BuildContext
	if err != nil {
		return nil, err
	}
	voc, err := r.v.LoadVocabulary(r.cfg.TagsFile)
	if err != nil {
		return nil, err
	}
	stats := countTags(docs, voc, prefix, nil)
	used := make(map[string]bool, len(stats))
	for _, s := range stats {
		used[s.Tag] = true
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for _, tag := range voc.Tags() {
		if strings.HasPrefix(tag, prefix) && !used[tag] {
			stats = append(stats, TagCount{Tag: tag})
		}
	}
	return stats, nil
}

func countTags(docs []*Doc, voc *vault.Vocabulary, prefix string, since *time.Time) []TagCount {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	byTag := map[string]*TagCount{}
	for _, d := range docs {
		if since != nil && truncateToDay(d.Date).Before(truncateToDay(*since)) {
			continue
		}
		for _, tag := range d.Tags {
			if !strings.HasPrefix(tag, prefix) {
				continue
			}
			tc := byTag[tag]
			if tc == nil {
				tc = &TagCount{Tag: tag, Unknown: voc != nil && !voc.Known(tag)}
				byTag[tag] = tc
			}
			tc.Count++
			if d.Date.After(tc.Last) {
				tc.Last = d.Date
			}
		}
	}
	out := make([]TagCount, 0, len(byTag))
	for _, tc := range byTag {
		out = append(out, *tc)
	}
	slices.SortFunc(out, func(a, b TagCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), strings.Compare(a.Tag, b.Tag))
	})
	return out
}
