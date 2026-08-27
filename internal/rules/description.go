package rules

import (
	"fmt"
	"strings"
)

const MaxHookLength = 125

func ValidateDescriptionHook(description, hook string) []Violation {
	var violations []Violation

	if len(hook) > MaxHookLength {
		violations = append(violations, Violation{
			Field:   "description_hook",
			Message: fmt.Sprintf("hook is %d characters, must be %d or fewer", len(hook), MaxHookLength),
		})
	}

	if !strings.HasPrefix(description, hook) {
		violations = append(violations, Violation{
			Field:   "description_hook",
			Message: "description must start with the hook",
		})
	}

	return violations
}
