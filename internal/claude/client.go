package claude

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/juansecalvinio/ytpublisher-api/internal/rules"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

const toolName = "emit_youtube_content"

type ContentDraft struct {
	Title           string   `json:"title"`
	Hook            string   `json:"hook"`
	Body            string   `json:"body"`
	Timestamps      string   `json:"timestamps"`
	LinksSection    string   `json:"links_section"`
	MentionsSection string   `json:"mentions_section"`
	Hashtags        []string `json:"hashtags"`
	Tags            []string `json:"tags"`
}

type GenerateInput struct {
	Topic         string
	Notes         string
	Language      string
	Links         []string
	Mentions      []string
	Tone          string
	StyleSummary  styleanalysis.Summary
	RelatedVideos []storage.ChannelVideo
}

type Client struct {
	anthropic anthropic.Client
	model     string
}

func NewClient(apiKey, model string) *Client {
	return &Client{
		anthropic: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:     model,
	}
}

func (c *Client) Generate(ctx context.Context, input GenerateInput) (ContentDraft, error) {
	prompt, err := buildGeneratePrompt(input)
	if err != nil {
		return ContentDraft{}, fmt.Errorf("claude: building prompt: %w", err)
	}
	return c.call(ctx, prompt)
}

func (c *Client) Repair(ctx context.Context, draft ContentDraft, violations []rules.Violation) (ContentDraft, error) {
	prompt, err := buildRepairPrompt(draft, violations)
	if err != nil {
		return ContentDraft{}, fmt.Errorf("claude: building repair prompt: %w", err)
	}
	return c.call(ctx, prompt)
}

func (c *Client) call(ctx context.Context, prompt string) (ContentDraft, error) {
	tool := anthropic.ToolParam{
		Name:        toolName,
		Description: anthropic.String("Emit the generated YouTube title, description parts, hashtags, and tags as structured data."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"title":            map[string]any{"type": "string"},
				"hook":             map[string]any{"type": "string", "description": "The opening hook, must be a short attention-grabbing line under 125 characters"},
				"body":             map[string]any{"type": "string"},
				"timestamps":       map[string]any{"type": "string"},
				"links_section":    map[string]any{"type": "string"},
				"mentions_section": map[string]any{"type": "string"},
				"hashtags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			Required: []string{"title", "hook", "body", "timestamps", "links_section", "mentions_section", "hashtags", "tags"},
			ExtraFields: map[string]any{
				"additionalProperties": false,
			},
		},
		Strict: anthropic.Bool(true),
	}

	resp, err := c.anthropic.Messages.New(ctx, anthropic.MessageNewParams{
		Model:      c.model,
		MaxTokens:  4096,
		Tools:      []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: anthropic.ToolChoiceParamOfTool(toolName),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	})
	if err != nil {
		return ContentDraft{}, fmt.Errorf("claude: request failed: %w", err)
	}

	for _, block := range resp.Content {
		if toolUse, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			var draft ContentDraft
			if err := json.Unmarshal([]byte(toolUse.JSON.Input.Raw()), &draft); err != nil {
				return ContentDraft{}, fmt.Errorf("claude: parsing tool input: %w", err)
			}
			return draft, nil
		}
	}
	return ContentDraft{}, fmt.Errorf("claude: response did not contain a tool_use block")
}
