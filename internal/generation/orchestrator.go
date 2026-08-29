package generation

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/juansecalvinio/ytpublisher-api/internal/claude"
	"github.com/juansecalvinio/ytpublisher-api/internal/rules"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type StyleProvider interface {
	GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error)
}

type RelatedVideosFinder interface {
	FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, error)
}

type ContentGenerator interface {
	Generate(ctx context.Context, input claude.GenerateInput) (claude.ContentDraft, error)
	Repair(ctx context.Context, draft claude.ContentDraft, violations []rules.Violation) (claude.ContentDraft, error)
}

type Input struct {
	ChannelID string
	Topic     string
	Notes     string
	Language  string
	Links     []string
	Mentions  []string
	Tone      string
}

type RelatedVideo struct {
	VideoID string
	Title   string
}

type Output struct {
	Title         string
	Description   string
	Tags          []string
	RelatedVideos []RelatedVideo
	Warnings      []string
}

const relatedVideosLimit = 5

// maxGenerationAttempts caps how many times we'll call Generate() when the
// draft looks broken (leaked tool-call syntax, or an empty title/tags that
// slipped past the schema's "required" check, since required only means
// present, not non-empty). Total attempts, including the first — so this
// allows up to maxGenerationAttempts-1 retries.
const maxGenerationAttempts = 3

type Orchestrator struct {
	style   StyleProvider
	related RelatedVideosFinder
	llm     ContentGenerator
}

func NewOrchestrator(style StyleProvider, related RelatedVideosFinder, llm ContentGenerator) *Orchestrator {
	return &Orchestrator{style: style, related: related, llm: llm}
}

func (o *Orchestrator) Generate(ctx context.Context, input Input) (Output, error) {
	styleSummary, err := o.style.GetStyle(ctx, input.ChannelID)
	if err != nil {
		return Output{}, fmt.Errorf("generation: getting style: %w", err)
	}

	relatedVideos, err := o.related.FindRelated(ctx, input.ChannelID, input.Topic, relatedVideosLimit)
	if err != nil {
		return Output{}, fmt.Errorf("generation: finding related videos: %w", err)
	}

	llmInput := claude.GenerateInput{
		Topic:         input.Topic,
		Notes:         input.Notes,
		Language:      input.Language,
		Links:         input.Links,
		Mentions:      input.Mentions,
		Tone:          input.Tone,
		StyleSummary:  styleSummary,
		RelatedVideos: relatedVideos,
	}

	draft, err := o.llm.Generate(ctx, llmInput)
	if err != nil {
		return Output{}, fmt.Errorf("generation: generating content: %w", err)
	}

	for attempt := 1; attempt < maxGenerationAttempts && needsRetry(draft); attempt++ {
		log.Printf("generation: draft needs retry (attempt %d/%d): malformed=%v emptyTitle=%v emptyTags=%v",
			attempt, maxGenerationAttempts-1, looksMalformed(draft), draft.Title == "", len(draft.Tags) == 0)
		draft, err = o.llm.Generate(ctx, llmInput)
		if err != nil {
			return Output{}, fmt.Errorf("generation: generating content (retry %d): %w", attempt, err)
		}
	}
	// Sanitize unconditionally, whether or not a retry happened: the retry
	// reduces how often this leaks, but doesn't guarantee a clean response,
	// so this is the deterministic backstop that always runs.
	draft = sanitizeDraft(draft)

	violations := rules.Validate(toGeneratedContent(draft))

	if len(violations) > 0 {
		log.Printf("generation: initial draft violated rules, repairing: %v", violations)
		repaired, err := o.llm.Repair(ctx, draft, violations)
		if err != nil {
			return Output{}, fmt.Errorf("generation: repairing content: %w", err)
		}
		draft = sanitizeDraft(repaired)
		violations = rules.Validate(toGeneratedContent(draft))
		if len(violations) > 0 {
			log.Printf("generation: repair did not resolve all violations, forcing compliance: %v", violations)
		}
	}

	var warnings []string
	if len(violations) > 0 {
		draft, warnings = forceCompliance(draft, violations)
	}

	content := toGeneratedContent(draft)

	related := make([]RelatedVideo, len(relatedVideos))
	for i, v := range relatedVideos {
		related[i] = RelatedVideo{VideoID: v.VideoID, Title: v.Title}
	}

	return Output{
		Title:         content.Title,
		Description:   content.Description,
		Tags:          content.Tags,
		RelatedVideos: related,
		Warnings:      warnings,
	}, nil
}

