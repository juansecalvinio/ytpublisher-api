package rules

import "fmt"

const MaxTagsTotalLength = 500

func ValidateTags(tags []string) []Violation {
	total := 0
	for _, tag := range tags {
		total += len(tag)
	}
	if total > MaxTagsTotalLength {
		return []Violation{{
			Field:   "tags",
			Message: fmt.Sprintf("tags total %d characters, must be %d or fewer", total, MaxTagsTotalLength),
		}}
	}
	return nil
}
