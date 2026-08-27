package rules

import (
	"strings"
	"testing"
)

func TestValidateTags_AllowsExactly500TotalCharacters(t *testing.T) {
	tags := []string{strings.Repeat("a", 250), strings.Repeat("b", 250)}

	violations := ValidateTags(tags)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for exactly 500 total characters", violations)
	}
}

func TestValidateTags_RejectsOver500TotalCharacters(t *testing.T) {
	tags := []string{strings.Repeat("a", 250), strings.Repeat("b", 251)}

	violations := ValidateTags(tags)

	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 for 501 total characters", len(violations))
	}
	if violations[0].Field != "tags" {
		t.Errorf("violations[0].Field = %q, want %q", violations[0].Field, "tags")
	}
}

func TestValidateTags_AllowsEmptyTagsList(t *testing.T) {
	violations := ValidateTags(nil)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for an empty tags list", violations)
	}
}
