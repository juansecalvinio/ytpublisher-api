package rules

type GeneratedContent struct {
	Title       string
	Description string
	Hook        string
	Tags        []string
}

func Validate(content GeneratedContent) []Violation {
	var violations []Violation
	violations = append(violations, ValidateTitle(content.Title)...)
	violations = append(violations, ValidateTags(content.Tags)...)
	violations = append(violations, ValidateDescriptionHook(content.Description, content.Hook)...)
	return violations
}
