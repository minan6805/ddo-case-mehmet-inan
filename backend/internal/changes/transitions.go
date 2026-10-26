package changes

// nextStatus validates the workflow separately from authorization and database writes.
func nextStatus(status, action string) (string, error) {
	next := ""
	switch action {
	case "submit":
		if status == "draft" {
			next = "submitted"
		}
	case "approve":
		if status == "submitted" {
			next = "approved"
		}
	case "reject":
		if status == "submitted" {
			next = "rejected"
		}
	case "request_changes":
		if status == "submitted" {
			next = "draft"
		}
	case "revise":
		if status == "submitted" || status == "approved" {
			next = "draft"
		}
	case "cancel":
		if status == "draft" || status == "submitted" || status == "approved" {
			next = "cancelled"
		}
	case "start":
		if status == "approved" {
			next = "implementing"
		}
	case "complete":
		if status == "implementing" {
			next = "completed"
		}
	case "fail":
		if status == "implementing" {
			next = "failed"
		}
	default:
		return "", ErrInvalid
	}
	if next == "" {
		return "", ErrTransition
	}
	return next, nil
}
