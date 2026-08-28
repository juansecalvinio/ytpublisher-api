package claude

import (
	"context"
	"os"
	"testing"

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
