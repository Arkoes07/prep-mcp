package read

import (
	"fmt"
	"time"
)

// FormatISOWeek formats t's ISO 8601 week as YYYY-Www (for example 2026-W03).
func FormatISOWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// truncateToDay truncates t to local midnight of its calendar date.
func truncateToDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local)
}
