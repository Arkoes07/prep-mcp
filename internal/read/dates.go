package read

import (
	"fmt"
	"time"
)

// ISOWeek formats t's ISO 8601 week as YYYY-Www (for example 2026-W03).
func ISOWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// dayOf truncates t to local midnight of its calendar date.
func dayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
