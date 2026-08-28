package generation

import (
	"context"
	"fmt"
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

	draft, err := o.llm.Generate(ctx, claude.GenerateInput{
		Topic:         input.Topic,
		Notes:         input.Notes,
		Language:      input.Language,
		Links:         input.Links,
		Mentions:      input.Mentions,
		Tone:          input.Tone,
		StyleSummary:  styleSummary,
		RelatedVideos: relatedVideos,
	})
	if err != nil {
		return Output{}, fmt.Errorf("generation: generating content: %w", err)
	}

	violations := rules.Validate(toGeneratedContent(draft))

	if len(violations) > 0 {
		repaired, err := o.llm.Repair(ctx, draft, violations)
		if err != nil {
			return Output{}, fmt.Errorf("generation: repairing content: %w", err)
		}
		draft = repaired
		violations = rules.Validate(toGeneratedContent(draft))
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
