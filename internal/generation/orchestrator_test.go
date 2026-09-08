package generation

import (
	"context"
	"strings"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/claude"
	"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"
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
	usage  relatedvideos.Usage
}

func (f *fakeRelatedVideosFinder) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, relatedvideos.Usage, error) {
	return f.videos, f.usage, nil
}

type fakeContentGenerator struct {
	generateResult      claude.ContentDraft
	firstGenerateResult *claude.ContentDraft // if set, returned only on the first Generate() call
	generateUsage       claude.Usage
	generateCalls       int
	repairResult        claude.ContentDraft
	repairUsage         claude.Usage
	repairCalls         int
}

func (f *fakeContentGenerator) Generate(ctx context.Context, input claude.GenerateInput) (claude.ContentDraft, claude.Usage, error) {
	f.generateCalls++
	if f.generateCalls == 1 && f.firstGenerateResult != nil {
		return *f.firstGenerateResult, f.generateUsage, nil
	}
	return f.generateResult, f.generateUsage, nil
}

func (f *fakeContentGenerator) Repair(ctx context.Context, draft claude.ContentDraft, violations []rules.Violation) (claude.ContentDraft, claude.Usage, error) {
	f.repairCalls++
	return f.repairResult, f.repairUsage, nil
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

func TestGenerate_RetriesOnceWhenDraftLooksMalformed(t *testing.T) {
	malformed := validDraft()
	malformed.Title = "placeholder"
	malformed.Body = `Some text <parameter name="title">oops</parameter>`

	good := validDraft()

	llm := &fakeContentGenerator{
		firstGenerateResult: &malformed,
		generateResult:      good,
	}
	orchestrator := NewOrchestrator(&fakeStyleProvider{}, &fakeRelatedVideosFinder{}, llm)

	output, err := orchestrator.Generate(context.Background(), Input{ChannelID: "UC123", Topic: "Go basics"})
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if llm.generateCalls != 2 {
		t.Errorf("generateCalls = %d, want 2 (one retry after detecting corruption)", llm.generateCalls)
	}
	if output.Title != good.Title {
		t.Errorf("Title = %q, want the retried good draft's title %q", output.Title, good.Title)
	}
}

func TestGenerate_SanitizesLeakedToolSyntaxEvenWhenRetryIsAlsoMalformed(t *testing.T) {
	malformed := validDraft()
	malformed.Body = `Some text </antml_parameter>\n<parameter name="title">oops</parameter> more text`

	llm := &fakeContentGenerator{
		firstGenerateResult: &malformed,
		generateResult:      malformed, // the retry also comes back malformed
	}
	orchestrator := NewOrchestrator(&fakeStyleProvider{}, &fakeRelatedVideosFinder{}, llm)

	output, err := orchestrator.Generate(context.Background(), Input{ChannelID: "UC123", Topic: "Go basics"})
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if strings.Contains(output.Description, "parameter") || strings.Contains(output.Description, "antml") {
		t.Errorf("Description = %q, want leaked tool-call syntax stripped even after a persistently malformed retry", output.Description)
	}
}

func TestGenerate_AccumulatesUsageAcrossRetryAndRepair(t *testing.T) {
	malformed := validDraft()
	malformed.Title = "" // triggers a retry (needsRetry: empty title)

	badDraft := validDraft()
	badDraft.Title = strings.Repeat("a", 101) // triggers repair (rule violation)

	llm := &fakeContentGenerator{
		firstGenerateResult: &malformed,
		generateResult:      badDraft,
		generateUsage:       claude.Usage{InputTokens: 100, OutputTokens: 50},
		repairResult:        validDraft(),
		repairUsage:         claude.Usage{InputTokens: 40, OutputTokens: 20},
	}
	related := &fakeRelatedVideosFinder{
		usage: relatedvideos.Usage{EmbeddingCalls: 2, EmbeddingTokens: 30},
	}
	orchestrator := NewOrchestrator(&fakeStyleProvider{}, related, llm)

	output, err := orchestrator.Generate(context.Background(), Input{ChannelID: "UC123", Topic: "Go basics"})
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if llm.generateCalls != 2 {
		t.Fatalf("generateCalls = %d, want 2", llm.generateCalls)
	}
	if llm.repairCalls != 1 {
		t.Fatalf("repairCalls = %d, want 1", llm.repairCalls)
	}
	// Two Generate() calls at (100 in, 50 out) each, plus one Repair() call
	// at (40 in, 20 out): totals (240, 120).
	wantUsage := Usage{
		LLMInputTokens:  240,
		LLMOutputTokens: 120,
		EmbeddingCalls:  2,
		EmbeddingTokens: 30,
	}
	if output.Usage != wantUsage {
		t.Errorf("output.Usage = %+v, want %+v", output.Usage, wantUsage)
	}
}
