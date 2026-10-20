package read_test

import (
	"testing"
	"time"

	"github.com/arkoes07/prep-mcp/internal/read"
)

func TestFormatISOWeek(t *testing.T) {
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
		if got := read.FormatISOWeek(day); got != tc.want {
			t.Errorf("FormatISOWeek(%s) = %q, want %q", day.Format(time.DateOnly), got, tc.want)
		}
	}
}
