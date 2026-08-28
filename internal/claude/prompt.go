package claude

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/juansecalvinio/ytpublisher-api/internal/rules"
)

func buildGeneratePrompt(input GenerateInput) (string, error) {
	styleJSON, err := json.Marshal(input.StyleSummary)
	if err != nil {
		return "", err
	}

	var relatedTitles []string
	for _, v := range input.RelatedVideos {
		relatedTitles = append(relatedTitles, v.Title)
	}

	var b strings.Builder
	b.WriteString("You write YouTube video publishing metadata (title, description, tags) that matches a specific channel's real style.\n\n")
	b.WriteString("Channel style summary (from statistical analysis of the channel's real videos):\n")
	b.Write(styleJSON)
	b.WriteString("\n\nMatch this style in tone, title format, tag usage, and description structure.\n\n")
	b.WriteString(fmt.Sprintf("Topic for the new video: %s\n", input.Topic))
	if input.Notes != "" {
		b.WriteString(fmt.Sprintf("Notes/transcript excerpt: %s\n", input.Notes))
	}
	if input.Language != "" {
		b.WriteString(fmt.Sprintf("Write in this language: %s\n", input.Language))
	}
	if input.Tone != "" {
		b.WriteString(fmt.Sprintf("Desired tone: %s\n", input.Tone))
	}
	if len(input.Links) > 0 {
		b.WriteString(fmt.Sprintf("Links to include in links_section: %s\n", strings.Join(input.Links, ", ")))
	}
	if len(input.Mentions) > 0 {
		b.WriteString(fmt.Sprintf("Mentions to include in mentions_section: %s\n", strings.Join(input.Mentions, ", ")))
	}
	if len(relatedTitles) > 0 {
		b.WriteString(fmt.Sprintf("\nFor context only (do not invent a related videos list — that is handled separately), the channel's most relevant existing videos on this topic are: %s\n", strings.Join(relatedTitles, "; ")))
	}
	b.WriteString("\nThe hook must be under 125 characters and be the literal opening of the description. Only fill links_section or mentions_section if links or mentions were provided above; otherwise leave them as empty strings.")
	b.WriteString("\nAlways include at least 3 relevant tags in the tags array — never leave it empty.")

	return b.String(), nil
}

func buildRepairPrompt(draft ContentDraft, violations []rules.Violation) (string, error) {
	draftJSON, err := json.Marshal(draft)
	if err != nil {
		return "", err
	}

	var messages []string
	for _, v := range violations {
		messages = append(messages, fmt.Sprintf("- %s: %s", v.Field, v.Message))
	}

	var b strings.Builder
	b.WriteString("Here is a previous draft of YouTube video metadata you generated:\n")
	b.Write(draftJSON)
	b.WriteString("\n\nIt has the following problems:\n")
	b.WriteString(strings.Join(messages, "\n"))
	b.WriteString("\n\nReturn a corrected version that fixes ONLY these problems, keeping everything else as close to the original as possible.")
	b.WriteString(" For any character-limit problem, aim comfortably under the stated limit (a few characters of margin), not exactly at it — character counting is easy to get slightly wrong.")

	return b.String(), nil
}
