package rules

import (
	"strings"
	"testing"
)

func TestValidateDescriptionHook_AllowsHookAtExactly125Characters(t *testing.T) {
	hook := strings.Repeat("a", 125)
	description := hook + " rest of the description"

	violations := ValidateDescriptionHook(description, hook)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for a 125-character hook", violations)
	}
}

func TestValidateDescriptionHook_RejectsHookOver125Characters(t *testing.T) {
	hook := strings.Repeat("a", 126)
	description := hook + " rest of the description"

	violations := ValidateDescriptionHook(description, hook)

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 for a 126-character hook", len(violations))
	}
	if violations[0].Field != "description_hook" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "description_hook")
	}
}

func TestValidateDescriptionHook_RequiresDescriptionToStartWithHook(t *testing.T) {
	violations := ValidateDescriptionHook("Something else entirely", "Expected hook")

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 when description does not start with the hook", len(violations))
	}
	if violations[0].Field != "description_hook" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "description_hook")
	}
}

func TestValidateDescriptionHook_ReturnsBothViolationsWhenHookIsTooLongAndMisplaced(t *testing.T) {
	hook := strings.Repeat("a", 126)

	violations := ValidateDescriptionHook("does not start with the hook", hook)

	if len(violations) != 2 {
		t.Fatalf("len(violations) = %d, want 2 (too long AND misplaced)", len(violations))
	}
}
