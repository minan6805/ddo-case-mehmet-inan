// Package simulator generates a repeatable workload and resumes it from database checkpoints.
package simulator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"destekmasa/internal/tickets"
)

var (
	ErrInvalid        = errors.New("invalid simulation configuration")
	ErrConflict       = errors.New("simulation name already has a different plan")
	ErrRunning        = errors.New("another simulator process is running")
	ErrProfileChanged = errors.New("expert profile changed outside the simulation")
	ErrNotFound       = errors.New("simulation run not found")
	ErrForbidden      = errors.New("simulation status requires leadership role")
)

type Config struct {
	Name       string `json:"name"`
	Day        string `json:"day"`
	Seed       int64  `json:"seed"`
	Count      int    `json:"count"`
	DaySeconds int    `json:"day_seconds"`
}

func (c Config) Validate() error {
	day, err := time.Parse(time.DateOnly, c.Day)
	if err != nil || day.Format(time.DateOnly) != c.Day || day.Year() < 1 || !regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`).MatchString(c.Name) || c.Seed < 0 || c.Seed > 2147483647 || c.Count < 4 || c.Count > 800 || c.DaySeconds < 0 || c.DaySeconds > 86400 {
		return ErrInvalid
	}
	return nil
}
func (c Config) Namespace() string { return fmt.Sprintf("sim-v1/%s/%s/%d", c.Name, c.Day, c.Seed) }

type Step struct {
	Kind               string              `json:"kind"`
	Index              int                 `json:"index"`
	Aux                int                 `json:"aux"`
	Value              string              `json:"value,omitempty"`
	Input              tickets.CreateInput `json:"input"`
	OffsetMilliseconds int64               `json:"offset_milliseconds"`
}
type Plan struct {
	Config Config `json:"config"`
	Steps  []Step `json:"steps"`
	Hash   string `json:"-"`
}

func Build(c Config) (Plan, error) {
	if err := c.Validate(); err != nil {
		return Plan{}, err
	}
	p := Plan{Config: c, Steps: []Step{}}
	add := func(kind string, index, aux int, value string, offset int64, in tickets.CreateInput) {
		p.Steps = append(p.Steps, Step{Kind: kind, Index: index, Aux: aux, Value: value, Input: in, OffsetMilliseconds: offset})
	}
	empty := tickets.CreateInput{}
	for i, kind := range []string{"network", "server", "service", "application"} {
		add("asset", i, 0, kind, 0, empty)
	}
	for _, pair := range [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 2}} {
		add("edge", pair[0], pair[1], "", 0, empty)
	}
	categories := []string{"general", "network", "hardware", "software"}
	priorities := []string{"low", "normal", "high", "critical"}
	topics := []string{"Bağlantı sorunu", "Uygulama hatası", "Cihaz desteği", "Erişim isteği"}
	for i := 0; i < c.Count; i++ {
		digest := sha256.Sum256([]byte(fmt.Sprintf("%s/item/%d", c.Namespace(), i)))
		offset := int64(i) * int64(c.DaySeconds) * 1000 / int64(c.Count)
		if i == c.Count/4 {
			add("shift", 0, 0, "morning", offset, empty)
		}
		if i == c.Count/2 {
			add("shift", 1, 0, "afternoon", offset, empty)
		}
		if i == 3*c.Count/4 {
			add("shift", 0, 0, "restore", offset, empty)
		}
		in := tickets.CreateInput{Title: fmt.Sprintf("[Simülasyon %s] %s %04d", c.Name, topics[int(digest[0])%len(topics)], i+1), Description: fmt.Sprintf("Tekrarlanabilir örnek talep. Plan günü: %s; öğe: %d; örnek kod: %x.", c.Day, i+1, digest[2:6]), Category: categories[int(digest[1])%len(categories)], Priority: priorities[int(digest[2])%len(priorities)]}
		kind := "ticket"
		if i%5 == 0 {
			kind = "email"
			in.Category = "general"
			in.Priority = "normal"
		}
		add(kind, i, 0, "", offset, in)
		if kind == "email" {
			add("email_replay", i, 0, "", offset, in)
			add("reply", i, 0, "", offset, empty)
			add("reply_replay", i, 0, "", offset, empty)
		}
		if i%20 == 0 {
			add("note", i, 0, "", offset, empty)
		}
		if i%10 != 0 {
			add("status", i, 0, "in_progress", offset, empty)
		}
		if i%10 == 2 {
			add("status", i, 0, "waiting_customer", offset, empty)
		} else if i%10 >= 3 {
			add("status", i, 0, "resolved", offset, empty)
			if i%10 == 4 || i%10 == 6 || i%10 >= 8 {
				add("status", i, 0, "closed", offset, empty)
			}
		}
	}
	end := int64(c.DaySeconds) * 1000
	add("shift", 1, 0, "restore", end, empty)
	raw, err := json.Marshal(p)
	if err != nil {
		return Plan{}, err
	}
	sum := sha256.Sum256(raw)
	p.Hash = hex.EncodeToString(sum[:])
	return p, nil
}
