package rules

import (
	"strings"
	"testing"
)

func TestValidate_ReturnsNoViolationsForValidContent(t *testing.T) {
	content := GeneratedContent{
		Title:       "A perfectly reasonable title",
		Hook:        "A short, punchy hook.",
		Description: "A short, punchy hook. And then the rest of the description follows here.",
		Tags:        []string{"go", "programming"},
	}

	violations := Validate(content)

	if len(violations) != 0 {
		t.Errorf("violations = %v, want none for fully valid content", violations)
	}
}

func TestValidate_AggregatesViolationsFromAllRules(t *testing.T) {
	content := GeneratedContent{
		Title:       strings.Repeat("a", 101),
		Hook:        strings.Repeat("b", 126),
		Description: strings.Repeat("b", 126) + " rest",
		Tags:        []string{strings.Repeat("c", 501)},
	}

	violations := Validate(content)

	fields := map[string]bool{}
	for _, v := range violations {
		fields[v.Field] = true
	}
	for _, want := range []string{"title", "tags", "description_hook"} {
		if !fields[want] {
			t.Errorf("expected a violation for field %q, got violations: %v", want, violations)
		}
	}
}
