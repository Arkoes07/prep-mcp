package read_test

import (
	"testing"
	"time"

	"github.com/Arkoes07/prep-mcp/internal/read"
)

func TestISOWeek(t *testing.T) {
	tests := []struct {
		y    int
		m    time.Month
		d    int
		want string
	}{
		{2026, 1, 14, "2026-W03"},
		{2026, 1, 1, "2026-W01"},
		{2027, 1, 1, "2026-W53"},
		{2024, 12, 30, "2025-W01"},
		{2026, 9, 29, "2026-W40"},
	}
	for _, tc := range tests {
		day := time.Date(tc.y, tc.m, tc.d, 9, 0, 0, 0, time.Local)
		if got := read.ISOWeek(day); got != tc.want {
			t.Errorf("ISOWeek(%s) = %q, want %q", day.Format(time.DateOnly), got, tc.want)
		}
	}
}

// ParseDate returns the calendar date as written: whatever the input format,
// Format(time.DateOnly) of the result is the date in the input. Any time of day
// in the input is ignored.
func TestParseDate(t *testing.T) {
	// A YAML-quoted date reaches ParseDate already unquoted, so the quoted
	// form is covered by TestScan (date: "2026-01-12").
	ok := []string{"2026-01-12", "2026-01-12T09:30:00Z"}
	for _, in := range ok {
		got, err := read.ParseDate(in)
		if err != nil {
			t.Errorf("ParseDate(%s): %v", in, err)
			continue
		}
		if s := got.Format(time.DateOnly); s != "2026-01-12" {
			t.Errorf("ParseDate(%s) = %s, want 2026-01-12", in, s)
		}
	}
	for _, in := range []string{"12/01/2026", "garbage", "", "2026-13-01"} {
		if got, err := read.ParseDate(in); err == nil {
			t.Errorf("ParseDate(%q) = %v, want an error", in, got)
		}
	}
}
