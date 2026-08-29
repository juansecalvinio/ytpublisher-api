package claude

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
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
				"timestamps":       map[string]any{"type": "string", "description": "Only include this key at all if the video naturally has timestamped segments; omit the key entirely otherwise — never emit it as an empty string"},
				"links_section":    map[string]any{"type": "string", "description": "Only include this key at all if links were provided; omit the key entirely otherwise — never emit it as an empty string"},
				"mentions_section": map[string]any{"type": "string", "description": "Only include this key at all if mentions were provided; omit the key entirely otherwise — never emit it as an empty string"},
				"hashtags":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
			// timestamps/links_section/mentions_section are deliberately NOT
			// required: a rare Claude generation glitch was observed where
			// being forced to emit an empty string for one of these leaked
			// tool-call formatting syntax into the value instead. Making them
			// optional (omittable) avoids ever asking for an empty string.
			Required: []string{"title", "hook", "body", "hashtags", "tags"},
			ExtraFields: map[string]any{
				"additionalProperties": false,
			},
		},
		Strict: anthropic.Bool(true),
	}

	// DisableParallelToolUse is critical here: without it, Claude sometimes
	// calls this same tool multiple times in one response (observed 3 calls
	// in a single turn), where earlier calls can come back with corrupted
	// leaked tool-call syntax and only a later call is clean. Our code only
	// looks at the first tool_use block, so a parallel call could silently
	// hand us a garbled draft even though a clean one was generated moments
	// later in the same response. Forcing a single call removes that risk
	// entirely, rather than trying to pick "the best" of several calls.
	toolChoice := anthropic.ToolChoiceUnionParam{
		OfTool: &anthropic.ToolChoiceToolParam{
			Name:                   toolName,
			DisableParallelToolUse: param.NewOpt(true),
		},
	}

	resp, err := c.anthropic.Messages.New(ctx, anthropic.MessageNewParams{
		Model:      c.model,
		MaxTokens:  4096,
		Tools:      []anthropic.ToolUnionParam{{OfTool: &tool}},
		ToolChoice: toolChoice,
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
