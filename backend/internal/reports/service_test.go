package reports

import (
	"errors"
	"testing"
	"time"
)

func TestReportDateRange(t *testing.T) {
	now := time.Date(2026, 10, 25, 22, 30, 0, 0, time.UTC) // October 26 in Istanbul.
	selected, err := normalize(Filter{}, now)
	if err != nil || selected.From != "2026-09-27" || selected.To != "2026-10-26" || selected.start.UTC() != time.Date(2026, 9, 26, 21, 0, 0, 0, time.UTC) || selected.end.UTC() != time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC) {
		t.Fatalf("%+v %v", selected, err)
	}
	for _, f := range []Filter{{From: "2026-10-25"}, {To: "2026-10-25"}, {From: "2026-02-30", To: "2026-03-01"}, {From: "2026-10-26", To: "2026-10-25"}, {From: "2026-1-01", To: "2026-01-01"}, {From: "2024-01-01", To: "2025-01-01"}, {From: "2026-01-01", To: "2026-01-01", Category: "a\x00b"}} {
		if _, err := normalize(f, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted %+v %v", f, err)
		}
	}
	for _, f := range []Filter{{From: "2024-01-01", To: "2024-12-31"}, {From: "2026-10-25", To: "2026-10-25"}} {
		if _, err := normalize(f, now); err != nil {
			t.Fatal(err)
		}
	}
}