// looksMalformed detects a rare Claude generation glitch where internal
// tool-call formatting syntax (e.g. "<parameter name=...>") leaks into a
// field's text content instead of being parsed into a clean tool_use block.
// This corruption doesn't necessarily violate any YouTube rule (a garbled
// title can still be short), so it needs its own check rather than relying
// on rules.Validate.
func looksMalformed(draft claude.ContentDraft) bool {
	const corruptionMarker = "<parameter"

	textFields := []string{draft.Title, draft.Hook, draft.Body, draft.Timestamps, draft.LinksSection, draft.MentionsSection}
	for _, f := range textFields {
		if strings.Contains(f, corruptionMarker) {
			return true
		}
	}
	for _, tag := range draft.Tags {
		if strings.Contains(tag, corruptionMarker) {
			return true
		}
	}
	for _, tag := range draft.Hashtags {
		if strings.Contains(tag, corruptionMarker) {
			return true
		}
	}
	return false
}

// corruptionTagPattern matches leaked tool-call syntax fragments regardless
// of surrounding garbage bytes (observed once with a stray unicode character
// inside the tag name), by matching any "<...>" span that mentions "antml"
// or "parameter" anywhere inside it.
var corruptionTagPattern = regexp.MustCompile(`<[^>]*(?:antml|parameter)[^>]*>`)

func sanitize(s string) string {
	return strings.TrimSpace(corruptionTagPattern.ReplaceAllString(s, ""))
}

// sanitizeDraft strips leaked tool-call syntax from every text field, as a
// deterministic backstop that runs regardless of whether looksMalformed
// triggered a retry — a retry reduces how often the corruption appears, but
// doesn't guarantee the replacement response is clean.
func sanitizeDraft(draft claude.ContentDraft) claude.ContentDraft {
	draft.Title = sanitize(draft.Title)
	draft.Hook = sanitize(draft.Hook)
	draft.Body = sanitize(draft.Body)
	draft.Timestamps = sanitize(draft.Timestamps)
	draft.LinksSection = sanitize(draft.LinksSection)
	draft.MentionsSection = sanitize(draft.MentionsSection)
	for i, tag := range draft.Tags {
		draft.Tags[i] = sanitize(tag)
	}
	for i, tag := range draft.Hashtags {
		draft.Hashtags[i] = sanitize(tag)
	}
	return draft
}

// needsRetry reports whether a draft is broken enough to be worth
// re-generating rather than repairing: leaked tool-call syntax, or an empty
// title/tags array — both indicate the model produced a schema-valid but
// unusable response, which the repair loop can't help with (repair fixes
// rule violations, not missing content).
func needsRetry(draft claude.ContentDraft) bool {
	return looksMalformed(draft) || draft.Title == "" || len(draft.Tags) == 0
}

func toGeneratedContent(draft claude.ContentDraft) rules.GeneratedContent {
	return rules.GeneratedContent{
		Title:       draft.Title,
		Hook:        draft.Hook,
		Description: assembleDescription(draft),
		Tags:        draft.Tags,
	}
}

func assembleDescription(draft claude.ContentDraft) string {
	parts := []string{draft.Hook}
	for _, part := range []string{draft.Body, draft.Timestamps, draft.LinksSection, draft.MentionsSection} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(draft.Hashtags) > 0 {
		parts = append(parts, strings.Join(draft.Hashtags, " "))
	}
	return strings.Join(parts, "\n\n")
}

func forceCompliance(draft claude.ContentDraft, violations []rules.Violation) (claude.ContentDraft, []string) {
	var warnings []string
	fixed := map[string]bool{}
	for _, v := range violations {
		if fixed[v.Field] {
			continue
		}
		fixed[v.Field] = true
		switch v.Field {
		case "title":
			draft.Title = truncate(draft.Title, rules.MaxTitleLength)
			warnings = append(warnings, fmt.Sprintf("title was truncated to %d characters", rules.MaxTitleLength))
		case "tags":
			draft.Tags = truncateTags(draft.Tags, rules.MaxTagsTotalLength)
			warnings = append(warnings, fmt.Sprintf("tags were trimmed to fit within %d characters total", rules.MaxTagsTotalLength))
		case "description_hook":
			draft.Hook = truncate(draft.Hook, rules.MaxHookLength)
			warnings = append(warnings, fmt.Sprintf("description hook was truncated to %d characters", rules.MaxHookLength))
		}
	}
	return draft, warnings
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func truncateTags(tags []string, maxTotal int) []string {
	var result []string
	total := 0
	for _, tag := range tags {
		if total+len(tag) > maxTotal {
			break
		}
		result = append(result, tag)
		total += len(tag)
	}
	return result
}
