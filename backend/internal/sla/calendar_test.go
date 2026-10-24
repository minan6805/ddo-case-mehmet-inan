package sla

import (
	"testing"
	"time"
)

func moment(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
func defaultCalendar() Calendar {
	return Calendar{Timezone: "Europe/Istanbul", StartMinute: 510, EndMinute: 1080, Version: 1, Holidays: []Holiday{}}
}
func TestBusinessDeadlineCases(t *testing.T) {
	calendar := defaultCalendar()
	calendar.Holidays = []Holiday{{Date: "2026-10-28", ClosedFromMinute: 780, Label: "Arife"}, {Date: "2026-10-29", ClosedFromMinute: 0, Label: "Bayram"}}
	cases := []struct {
		name, from string
		minutes    int
		want       string
	}{
		{"within_day", "2026-10-26T09:00:00+03:00", 60, "2026-10-26T10:00:00+03:00"},
		{"before_open", "2026-10-26T07:00:00+03:00", 30, "2026-10-26T09:00:00+03:00"},
		{"exact_close", "2026-10-26T17:30:00+03:00", 30, "2026-10-26T18:00:00+03:00"},
		{"after_close", "2026-10-26T18:00:00+03:00", 30, "2026-10-27T09:00:00+03:00"},
		{"friday_evening", "2026-10-23T17:30:00+03:00", 60, "2026-10-26T09:00:00+03:00"},
		{"weekend", "2026-10-24T12:00:00+03:00", 30, "2026-10-26T09:00:00+03:00"},
		{"half_and_full_holiday", "2026-10-28T12:30:00+03:00", 60, "2026-10-30T09:00:00+03:00"},
		{"full_holiday_start", "2026-10-29T10:00:00+03:00", 60, "2026-10-30T09:30:00+03:00"},
		{"full_business_day", "2026-10-26T08:30:00+03:00", 570, "2026-10-26T18:00:00+03:00"},
		{"utc_input", "2026-10-26T06:00:00Z", 60, "2026-10-26T10:00:00+03:00"},
		{"zero_budget", "2026-10-24T12:00:00+03:00", 0, "2026-10-24T12:00:00+03:00"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result, err := calendar.Add(moment(test.from), time.Duration(test.minutes)*time.Minute)
			if err != nil || !result.Equal(moment(test.want)) {
				t.Fatalf("got %s, err %v; want %s", result, err, test.want)
			}
			counted, err := calendar.Between(moment(test.from), result)
			if err != nil || counted != time.Duration(test.minutes)*time.Minute {
				t.Fatalf("inverse duration: %s %v", counted, err)
			}
		})
	}
}
func TestBusinessTimePrecisionAndDST(t *testing.T) {
	calendar := defaultCalendar()
	start := moment("2026-10-23T17:59:59.500+03:00")
	due, err := calendar.Add(start, time.Second)
	if err != nil || !due.Equal(moment("2026-10-26T08:30:00.500+03:00")) {
		t.Fatalf("precision: %s %v", due, err)
	}
	calendar.Timezone = "Europe/Berlin"
	// DST ends on Sunday; only Friday's 30 minutes and Monday's 30 minutes count.
	due, err = calendar.Add(moment("2026-10-23T17:30:00+02:00"), time.Hour)
	if err != nil || !due.Equal(moment("2026-10-26T09:00:00+01:00")) {
		t.Fatalf("DST: %s %v", due, err)
	}
}
func TestCalendarValidation(t *testing.T) {
	cases := []Calendar{
		{Timezone: "bad/timezone", StartMinute: 510, EndMinute: 1080, Version: 1},
		{Timezone: "UTC", StartMinute: 1080, EndMinute: 510, Version: 1},
		{Timezone: "UTC", StartMinute: -1, EndMinute: 510, Version: 1},
		{Timezone: "UTC", StartMinute: 0, EndMinute: 1441, Version: 1},
		{Timezone: "UTC", StartMinute: 0, EndMinute: 500, Version: 0},
	}
	for _, calendar := range cases {
		if _, err := calendar.Add(time.Now(), time.Hour); err == nil {
			t.Fatal("invalid calendar accepted")
		}
	}
	calendar := defaultCalendar()
	calendar.Holidays = []Holiday{{Date: "2026-02-30", Label: "invalid"}}
	if _, err := calendar.Add(time.Now(), time.Hour); err == nil {
		t.Fatal("invalid holiday date accepted")
	}
	calendar.Holidays = []Holiday{{Date: "2026-10-29", Label: "a"}, {Date: "2026-10-29", Label: "b"}}
	if _, err := calendar.Add(time.Now(), time.Hour); err == nil {
		t.Fatal("duplicate holiday accepted")
	}
	calendar = defaultCalendar()
	if _, err := calendar.Between(time.Now(), time.Now().Add(-time.Hour)); err == nil {
		t.Fatal("negative range accepted")
	}
	if _, err := calendar.Add(time.Now(), -time.Second); err == nil {
		t.Fatal("negative budget accepted")
	}
}
func TestSLAWaitingPausesAndResumeMovesDeadline(t *testing.T) {
	calendar := defaultCalendar()
	history := []StatusEvent{{Status: "open", At: moment("2026-10-26T09:00:00+03:00")}, {Status: "in_progress", At: moment("2026-10-26T09:10:00+03:00")}, {Status: "waiting_customer", At: moment("2026-10-26T09:30:00+03:00")}}
	paused, err := Evaluate(calendar, history, moment("2026-10-26T16:00:00+03:00"), 30*60, 60*60)
	if err != nil || !paused.Paused || paused.Resolution.ElapsedSeconds != 30*60 || paused.Resolution.DueAt != nil || paused.Response.CompletedAt == nil || paused.Response.ElapsedSeconds != 10*60 {
		t.Fatalf("pause: %+v %v", paused, err)
	}
	history = append(history, StatusEvent{Status: "in_progress", At: moment("2026-10-27T09:00:00+03:00")})
	resumed, err := Evaluate(calendar, history, moment("2026-10-27T09:10:00+03:00"), 30*60, 60*60)
	if err != nil || resumed.Paused || resumed.Resolution.ElapsedSeconds != 40*60 || resumed.Resolution.DueAt == nil || !resumed.Resolution.DueAt.Equal(moment("2026-10-27T09:30:00+03:00")) {
		t.Fatalf("resume: %+v %v", resumed, err)
	}
}
func TestSLAWarningBreachAndStableOverdueDeadline(t *testing.T) {
	calendar := defaultCalendar()
	history := []StatusEvent{{Status: "open", At: moment("2026-10-26T09:00:00+03:00")}}
	warning, err := Evaluate(calendar, history, moment("2026-10-26T09:48:00+03:00"), 60*60, 120*60)
	if err != nil || warning.Response.State != "warning" || warning.Response.Percent != 80 {
		t.Fatalf("warning: %+v %v", warning, err)
	}
	for _, asOf := range []string{"2026-10-26T10:00:00+03:00", "2026-10-26T11:00:00+03:00"} {
		breached, err := Evaluate(calendar, history, moment(asOf), 60*60, 120*60)
		if err != nil || breached.Response.State != "breached" || breached.Response.DueAt == nil || !breached.Response.DueAt.Equal(moment("2026-10-26T10:00:00+03:00")) {
			t.Fatalf("breach: %+v %v", breached, err)
		}
	}
}
func TestSLAResolvedAndReopened(t *testing.T) {
	history := []StatusEvent{
		{Status: "open", At: moment("2026-10-26T09:00:00+03:00")}, {Status: "in_progress", At: moment("2026-10-26T09:10:00+03:00")},
		{Status: "resolved", At: moment("2026-10-26T09:30:00+03:00")},
	}
	resolved, err := Evaluate(defaultCalendar(), history, moment("2026-10-27T10:00:00+03:00"), 60*60, 120*60)
	if err != nil || resolved.Resolution.State != "completed" || resolved.Resolution.ElapsedSeconds != 30*60 {
		t.Fatalf("resolved: %+v %v", resolved, err)
	}
	history = append(history, StatusEvent{Status: "in_progress", At: moment("2026-10-27T10:00:00+03:00")})
	reopened, err := Evaluate(defaultCalendar(), history, moment("2026-10-27T10:15:00+03:00"), 60*60, 120*60)
	if err != nil || reopened.Resolution.CompletedAt != nil || reopened.Resolution.ElapsedSeconds != 45*60 || reopened.Response.ElapsedSeconds != 10*60 {
		t.Fatalf("reopen: %+v %v", reopened, err)
	}
}
func TestSLACalendarEditReplaysElapsedAndDeadline(t *testing.T) {
	history := []StatusEvent{{Status: "open", At: moment("2026-10-26T09:00:00+03:00")}}
	calendar := defaultCalendar()
	before, err := Evaluate(calendar, history, moment("2026-10-26T10:00:00+03:00"), 120*60, 180*60)
	if err != nil {
		t.Fatal(err)
	}
	calendar.Version++
	calendar.Holidays = []Holiday{{Date: "2026-10-26", Label: "Kapanış"}}
	after, err := Evaluate(calendar, history, moment("2026-10-26T10:00:00+03:00"), 120*60, 180*60)
	if err != nil || after.Response.ElapsedSeconds != 0 || before.Response.ElapsedSeconds != 3600 || after.Response.DueAt == nil || !after.Response.DueAt.Equal(moment("2026-10-27T10:30:00+03:00")) {
		t.Fatalf("recompute: %+v %v", after, err)
	}
}

