package vault

import (
	"fmt"
	"strings"
	"time"
)

// ISOWeek formats t's ISO 8601 week as YYYY-Www (for example 2026-W03).
func ISOWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// ParseDate parses YYYY-MM-DD, or a timestamp starting with one. It returns
// the calendar date as written, at local midnight; any time of day is dropped.
func ParseDate(s string) (time.Time, error) {
	orig := s
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	bad := func() (time.Time, error) {
		return time.Time{}, fmt.Errorf("%q is not a date; use YYYY-MM-DD", orig)
	}
	if len(s) > 10 {
		if s[10] != 'T' && s[10] != ' ' {
			return bad()
		}
		// The whole value must be a valid timestamp, but only its date counts.
		ok := false
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", time.DateTime, "2006-01-02T15:04", "2006-01-02 15:04"} {
			if _, err := time.Parse(layout, s); err == nil {
				ok = true
				break
			}
		}
		if !ok {
			return bad()
		}
		s = s[:10]
	}
	t, err := time.ParseInLocation(time.DateOnly, s, time.Local)
	if err != nil {
		return bad()
	}
	return t, nil
}

// dayOf truncates t to local midnight of its calendar date.
func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
