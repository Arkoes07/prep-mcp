package vault

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// TagCount is one tag with its usage.
type TagCount struct {
	Tag     string
	Count   int       // documents carrying the tag
	Last    time.Time // newest document date
	Unknown bool      // not in the tags file (never set without one)
}

// TagStats counts tags over typed documents, most used first, then by name.
// prefix limits it to tags starting with it; since drops documents dated
// before that day.
func (v *Vault) TagStats(prefix string, since *time.Time) ([]TagCount, error) {
	docs, _, err := v.Scan() // problems are reported by List and Context
	if err != nil {
		return nil, err
	}
	voc, err := v.Vocabulary()
	if err != nil {
		return nil, err
	}
	return tagStats(docs, voc, prefix, since), nil
}

func tagStats(docs []*Doc, voc *Vocabulary, prefix string, since *time.Time) []TagCount {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	byTag := map[string]*TagCount{}
	for _, d := range docs {
		if since != nil && dayOf(d.Date).Before(dayOf(*since)) {
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
