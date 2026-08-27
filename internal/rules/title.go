package rules

import "fmt"

const MaxTitleLength = 100

type Violation struct {
	Field   string
	Message string
}

func ValidateTitle(title string) []Violation {
	if len(title) > MaxTitleLength {
		return []Violation{{
			Field:   "title",
			Message: fmt.Sprintf("title is %d characters, must be %d or fewer", len(title), MaxTitleLength),
		}}
	}
	return nil
}
