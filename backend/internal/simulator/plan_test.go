package simulator

import (
	"bytes"
	"encoding/json"
	"testing"

	mailinput "destekmasa/internal/email"
)

func testConfig(name string, count int) Config {
	return Config{Name: name, Day: "2026-10-26", Seed: 42, Count: count, DaySeconds: 0}
}
func TestDeterministicDailyPlan(t *testing.T) {
	c := testConfig("daily", 800)
	c.DaySeconds = 120
	p, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	q, err := Build(c)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(p)
	b, _ := json.Marshal(q)
	if !bytes.Equal(a, b) || p.Hash != q.Hash || len(p.Hash) != 64 {
		t.Fatal("same configuration produced different plan")
	}
	kinds := map[string]int{}
	var last int64
	for _, step := range p.Steps {
		kinds[step.Kind]++
		if step.OffsetMilliseconds < last {
			t.Fatal("schedule moved backwards")
		}
		last = step.OffsetMilliseconds
	}
	if kinds["ticket"] != 640 || kinds["email"] != 160 || kinds["email_replay"] != 160 || kinds["reply"] != 160 || kinds["reply_replay"] != 160 || kinds["shift"] != 4 || kinds["asset"] != 4 || kinds["edge"] != 4 || last != 120000 || len(p.Steps) != 3012 {
		t.Fatalf("%v steps=%d last=%d", kinds, len(p.Steps), last)
	}
	c.Seed++
	different, err := Build(c)
	if err != nil || different.Hash == p.Hash {
		t.Fatal("seed did not change plan")
	}
}
func TestSimulatorInputBounds(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.Name = "../bad" }, func(c *Config) { c.Day = "2026-02-30" }, func(c *Config) { c.Day = "0000-01-01" }, func(c *Config) { c.Seed = -1 }, func(c *Config) { c.Seed = 2147483648 }, func(c *Config) { c.Count = 3 }, func(c *Config) { c.Count = 801 }, func(c *Config) { c.DaySeconds = -1 }, func(c *Config) { c.DaySeconds = 86401 }} {
		c := testConfig("valid", 8)
		change(&c)
		if _, err := Build(c); err != ErrInvalid {
			t.Fatalf("%+v %v", c, err)
		}
	}
}
func TestRepeatableMIMEAndReply(t *testing.T) {
	p, err := Build(testConfig("mail", 8))
	if err != nil {
		t.Fatal(err)
	}
	var newStep, reply Step
	for _, s := range p.Steps {
		if s.Kind == "email" && s.Index == 0 {
			newStep = s
		}
		if s.Kind == "reply" && s.Index == 0 {
			reply = s
		}
	}
	a, err := mailBytes(p, newStep, 0)
	if err != nil {
		t.Fatal(err)
	}
	newStep.Kind = "email_replay"
	b, err := mailBytes(p, newStep, 0)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("mail retry bytes differ")
	}
	msg, err := mailinput.Parse(a)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg.Attachments) != 1 || msg.TicketID != 0 {
		t.Fatalf("%+v", msg)
	}
	a, err = mailBytes(p, reply, 123)
	if err != nil {
		t.Fatal(err)
	}
	reply.Kind = "reply_replay"
	b, err = mailBytes(p, reply, 123)
	if err != nil || !bytes.Equal(a, b) {
		t.Fatal("reply retry bytes differ")
	}
	msg, err = mailinput.Parse(a)
	if err != nil || msg.TicketID != 123 {
		t.Fatalf("%+v %v", msg, err)
	}
}
