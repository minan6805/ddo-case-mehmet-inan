package sla

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid SLA input")
var ErrConflict = errors.New("SLA version conflict")

// Calendar counts weekdays in a local timezone. Holidays can close a whole day or its afternoon.
type Calendar struct {
	Timezone    string    `json:"timezone"`
	StartMinute int       `json:"start_minute"`
	EndMinute   int       `json:"end_minute"`
	Version     int64     `json:"version"`
	Holidays    []Holiday `json:"holidays"`
}
type Holiday struct {
	Date             string `json:"date"`
	ClosedFromMinute int    `json:"closed_from_minute"`
	Label            string `json:"label"`
}

type businessCalendar struct {
	location   *time.Location
	start, end int
	holidays   map[string]int
}

func (c Calendar) compile() (businessCalendar, error) {
	if c.Timezone == "" || c.Timezone == "Local" || c.StartMinute < 0 || c.EndMinute > 1440 || c.StartMinute >= c.EndMinute || c.Version < 1 || len(c.Holidays) > 1000 {
		return businessCalendar{}, ErrInvalid
	}
	location, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return businessCalendar{}, ErrInvalid
	}
	result := businessCalendar{location: location, start: c.StartMinute, end: c.EndMinute, holidays: map[string]int{}}
	for _, holiday := range c.Holidays {
		date, err := time.Parse("2006-01-02", holiday.Date)
		if err != nil || date.Format("2006-01-02") != holiday.Date || holiday.ClosedFromMinute < 0 || holiday.ClosedFromMinute > 1439 || len(strings.TrimSpace(holiday.Label)) < 1 || len([]rune(holiday.Label)) > 120 {
			return businessCalendar{}, ErrInvalid
		}
		if _, exists := result.holidays[holiday.Date]; exists {
			return businessCalendar{}, ErrInvalid
		}
		result.holidays[holiday.Date] = holiday.ClosedFromMinute
	}
	return result, nil
}
func (c businessCalendar) window(day time.Time) (time.Time, time.Time) {
	local := day.In(c.location)
	year, month, date := local.Date()
	start := time.Date(year, month, date, c.start/60, c.start%60, 0, 0, c.location)
	endMinute := c.end
	if closed, exists := c.holidays[local.Format("2006-01-02")]; exists && closed < endMinute {
		endMinute = closed
	}
	end := time.Date(year, month, date, endMinute/60, endMinute%60, 0, 0, c.location)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday || endMinute <= c.start {
		return start, start
	}
	return start, end
}

// Between intersects each local work window with [from,to); weekends and closures contribute zero.
func (c Calendar) Between(from, to time.Time) (time.Duration, error) {
	compiled, err := c.compile()
	if err != nil {
		return 0, err
	}
	return compiled.between(from, to)
}
func (c businessCalendar) between(from, to time.Time) (time.Duration, error) {
	if to.Before(from) {
		return 0, ErrInvalid
	}
	local := from.In(c.location)
	year, month, date := local.Date()
	day := time.Date(year, month, date, 0, 0, 0, 0, c.location)
	var elapsed time.Duration
	for i := 0; day.Before(to); i++ {
		if i >= 3660 {
			return 0, fmt.Errorf("calendar range exceeds ten years: %w", ErrInvalid)
		}
		start, end := c.window(day)
		if from.After(start) {
			start = from
		}
		if to.Before(end) {
			end = to
		}
		if end.After(start) {
			elapsed += end.Sub(start)
		}
		day = day.AddDate(0, 0, 1)
	}
	return elapsed, nil
}

// Add advances through work windows, preserving sub-second precision and exact closing boundaries.
func (c Calendar) Add(from time.Time, budget time.Duration) (time.Time, error) {
	compiled, err := c.compile()
	if err != nil {
		return time.Time{}, err
	}
	return compiled.add(from, budget)
}
func (c businessCalendar) add(from time.Time, budget time.Duration) (time.Time, error) {
	if budget < 0 {
		return time.Time{}, ErrInvalid
	}
	if budget == 0 {
		return from, nil
	}
	local := from.In(c.location)
	year, month, date := local.Date()
	day := time.Date(year, month, date, 0, 0, 0, 0, c.location)
	for i := 0; i < 3660; i++ {
		start, end := c.window(day)
		if from.After(start) {
			start = from
		}
		if end.After(start) {
			available := end.Sub(start)
			if budget <= available {
				return start.Add(budget).UTC(), nil
			}
			budget -= available
		}
		day = day.AddDate(0, 0, 1)
	}
	return time.Time{}, fmt.Errorf("calendar horizon exceeds ten years: %w", ErrInvalid)
}

