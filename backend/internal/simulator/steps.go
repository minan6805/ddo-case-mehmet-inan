package simulator

import (
	"context"
	"fmt"
	"time"

	"destekmasa/internal/assets"
	"destekmasa/internal/assignment"
	"destekmasa/internal/auth"
	"destekmasa/internal/tickets"
)

// stepExecutor owns business operations; Runner owns scheduling, checkpoints and cleanup.
// State and counters point to Runner's variables so rollback to a prior checkpoint
// remains visible here without copying services or changing replay semantics.
type stepExecutor struct {
	plan        Plan
	state       *checkpoint
	result      *Run
	admin       auth.User
	employee    auth.User
	routing     *assignment.Service
	tickets     *tickets.Service
	assets      *assets.Service
	assetPrefix string
}

func (e *stepExecutor) execute(ctx context.Context, step Step) error {
	check, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	key := fmt.Sprintf("%s/step/%d", e.plan.Config.Namespace(), e.result.CompletedSteps)
	switch step.Kind {
	case "asset":
		a, err := e.assets.Create(check, e.admin, assets.Input{Code: fmt.Sprintf("%s-%d", e.assetPrefix, step.Index), Name: fmt.Sprintf("Simülasyon %s · %s", e.plan.Config.Name, step.Value), Kind: step.Value, State: "active"}, key)
		if err != nil {
			return err
		}
		e.state.Assets[step.Index] = a.ID
	case "edge":
		return e.assets.Dependency(check, e.admin, e.state.Assets[step.Index], e.state.Assets[step.Aux], true)
	case "ticket":
		ticket, err := e.tickets.Create(check, e.employee, step.Input, key)
		if err != nil {
			return err
		}
		e.state.Tickets[step.Index] = ticket.ID
		e.result.GeneratedTickets++
	case "email", "email_replay", "reply", "reply_replay":
		raw, err := mailBytes(e.plan, step, e.state.Tickets[step.Index])
		if err != nil {
			return err
		}
		receipt, err := e.tickets.IngestEmail(check, e.admin, raw)
		if err != nil {
			return err
		}
		if step.Kind == "email" {
			if !receipt.CreatedTicket {
				return ErrConflict
			}
			e.state.Tickets[step.Index] = receipt.TicketID
			e.result.GeneratedTickets++
		} else if receipt.TicketID != e.state.Tickets[step.Index] {
			return ErrConflict
		}
		if step.Kind == "email_replay" || step.Kind == "reply_replay" {
			if !receipt.Duplicate {
				return ErrConflict
			}
			e.result.EmailReplays++
		}
		if step.Kind == "reply" {
			if receipt.CreatedTicket {
				return ErrConflict
			}
			e.result.ReplyDeliveries++
		}
	case "note":
		_, err := e.tickets.AddComment(check, e.admin, e.state.Tickets[step.Index], tickets.CommentInput{Body: "Simülasyon iç notu: ilk kontroller tamamlandı.", Visibility: "internal"}, key)
		return err
	case "status":
		ticket, err := e.tickets.Get(check, e.admin, e.state.Tickets[step.Index])
		if err != nil {
			return err
		}
		if ticket.Status == step.Value {
			return nil
		}
		expected := map[string]string{"in_progress": "open", "waiting_customer": "in_progress", "resolved": "in_progress", "closed": "resolved"}
		if ticket.Status != expected[step.Value] {
			return ErrConflict
		}
		_, err = e.tickets.UpdateStatus(check, e.admin, ticket.ID, step.Value)
		return err
	case "shift":
		p := &e.state.Profiles[step.Index]
		target := p.Original
		if step.Value != "restore" {
			target = shiftInput(p.Original, step.Index)
		}
		previous := *p
		p.Desired = target
		if err := setProfile(check, e.routing, e.admin.ID, previous, step.Index, target); err != nil {
			return err
		}
		e.result.ShiftSteps++
	default:
		return ErrInvalid
	}
	return nil
}
