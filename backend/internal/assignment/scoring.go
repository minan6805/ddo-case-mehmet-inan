package assignment

import (
	"errors"
	"sort"
	"strings"
	"time"
)

var ErrInvalid = errors.New("invalid expert profile")
var ErrConflict = errors.New("expert profile version conflict")
var ErrNotFound = errors.New("assignment entity not found")
var ErrForbidden = errors.New("assignment access forbidden")

type Skill struct {
	Code  string `json:"code"`
	Level int    `json:"level"`
}
type SkillInfo struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}
type Expert struct {
	ID                int64   `json:"id"`
	DisplayName       string  `json:"display_name"`
	Email             string  `json:"email"`
	Capacity          int     `json:"capacity"`
	Load              int     `json:"load"`
	Enabled           bool    `json:"enabled"`
	OnLeave           bool    `json:"on_leave"`
	Timezone          string  `json:"timezone"`
	ShiftStartMinute  int     `json:"shift_start_minute"`
	ShiftEndMinute    int     `json:"shift_end_minute"`
	Weekdays          []int   `json:"weekdays"`
	Skills            []Skill `json:"skills"`
	LastAssignmentSeq int64   `json:"last_assignment_seq"`
	OnShift           bool    `json:"on_shift"`
	Version           int64   `json:"version"`
}
type ProfileInput struct {
	Capacity         int     `json:"capacity"`
	Enabled          bool    `json:"enabled"`
	OnLeave          bool    `json:"on_leave"`
	Timezone         string  `json:"timezone"`
	ShiftStartMinute int     `json:"shift_start_minute"`
	ShiftEndMinute   int     `json:"shift_end_minute"`
	Weekdays         []int   `json:"weekdays"`
	Skills           []Skill `json:"skills"`
	Version          int64   `json:"version"`
}

func weekdayMask(days []int) (int, error) {
	mask := 0
	for _, day := range days {
		if day < 1 || day > 7 || mask&(1<<(day-1)) != 0 {
			return 0, ErrInvalid
		}
		mask |= 1 << (day - 1)
	}
	if mask == 0 {
		return 0, ErrInvalid
	}
	return mask, nil
}
func weekdays(mask int) []int {
	result := []int{}
	for day := 1; day <= 7; day++ {
		if mask&(1<<(day-1)) != 0 {
			result = append(result, day)
		}
	}
	return result
}
func validate(input ProfileInput) error {
	if input.Capacity < 1 || input.Capacity > 100 || input.Version < 1 || input.Timezone == "" || input.Timezone == "Local" ||
		input.ShiftStartMinute < 0 || input.ShiftStartMinute > 1439 || input.ShiftEndMinute < 1 || input.ShiftEndMinute > 1440 || input.ShiftStartMinute >= input.ShiftEndMinute || len(input.Skills) > 50 {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(input.Timezone); err != nil {
		return ErrInvalid
	}
	if _, err := weekdayMask(input.Weekdays); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, skill := range input.Skills {
		if strings.TrimSpace(skill.Code) == "" || seen[skill.Code] || skill.Level < 1 || skill.Level > 5 {
			return ErrInvalid
		}
		seen[skill.Code] = true
	}
	return nil
}
func onShift(expert Expert, at time.Time) bool {
	location, err := time.LoadLocation(expert.Timezone)
	if err != nil {
		return false
	}
	local := at.In(location)
	day := int(local.Weekday())
	if day == 0 {
		day = 7
	}
	works := false
	for _, candidate := range expert.Weekdays {
		if candidate == day {
			works = true
			break
		}
	}
	minute := local.Hour()*60 + local.Minute()
	return works && minute >= expert.ShiftStartMinute && minute < expert.ShiftEndMinute
}
func skillPoints(expert Expert, required []string) (int, bool) {
	if len(required) == 0 {
		return 0, false
	}
	levels := map[string]int{}
	for _, skill := range expert.Skills {
		levels[skill.Code] = skill.Level
	}
	total := 0
	for _, code := range required {
		level := levels[code]
		if level < 1 {
			return 0, false
		}
		total += level
	}
	return total * 1000 / (5 * len(required)), true
}

type Score struct {
	ExpertID       int64 `json:"expert_id"`
	SkillPoints    int   `json:"skill_points"`
	SparePoints    int   `json:"spare_points"`
	RotationPoints int   `json:"rotation_points"`
	Total          int   `json:"total"`
}

// Score = 50% proficiency + 30% spare capacity + 20% relative round-robin position.
// Eligibility first requires every skill, an open shift, no leave, and a free slot.
func Rank(experts []Expert, required []string, at time.Time) []Score {
	eligible := []Expert{}
	for _, expert := range experts {
		_, matches := skillPoints(expert, required)
		if expert.Enabled && !expert.OnLeave && onShift(expert, at) && expert.Capacity > 0 && expert.Load < expert.Capacity && matches {
			eligible = append(eligible, expert)
		}
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].LastAssignmentSeq != eligible[j].LastAssignmentSeq {
			return eligible[i].LastAssignmentSeq < eligible[j].LastAssignmentSeq
		}
		return eligible[i].ID < eligible[j].ID
	})
	result := make([]Score, 0, len(eligible))
	for index, expert := range eligible {
		proficiency, _ := skillPoints(expert, required)
		spare := (expert.Capacity - expert.Load) * 1000 / expert.Capacity
		rotation := (len(eligible) - index) * 1000 / len(eligible)
		result = append(result, Score{ExpertID: expert.ID, SkillPoints: proficiency, SparePoints: spare, RotationPoints: rotation, Total: 5*proficiency + 3*spare + 2*rotation})
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Total > result[j].Total })
	return result
}
