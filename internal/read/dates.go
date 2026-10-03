package read

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
// the calendar date as written, at local midnight; anything after the date
// (a time of day, a zone) is dropped unread, so the same vault gives the same
// answers in every timezone.
func ParseDate(s string) (time.Time, error) {
	d := strings.TrimSpace(s)
	if len(d) > 10 && (d[10] == 'T' || d[10] == ' ') {
		d = d[:10]
	}
	t, err := time.ParseInLocation(time.DateOnly, d, time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("%q is not a date; use YYYY-MM-DD", s)
	}
	return t, nil
}

// dayOf truncates t to local midnight of its calendar date.
func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
