package generation

import (
	"context"
	"strings"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/claude"
	"github.com/juansecalvinio/ytpublisher-api/internal/rules"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type fakeStyleProvider struct {
	summary styleanalysis.Summary
}

func (f *fakeStyleProvider) GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error) {
	return f.summary, nil
}

type fakeRelatedVideosFinder struct {
	videos []storage.ChannelVideo
}

func (f *fakeRelatedVideosFinder) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, error) {
	return f.videos, nil
}

type fakeContentGenerator struct {
	generateResult claude.ContentDraft
	repairResult   claude.ContentDraft
	repairCalls    int
}

func (f *fakeContentGenerator) Generate(ctx context.Context, input claude.GenerateInput) (claude.ContentDraft, error) {
	return f.generateResult, nil
}

func (f *fakeContentGenerator) Repair(ctx context.Context, draft claude.ContentDraft, violations []rules.Violation) (claude.ContentDraft, error) {
	f.repairCalls++
	return f.repairResult, nil
}

func validDraft() claude.ContentDraft {
	return claude.ContentDraft{
		Title: "A perfectly reasonable title",
		Hook:  "A short, punchy hook.",
		Body:  "The rest of the description follows here.",
		Tags:  []string{"go", "programming"},
	}
}

func TestGenerate_ReturnsAssembledOutputOnFirstTry(t *testing.T) {
	llm := &fakeContentGenerator{generateResult: validDraft()}
	orchestrator := NewOrchestrator(
		&fakeStyleProvider{},
		&fakeRelatedVideosFinder{videos: []storage.ChannelVideo{{VideoID: "v1", Title: "Related Video"}}},
		llm,
	)

	output, err := orchestrator.Generate(context.Background(), Input{ChannelID: "UC123", Topic: "Go basics"})
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if output.Title != "A perfectly reasonable title" {
		t.Errorf("Title = %q, want %q", output.Title, "A perfectly reasonable title")
	}
	if !strings.HasPrefix(output.Description, "A short, punchy hook.") {
		t.Errorf("Description = %q, want it to start with the hook", output.Description)
	}
	if len(output.RelatedVideos) != 1 || output.RelatedVideos[0].VideoID != "v1" {
		t.Errorf("RelatedVideos = %v, want the one video from the finder", output.RelatedVideos)
	}
	if len(output.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none", output.Warnings)
	}
	if llm.repairCalls != 0 {
		t.Errorf("repairCalls = %d, want 0 (no violations)", llm.repairCalls)
	}
}

func TestGenerate_RepairsWhenValidationFails(t *testing.T) {
	badDraft := validDraft()
	badDraft.Title = strings.Repeat("a", 101)

	llm := &fakeContentGenerator{
		generateResult: badDraft,
		repairResult:   validDraft(),
	}
	orchestrator := NewOrchestrator(&fakeStyleProvider{}, &fakeRelatedVideosFinder{}, llm)

	output, err := orchestrator.Generate(context.Background(), Input{ChannelID: "UC123", Topic: "Go basics"})
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if llm.repairCalls != 1 {
		t.Errorf("repairCalls = %d, want 1", llm.repairCalls)
	}
	if output.Title != "A perfectly reasonable title" {
		t.Errorf("Title = %q, want the repaired title", output.Title)
	}
	if len(output.Warnings) != 0 {
		t.Errorf("Warnings = %v, want none (repair succeeded)", output.Warnings)
	}
}

func TestGenerate_ForcesComplianceWhenRepairStillFails(t *testing.T) {
	badDraft := validDraft()
	badDraft.Title = strings.Repeat("a", 101)

	stillBadDraft := validDraft()
	stillBadDraft.Title = strings.Repeat("b", 105)

	llm := &fakeContentGenerator{
		generateResult: badDraft,
		repairResult:   stillBadDraft,
	}
	orchestrator := NewOrchestrator(&fakeStyleProvider{}, &fakeRelatedVideosFinder{}, llm)

	output, err := orchestrator.Generate(context.Background(), Input{ChannelID: "UC123", Topic: "Go basics"})
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if len(output.Title) > rules.MaxTitleLength {
		t.Errorf("len(output.Title) = %d, want <= %d (force-truncated)", len(output.Title), rules.MaxTitleLength)
	}
	if len(output.Warnings) == 0 {
		t.Error("Warnings = none, want at least one warning about the forced truncation")
	}
}
