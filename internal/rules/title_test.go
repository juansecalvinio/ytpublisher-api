package rules

import (
	"strings"
	"testing"
)

func TestValidateTitle_AllowsExactly100Characters(t *testing.T) {
	title := strings.Repeat("a", 100)

	violations := ValidateTitle(title)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for a 100-character title", violations)
	}
}

func TestValidateTitle_RejectsExactly101Characters(t *testing.T) {
	title := strings.Repeat("a", 101)

	violations := ValidateTitle(title)

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 for a 101-character title", len(violations))
	}
	if violations[0].Field != "title" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "title")
	}
}

func TestValidateTitle_AllowsEmptyTitle(t *testing.T) {
	violations := ValidateTitle("")

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for an empty title (length-only rule)", violations)
	}
}