type StatusEvent struct {
	Status string    `json:"status"`
	At     time.Time `json:"at"`
}
type Timer struct {
	BudgetSeconds  int64      `json:"budget_seconds"`
	ElapsedSeconds int64      `json:"elapsed_seconds"`
	DueAt          *time.Time `json:"due_at"`
	CompletedAt    *time.Time `json:"completed_at"`
	State          string     `json:"state"`
	Percent        int64      `json:"percent"`
}
type Snapshot struct {
	Response        Timer     `json:"response"`
	Resolution      Timer     `json:"resolution"`
	Paused          bool      `json:"paused"`
	CalendarVersion int64     `json:"calendar_version"`
	CheckedAt       time.Time `json:"checked_at"`
	EscalationLevel int       `json:"escalation_level"`
}

// Evaluate replays the status timeline under the current calendar, so calendar edits can be recomputed.
// The first in_progress event is the public acknowledgement; resolved/closed stop resolution time.
func Evaluate(calendar Calendar, history []StatusEvent, asOf time.Time, responseBudget, resolutionBudget int64) (Snapshot, error) {
	compiled, err := calendar.compile()
	if err != nil || len(history) == 0 || responseBudget < 1 || resolutionBudget < 1 || responseBudget > 43200*60 || resolutionBudget > 43200*60 {
		return Snapshot{}, ErrInvalid
	}
	if history[0].Status != "open" || history[0].At.After(asOf) || asOf.Sub(history[0].At) > 3660*24*time.Hour {
		return Snapshot{}, ErrInvalid
	}
	snapshot := Snapshot{CalendarVersion: calendar.Version, CheckedAt: asOf.UTC()}
	var responseElapsed, resolutionElapsed time.Duration
	var firstResponse, completed *time.Time
	for i, event := range history {
		if event.At.After(asOf) || (i > 0 && event.At.Before(history[i-1].At)) {
			return Snapshot{}, ErrInvalid
		}
		switch event.Status {
		case "open", "in_progress", "waiting_customer", "resolved", "closed":
		default:
			return Snapshot{}, ErrInvalid
		}
		if event.Status == "in_progress" && firstResponse == nil {
			at := event.At.UTC()
			firstResponse = &at
		}
		if event.Status == "resolved" || event.Status == "closed" {
			if completed == nil {
				at := event.At.UTC()
				completed = &at
			}
		} else {
			completed = nil
		}
		end := asOf
		if i+1 < len(history) {
			end = history[i+1].At
		}
		active := event.Status == "open" || event.Status == "in_progress"
		if active {
			elapsed, err := compiled.between(event.At, end)
			if err != nil {
				return Snapshot{}, err
			}
			resolutionElapsed += elapsed
			if firstResponse == nil {
				responseElapsed += elapsed
			}
		}
	}
	latest := history[len(history)-1].Status
	snapshot.Paused = latest == "waiting_customer"
	stopped := latest == "resolved" || latest == "closed"
	makeTimer := func(budget int64, elapsed time.Duration, done *time.Time) (Timer, error) {
		timer := Timer{BudgetSeconds: budget, ElapsedSeconds: int64(elapsed / time.Second), CompletedAt: done, State: "running"}
		timer.Percent = min(int64(100), timer.ElapsedSeconds*100/budget)
		if done != nil {
			timer.State = "completed"
		} else if snapshot.Paused {
			timer.State = "paused"
		} else if stopped {
			timer.State = "stopped"
		}
		if timer.ElapsedSeconds >= budget {
			timer.State = "breached"
		} else if timer.ElapsedSeconds*100 >= budget*80 && done == nil {
			timer.State = "warning"
		}
		if done == nil && !snapshot.Paused && !stopped {
			// Remaining exact duration avoids losing fractions of a second on repeated checks.
			remaining := time.Duration(budget)*time.Second - elapsed
			if remaining < 0 {
				remaining = 0
			}
			due, err := compiled.add(asOf, remaining)
			if err != nil {
				return Timer{}, err
			}
			if elapsed >= time.Duration(budget)*time.Second {
				// Locate the original deadline inside active intervals instead of making it move forward.
				left := time.Duration(budget) * time.Second
				for j, event := range history {
					end := asOf
					if j+1 < len(history) {
						end = history[j+1].At
					}
					if event.Status != "open" && event.Status != "in_progress" {
						continue
					}
					span, err := compiled.between(event.At, end)
					if err != nil {
						return Timer{}, err
					}
					if left <= span {
						due, err = compiled.add(event.At, left)
						if err != nil {
							return Timer{}, err
						}
						break
					}
					left -= span
				}
			}
			timer.DueAt = &due
		}
		return timer, nil
	}
	snapshot.Response, err = makeTimer(responseBudget, responseElapsed, firstResponse)
	if err != nil {
		return Snapshot{}, err
	}
	snapshot.Resolution, err = makeTimer(resolutionBudget, resolutionElapsed, completed)
	return snapshot, err
}
