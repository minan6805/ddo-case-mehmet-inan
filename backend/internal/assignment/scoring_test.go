package assignment

import (
	"testing"
	"time"
)

func instant(value string) time.Time {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return at
}
func expert(id int64) Expert {
	return Expert{ID: id, Capacity: 8, Enabled: true, Timezone: "Europe/Istanbul", ShiftStartMinute: 510, ShiftEndMinute: 1080, Weekdays: []int{1, 2, 3, 4, 5}, Skills: []Skill{{Code: "network", Level: 5}, {Code: "general", Level: 5}}}
}
func TestRankRequiresEverySkillAndEligibility(t *testing.T) {
	at := instant("2026-10-26T10:00:00+03:00")
	candidates := []Expert{expert(1), expert(2), expert(3), expert(4), expert(5), expert(6)}
	candidates[1].OnLeave = true
	candidates[2].Enabled = false
	candidates[3].Load = 8
	candidates[4].Skills = []Skill{{Code: "network", Level: 5}}
	candidates[5].ShiftEndMinute = 600
	scores := Rank(candidates, []string{"network", "general"}, at)
	if len(scores) != 1 || scores[0].ExpertID != 1 {
		t.Fatalf("ineligible experts included: %+v", scores)
	}
	if len(Rank(candidates, []string{}, at)) != 0 {
		t.Fatal("missing skill requirements treated as match")
	}
}
func TestScoreCombinesSkillLoadAndRoundRobin(t *testing.T) {
	at := instant("2026-10-26T10:00:00+03:00")
	a, b := expert(1), expert(2)
	a.Load = 7
	scores := Rank([]Expert{a, b}, []string{"network"}, at)
	if scores[0].ExpertID != 2 {
		t.Fatal("load was ignored")
	}
	a.Load = 0
	a.LastAssignmentSeq = 2
	b.LastAssignmentSeq = 1
	scores = Rank([]Expert{a, b}, []string{"network"}, at)
	if scores[0].ExpertID != 2 || scores[0].RotationPoints != 1000 {
		t.Fatal("rotation was ignored")
	}
	a.LastAssignmentSeq = 0
	a.Skills = []Skill{{Code: "network", Level: 1}}
	scores = Rank([]Expert{a, b}, []string{"network"}, at)
	if scores[0].ExpertID != 2 {
		t.Fatal("proficiency was ignored")
	}
	for _, score := range scores {
		if score.Total != 5*score.SkillPoints+3*score.SparePoints+2*score.RotationPoints {
			t.Fatal("score components differ")
		}
	}
}
func TestShiftTimezoneWeekendAndBoundaries(t *testing.T) {
	candidate := expert(1)
	cases := []struct {
		at   string
		want bool
	}{
		{"2026-10-26T05:29:59Z", false}, {"2026-10-26T05:30:00Z", true},
		{"2026-10-26T14:59:59Z", true}, {"2026-10-26T15:00:00Z", false},
		{"2026-10-24T12:00:00+03:00", false},
	}
	for _, entry := range cases {
		if onShift(candidate, instant(entry.at)) != entry.want {
			t.Errorf("shift: %s", entry.at)
		}
	}
	candidate.ShiftStartMinute = 0
	candidate.ShiftEndMinute = 1440
	candidate.Weekdays = []int{7}
	if !onShift(candidate, instant("2026-10-25T23:59:59+03:00")) {
		t.Fatal("Sunday all-day shift failed")
	}
}
func TestProfileRejectsInvalidCapacityShiftAndSkills(t *testing.T) {
	valid := ProfileInput{Capacity: 8, Enabled: true, Timezone: "Europe/Istanbul", ShiftEndMinute: 1440, Weekdays: []int{1, 2, 3, 4, 5, 6, 7}, Skills: []Skill{{Code: "network", Level: 5}}, Version: 1}
	if err := validate(valid); err != nil {
		t.Fatal(err)
	}
	cases := []ProfileInput{}
	changed := valid
	changed.Capacity = 0
	cases = append(cases, changed)
	changed = valid
	changed.Capacity = 101
	cases = append(cases, changed)
	changed = valid
	changed.Weekdays = []int{1, 1}
	cases = append(cases, changed)
	changed = valid
	changed.Weekdays = []int{}
	cases = append(cases, changed)
	changed = valid
	changed.Timezone = "Local"
	cases = append(cases, changed)
	changed = valid
	changed.ShiftStartMinute = 1440
	cases = append(cases, changed)
	changed = valid
	changed.Skills = []Skill{{Code: "network", Level: 0}}
	cases = append(cases, changed)
	changed = valid
	changed.Skills = []Skill{{Code: "network", Level: 1}, {Code: "network", Level: 5}}
	cases = append(cases, changed)
	changed = valid
	changed.Version = 0
	cases = append(cases, changed)
	for _, entry := range cases {
		if validate(entry) == nil {
			t.Fatal("invalid profile accepted")
		}
	}
}
