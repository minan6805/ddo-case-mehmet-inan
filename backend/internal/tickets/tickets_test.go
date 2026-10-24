package tickets

import (
	"strings"
	"testing"
)

func TestTicketStatusTransitions(t *testing.T) {
	allowed := map[string]bool{
		"open:in_progress": true, "in_progress:waiting_customer": true,
		"in_progress:resolved": true, "waiting_customer:in_progress": true,
		"resolved:in_progress": true, "resolved:closed": true,
	}
	statuses := []string{"open", "in_progress", "waiting_customer", "resolved", "closed", "invalid"}
	for _, from := range statuses {
		for _, to := range statuses {
			if actual := ValidTransition(from, to); actual != allowed[from+":"+to] {
				t.Errorf("%s -> %s = %v", from, to, actual)
			}
		}
	}
}
func TestTicketInputValidationAndWhitespace(t *testing.T) {
	input := CreateInput{Title: "  Bağlantı sorunu  ", Description: "  Açıklama  ", Priority: "normal", Category: "network"}
	normalized, err := normalize(input)
	if err != nil || normalized.Title != "Bağlantı sorunu" || normalized.Description != "Açıklama" {
		t.Fatal("normalization failed")
	}
	cases := []CreateInput{
		{Title: " ", Description: "x", Priority: "normal", Category: "general"},
		{Title: strings.Repeat("ş", 201), Description: "x", Priority: "normal", Category: "general"},
		{Title: "x", Description: " ", Priority: "normal", Category: "general"},
		{Title: "x", Description: strings.Repeat("a", 10001), Priority: "normal", Category: "general"},
		{Title: "x", Description: "x", Priority: "invalid", Category: "general"},
		{Title: "x", Description: "x", Priority: "normal", Category: ""},
	}
	for _, input := range cases {
		if _, err := normalize(input); err == nil {
			t.Errorf("invalid input accepted: %.40s", input.Title)
		}
	}
	input.Title = strings.Repeat("ş", 200)
	if _, err := normalize(input); err != nil {
		t.Fatal("valid Unicode title rejected")
	}
}
func TestIdempotencyKeyValidation(t *testing.T) {
	for _, value := range []string{"", strings.Repeat("a", 129), "with space", "newline\n", "ş"} {
		if ValidKey(value) {
			t.Errorf("accepted key %q", value)
		}
	}
	for _, value := range []string{"request-123", strings.Repeat("a", 128)} {
		if !ValidKey(value) {
			t.Errorf("rejected key %q", value)
		}
	}
}
