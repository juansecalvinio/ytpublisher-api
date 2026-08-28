package claude

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/rules"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

func TestGenerate_ReturnsPopulatedDraft(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping integration test against the real Claude API")
	}

	client := NewClient(apiKey, "claude-sonnet-5")

	draft, err := client.Generate(context.Background(), GenerateInput{
		Topic:    "How to write your first Go program",
		Language: "English",
		Tone:     "friendly and encouraging",
		StyleSummary: styleanalysis.Summary{
			Confidence:          "high",
			AverageTitleLength:  55,
			AverageTagsPerVideo: 6,
		},
	})
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if draft.Title == "" {
		t.Error("draft.Title is empty")
	}
	if draft.Hook == "" {
		t.Error("draft.Hook is empty")
	}
	if len(draft.Tags) == 0 {
		t.Error("draft.Tags is empty")
	}
}

func TestRepair_FixesReportedViolation(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set; skipping integration test against the real Claude API")
	}

	client := NewClient(apiKey, "claude-sonnet-5")

	badDraft := ContentDraft{
		Title: strings.Repeat("a", 150),
		Hook:  "A short hook.",
		Body:  "Some body text.",
		Tags:  []string{"go", "programming"},
	}
	violations := []rules.Violation{
		{Field: "title", Message: "title is 150 characters, must be 100 or fewer"},
	}

	repaired, err := client.Repair(context.Background(), badDraft, violations)
	if err != nil {
		t.Fatalf("Repair() returned unexpected error: %v", err)
	}
	if len(repaired.Title) > rules.MaxTitleLength {
		t.Errorf("len(repaired.Title) = %d, want <= %d", len(repaired.Title), rules.MaxTitleLength)
	}
}