// Independent minute-by-minute reference verifies hundreds of deadline inversions.
func TestBusinessCalendarAgainstMinuteReference(t *testing.T) {
	calendar := defaultCalendar()
	calendar.Holidays = []Holiday{{Date: "2026-10-28", ClosedFromMinute: 780, Label: "Arife"}, {Date: "2026-10-29", Label: "Tatil"}}
	location, _ := time.LoadLocation("Europe/Istanbul")
	for i := 0; i < 140; i++ {
		start := moment("2026-10-23T06:00:00+03:00").Add(time.Duration(i*73) * time.Minute)
		budget := 1 + (i*97)%1000
		left := budget
		cursor := start
		for left > 0 {
			local := cursor.In(location)
			minute := local.Hour()*60 + local.Minute()
			end := 1080
			if local.Format("2006-01-02") == "2026-10-28" {
				end = 780
			}
			if local.Format("2006-01-02") == "2026-10-29" {
				end = 0
			}
			if local.Weekday() != time.Saturday && local.Weekday() != time.Sunday && minute >= 510 && minute < end {
				left--
			}
			cursor = cursor.Add(time.Minute)
		}
		got, err := calendar.Add(start, time.Duration(budget)*time.Minute)
		if err != nil || !got.Equal(cursor) {
			t.Fatalf("case %d: got %s want %s (%v)", i, got, cursor, err)
		}
	}
}

func TestSLARejectsUnorderedFutureAndExcessiveHistory(t *testing.T) {
	calendar := defaultCalendar()
	start := moment("2026-10-26T09:00:00+03:00")
	cases := []struct {
		history []StatusEvent
		asOf    time.Time
	}{
		{[]StatusEvent{{Status: "open", At: start}}, start.Add(-time.Hour)},
		{[]StatusEvent{{Status: "open", At: start}, {Status: "in_progress", At: start.Add(-time.Minute)}}, start},
		{[]StatusEvent{{Status: "open", At: start}}, start.AddDate(11, 0, 0)},
		{[]StatusEvent{{Status: "unknown", At: start}}, start},
	}
	for _, entry := range cases {
		if _, err := Evaluate(calendar, entry.history, entry.asOf, 60, 120); err == nil {
			t.Fatal("invalid timeline accepted")
		}
	}
}
