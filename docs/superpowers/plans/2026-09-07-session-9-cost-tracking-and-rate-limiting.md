# Session 9: Cost Tracking and Rate Limiting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record real Claude/Voyage token usage and cost into `usage_events` for `/v1/generate`, and add per-minute + per-daily-cap rate limiting to the same endpoint.

**Architecture:** Cost data flows through the existing request lifecycle: `RequireAPIKey` middleware keeps inserting a zero-cost `usage_events` row up front and now also stores the generated `request_id` in the request context; `handleGenerate` reads it back, sums usage from every Claude/Voyage call the orchestrator made, prices it, and issues an `UPDATE` after the handler's real work is done (success or failure). Rate limiting is a separate middleware mounted only on `/v1/generate`, after `RequireAPIKey`, checking an in-memory per-minute counter then a Postgres-backed per-day counter, returning `429` before any paid API call happens.

**Tech Stack:** Go 1.25, chi router, pgx v5 (Postgres/Supabase), goose migrations, anthropic-sdk-go, raw HTTP to Voyage AI.

**Spec:** `docs/superpowers/specs/2026-09-07-session-9-cost-tracking-and-rate-limiting-design.md`

## Global Constraints

- Cost tracking and rate limiting apply ONLY to `POST /v1/generate` — no other endpoint changes.
- Pricing is hardcoded Go constants: Claude Sonnet 5 = $2/MTok input, $10/MTok output; Voyage 3.5 Lite = $0.02/MTok.
- Rate limit thresholds are env-configurable, defaulting to 10 requests/minute and 200 requests/day per client.
- A tracking/accounting failure (usage update, daily counter increment) must never fail the client-facing request — log and continue. The only condition that returns `429` is an actual limit being exceeded.
- No new tables for cost data (`usage_events` already has the needed columns). One new table, `client_daily_usage`, for the daily rate-limit cap.
- Follow existing patterns exactly: goose migration format (`-- +goose Up` / `-- +goose Down`), `Store` methods on `*storage.Store` using `s.pool`, table-driven or plain `t.Run`-free unit tests matching the style already in each package, integration tests against real Postgres/Anthropic/Voyage gated by `t.Skip` when the relevant API key/`DATABASE_URL` env var is unset.

---

### Task 1: `internal/pricing` package

**Files:**
- Create: `internal/pricing/pricing.go`
- Test: `internal/pricing/pricing_test.go`

**Interfaces:**
- Produces: `pricing.ClaudeCost(inputTokens, outputTokens int64) float64`, `pricing.EmbeddingCost(totalTokens int64) float64` — used by Task 12 (`handleGenerate`).

- [ ] **Step 1: Write the failing test**

```go
package pricing

import "testing"

func TestClaudeCost(t *testing.T) {
	tests := []struct {
		name                      string
		inputTokens, outputTokens int64
		want                      float64
	}{
		{"zero tokens", 0, 0, 0},
		{"1M input tokens only", 1_000_000, 0, 2.0},
		{"1M output tokens only", 0, 1_000_000, 10.0},
		{"mixed", 500_000, 200_000, 3.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClaudeCost(tt.inputTokens, tt.outputTokens)
			if got != tt.want {
				t.Errorf("ClaudeCost(%d, %d) = %v, want %v", tt.inputTokens, tt.outputTokens, got, tt.want)
			}
		})
	}
}

func TestEmbeddingCost(t *testing.T) {
	tests := []struct {
		name        string
		totalTokens int64
		want        float64
	}{
		{"zero tokens", 0, 0},
		{"1M tokens", 1_000_000, 0.02},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EmbeddingCost(tt.totalTokens)
			if got != tt.want {
				t.Errorf("EmbeddingCost(%d) = %v, want %v", tt.totalTokens, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pricing/... -v`
Expected: FAIL — build fails, `ClaudeCost`/`EmbeddingCost` undefined.

- [ ] **Step 3: Write minimal implementation**

```go
package pricing

// Prices are Claude Sonnet 5's standing (not introductory) per-token rate
// (platform.claude.com/docs/en/about-claude/pricing) and Voyage 3.5 Lite's
// rate (docs.voyageai.com/docs/pricing), captured 2026-09-07.
const (
	claudeInputCostPerMTok  = 2.0
	claudeOutputCostPerMTok = 10.0
	voyageCostPerMTok       = 0.02
)

func ClaudeCost(inputTokens, outputTokens int64) float64 {
	return float64(inputTokens)/1_000_000*claudeInputCostPerMTok +
		float64(outputTokens)/1_000_000*claudeOutputCostPerMTok
}

func EmbeddingCost(totalTokens int64) float64 {
	return float64(totalTokens) / 1_000_000 * voyageCostPerMTok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pricing/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pricing
git commit -m "$(cat <<'EOF'
internal/pricing: add Claude and Voyage cost calculation

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 2: `internal/claude` returns token usage

**Files:**
- Modify: `internal/claude/client.go`
- Modify: `internal/claude/client_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `claude.Usage{InputTokens, OutputTokens int64}`; `Client.Generate`/`Client.Repair` now return `(ContentDraft, Usage, error)` — consumed by Task 5 (`generation.ContentGenerator`).

- [ ] **Step 1: Update the test to the new signature**

Edit `internal/claude/client_test.go`:

```go
	draft, usage, err := client.Generate(context.Background(), GenerateInput{
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
	if usage.InputTokens == 0 {
		t.Error("usage.InputTokens = 0, want a real token count")
	}
	if usage.OutputTokens == 0 {
		t.Error("usage.OutputTokens = 0, want a real token count")
	}
```

And in `TestRepair_FixesReportedViolation`:

```go
	repaired, _, err := client.Repair(context.Background(), badDraft, violations)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/claude/... -v`
Expected: FAIL — build fails (`Generate`/`Repair` still return 2 values).

- [ ] **Step 3: Update the implementation**

In `internal/claude/client.go`, add the `Usage` type and thread it through `Generate`, `Repair`, and `call`:

```go
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}
```

```go
func (c *Client) Generate(ctx context.Context, input GenerateInput) (ContentDraft, Usage, error) {
	prompt, err := buildGeneratePrompt(input)
	if err != nil {
		return ContentDraft{}, Usage{}, fmt.Errorf("claude: building prompt: %w", err)
	}
	return c.call(ctx, prompt)
}

func (c *Client) Repair(ctx context.Context, draft ContentDraft, violations []rules.Violation) (ContentDraft, Usage, error) {
	prompt, err := buildRepairPrompt(draft, violations)
	if err != nil {
		return ContentDraft{}, Usage{}, fmt.Errorf("claude: building repair prompt: %w", err)
	}
	return c.call(ctx, prompt)
}

func (c *Client) call(ctx context.Context, prompt string) (ContentDraft, Usage, error) {
	// ... tool/toolChoice construction unchanged ...

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
		return ContentDraft{}, Usage{}, fmt.Errorf("claude: request failed: %w", err)
	}

	usage := Usage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}

	for _, block := range resp.Content {
		if toolUse, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
			var draft ContentDraft
			if err := json.Unmarshal([]byte(toolUse.JSON.Input.Raw()), &draft); err != nil {
				return ContentDraft{}, usage, fmt.Errorf("claude: parsing tool input: %w", err)
			}
			return draft, usage, nil
		}
	}
	return ContentDraft{}, usage, fmt.Errorf("claude: response did not contain a tool_use block")
}
```

Note the `call` failure paths that occur *after* a successful API response (missing tool_use block, bad JSON) still return the real `usage` — those tokens were genuinely billed even though parsing failed downstream. Only the request-failed path returns `Usage{}`, since no response came back to bill.

- [ ] **Step 4: Run test to verify it passes**

Run: `ANTHROPIC_API_KEY=<your key> go test ./internal/claude/... -v`
Expected: PASS (or SKIP if `ANTHROPIC_API_KEY` is unset — both are acceptable, this is an integration test).

- [ ] **Step 5: Commit**

```bash
git add internal/claude
git commit -m "$(cat <<'EOF'
internal/claude: return token usage from Generate and Repair

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 3: `internal/embeddings` returns token usage

**Files:**
- Modify: `internal/embeddings/client.go`
- Modify: `internal/embeddings/client_test.go`

**Interfaces:**
- Produces: `embeddings.Usage{TotalTokens int64}`; `Client.EmbedDocuments`/`Client.EmbedQuery` now return `([][]float32, Usage, error)` / `([]float32, Usage, error)` — consumed by Task 4 (`relatedvideos.Embedder`).

- [ ] **Step 1: Update the test to the new signature**

Edit `internal/embeddings/client_test.go`:

```go
	vectors, usage, err := client.EmbedDocuments(context.Background(), []string{"Go programming tutorial", "How to bake bread"})
	if err != nil {
		t.Fatalf("EmbedDocuments() returned unexpected error: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("len(vectors) = %d, want 2", len(vectors))
	}
	for i, v := range vectors {
		if len(v) != 1024 {
			t.Errorf("len(vectors[%d]) = %d, want 1024", i, len(v))
		}
	}
	if usage.TotalTokens == 0 {
		t.Error("usage.TotalTokens = 0, want a real token count")
	}
```

```go
	vector, usage, err := client.EmbedQuery(context.Background(), "Go programming")
	if err != nil {
		t.Fatalf("EmbedQuery() returned unexpected error: %v", err)
	}
	if len(vector) != 1024 {
		t.Errorf("len(vector) = %d, want 1024", len(vector))
	}
	if usage.TotalTokens == 0 {
		t.Error("usage.TotalTokens = 0, want a real token count")
	}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/embeddings/... -v`
Expected: FAIL — build fails.

- [ ] **Step 3: Update the implementation**

Rewrite `internal/embeddings/client.go`:

```go
package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const embeddingsURL = "https://api.voyageai.com/v1/embeddings"

type Client struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewClient(apiKey, model string) *Client {
	return &Client{apiKey: apiKey, model: model, httpClient: &http.Client{}}
}

type Usage struct {
	TotalTokens int64
}

func (c *Client) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, Usage, error) {
	return c.embed(ctx, texts, "document")
}

func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, Usage, error) {
	vectors, usage, err := c.embed(ctx, []string{text}, "query")
	if err != nil {
		return nil, Usage{}, err
	}
	return vectors[0], usage, nil
}

type embedRequest struct {
	Input     []string `json:"input"`
	Model     string   `json:"model"`
	InputType string   `json:"input_type"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		TotalTokens int64 `json:"total_tokens"`
	} `json:"usage"`
}

func (c *Client) embed(ctx context.Context, texts []string, inputType string) ([][]float32, Usage, error) {
	reqBody, err := json.Marshal(embedRequest{Input: texts, Model: c.model, InputType: inputType})
	if err != nil {
		return nil, Usage{}, fmt.Errorf("embeddings: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embeddingsURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, Usage{}, fmt.Errorf("embeddings: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, Usage{}, fmt.Errorf("embeddings: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, Usage{}, fmt.Errorf("embeddings: voyage returned status %d: %s", resp.StatusCode, body)
	}

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, Usage{}, fmt.Errorf("embeddings: decoding response: %w", err)
	}

	result := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(result) {
			return nil, Usage{}, fmt.Errorf("embeddings: response index %d out of range", d.Index)
		}
		result[d.Index] = d.Embedding
	}
	return result, Usage{TotalTokens: parsed.Usage.TotalTokens}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `VOYAGE_API_KEY=<your key> go test ./internal/embeddings/... -v`
Expected: PASS (or SKIP if `VOYAGE_API_KEY` is unset).

- [ ] **Step 5: Commit**

```bash
git add internal/embeddings
git commit -m "$(cat <<'EOF'
internal/embeddings: return total_tokens usage from Voyage calls

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 4: `internal/relatedvideos` aggregates embedding usage

**Files:**
- Modify: `internal/relatedvideos/provider.go`
- Modify: `internal/relatedvideos/provider_test.go`
- Modify: `internal/api/related_videos.go` (a *separate* interface, `api.RelatedVideosProvider`, also calls `Provider.FindRelated` directly for the debug endpoint `GET /v1/internal/channels/{channelID}/related-videos` — it breaks the moment `Provider.FindRelated`'s signature changes, so it must be fixed in this same task, before Task 9's `go test ./internal/api/...` checkpoint)
- Modify: `internal/api/router_test.go` (only the `fakeRelatedVideosProvider` fake, to match)

**Interfaces:**
- Consumes: `embeddings.Usage` (Task 3).
- Produces: `relatedvideos.Usage{EmbeddingCalls int, EmbeddingTokens int64}`; `Provider.FindRelated` now returns `([]storage.ChannelVideo, Usage, error)` — consumed by Task 5 (`generation.RelatedVideosFinder`) and adapted-around by `api.RelatedVideosProvider` below (that debug endpoint doesn't track cost, per the spec's non-goals, so it discards the usage value).

- [ ] **Step 1: Update the test to the new signature**

Rewrite `internal/relatedvideos/provider_test.go`:

```go
package relatedvideos

import (
	"context"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/embeddings"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type fakeVideoStore struct {
	videos            []storage.ChannelVideo
	updatedEmbeddings map[string][]float32
	similarResult     []storage.ChannelVideo
}

func (f *fakeVideoStore) ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error) {
	return f.videos, nil
}

func (f *fakeVideoStore) UpdateChannelVideoEmbedding(ctx context.Context, channelID, videoID string, embedding []float32) error {
	if f.updatedEmbeddings == nil {
		f.updatedEmbeddings = map[string][]float32{}
	}
	f.updatedEmbeddings[videoID] = embedding
	return nil
}

func (f *fakeVideoStore) FindSimilarVideos(ctx context.Context, channelID string, queryEmbedding []float32, limit int) ([]storage.ChannelVideo, error) {
	return f.similarResult, nil
}

type fakeEmbedder struct {
	documentCalls [][]string
	queryCalls    []string
}

func (f *fakeEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, embeddings.Usage, error) {
	f.documentCalls = append(f.documentCalls, texts)
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{float32(i)}
	}
	return result, embeddings.Usage{TotalTokens: int64(len(texts)) * 10}, nil
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, embeddings.Usage, error) {
	f.queryCalls = append(f.queryCalls, text)
	return []float32{1}, embeddings.Usage{TotalTokens: 5}, nil
}

func TestFindRelated_EmbedsOnlyMissingVideosBeforeSearching(t *testing.T) {
	videoStore := &fakeVideoStore{
		videos: []storage.ChannelVideo{
			{VideoID: "v1", Title: "Video 1"},
			{VideoID: "v2", Title: "Video 2", Embedding: []float32{0.5}},
		},
		similarResult: []storage.ChannelVideo{{VideoID: "v1", Title: "Video 1"}},
	}
	embedder := &fakeEmbedder{}
	provider := NewProvider(videoStore, embedder)

	results, usage, err := provider.FindRelated(context.Background(), "UC123", "some topic", 5)
	if err != nil {
		t.Fatalf("FindRelated() returned unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
	if len(embedder.documentCalls) != 1 || len(embedder.documentCalls[0]) != 1 {
		t.Errorf("documentCalls = %v, want exactly 1 call embedding exactly 1 text (only v1, which lacked an embedding)", embedder.documentCalls)
	}
	if _, ok := videoStore.updatedEmbeddings["v1"]; !ok {
		t.Error("expected v1's embedding to be updated")
	}
	if _, ok := videoStore.updatedEmbeddings["v2"]; ok {
		t.Error("v2 already had an embedding, should not have been re-embedded")
	}
	if len(embedder.queryCalls) != 1 || embedder.queryCalls[0] != "some topic" {
		t.Errorf("queryCalls = %v, want [\"some topic\"]", embedder.queryCalls)
	}
	// EmbedDocuments (1 text -> 10 tokens) + EmbedQuery (5 tokens) = 2 calls, 15 tokens.
	if usage.EmbeddingCalls != 2 {
		t.Errorf("usage.EmbeddingCalls = %d, want 2", usage.EmbeddingCalls)
	}
	if usage.EmbeddingTokens != 15 {
		t.Errorf("usage.EmbeddingTokens = %d, want 15", usage.EmbeddingTokens)
	}
}

func TestFindRelated_SkipsEmbeddingWhenAllVideosAlreadyEmbedded(t *testing.T) {
	videoStore := &fakeVideoStore{
		videos: []storage.ChannelVideo{
			{VideoID: "v1", Title: "Video 1", Embedding: []float32{0.1}},
		},
	}
	embedder := &fakeEmbedder{}
	provider := NewProvider(videoStore, embedder)

	_, usage, err := provider.FindRelated(context.Background(), "UC123", "some topic", 5)
	if err != nil {
		t.Fatalf("FindRelated() returned unexpected error: %v", err)
	}
	if len(embedder.documentCalls) != 0 {
		t.Errorf("documentCalls = %v, want none (all videos already embedded)", embedder.documentCalls)
	}
	// Only EmbedQuery ran (5 tokens); EmbedDocuments was skipped entirely.
	if usage.EmbeddingCalls != 1 {
		t.Errorf("usage.EmbeddingCalls = %d, want 1 (only EmbedQuery ran)", usage.EmbeddingCalls)
	}
	if usage.EmbeddingTokens != 5 {
		t.Errorf("usage.EmbeddingTokens = %d, want 5", usage.EmbeddingTokens)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relatedvideos/... -v`
Expected: FAIL — build fails.

- [ ] **Step 3: Update the implementation**

Rewrite `internal/relatedvideos/provider.go`:

```go
package relatedvideos

import (
	"context"
	"fmt"

	"github.com/juansecalvinio/ytpublisher-api/internal/embeddings"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type VideoStore interface {
	ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error)
	UpdateChannelVideoEmbedding(ctx context.Context, channelID, videoID string, embedding []float32) error
	FindSimilarVideos(ctx context.Context, channelID string, queryEmbedding []float32, limit int) ([]storage.ChannelVideo, error)
}

type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, embeddings.Usage, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, embeddings.Usage, error)
}

type Usage struct {
	EmbeddingCalls  int
	EmbeddingTokens int64
}

type Provider struct {
	videos   VideoStore
	embedder Embedder
}

func NewProvider(videos VideoStore, embedder Embedder) *Provider {
	return &Provider{videos: videos, embedder: embedder}
}

func (p *Provider) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, Usage, error) {
	var usage Usage

	videos, err := p.videos.ListChannelVideos(ctx, channelID)
	if err != nil {
		return nil, usage, fmt.Errorf("relatedvideos: listing videos: %w", err)
	}

	var missing []storage.ChannelVideo
	for _, v := range videos {
		if len(v.Embedding) == 0 {
			missing = append(missing, v)
		}
	}

	if len(missing) > 0 {
		texts := make([]string, len(missing))
		for i, v := range missing {
			texts[i] = v.Title + "\n\n" + v.Description
		}
		newEmbeddings, embedUsage, err := p.embedder.EmbedDocuments(ctx, texts)
		usage.EmbeddingCalls++
		usage.EmbeddingTokens += embedUsage.TotalTokens
		if err != nil {
			return nil, usage, fmt.Errorf("relatedvideos: embedding videos: %w", err)
		}
		for i, v := range missing {
			if err := p.videos.UpdateChannelVideoEmbedding(ctx, channelID, v.VideoID, newEmbeddings[i]); err != nil {
				return nil, usage, fmt.Errorf("relatedvideos: storing embedding: %w", err)
			}
		}
	}

	queryEmbedding, queryUsage, err := p.embedder.EmbedQuery(ctx, topic)
	usage.EmbeddingCalls++
	usage.EmbeddingTokens += queryUsage.TotalTokens
	if err != nil {
		return nil, usage, fmt.Errorf("relatedvideos: embedding topic: %w", err)
	}

	results, err := p.videos.FindSimilarVideos(ctx, channelID, queryEmbedding, limit)
	if err != nil {
		return nil, usage, fmt.Errorf("relatedvideos: searching similar videos: %w", err)
	}
	return results, usage, nil
}
```

Usage is accumulated *before* each error check, so a call that incurs real cost but then fails downstream (e.g. `EmbedDocuments` succeeds but `UpdateChannelVideoEmbedding` fails) still reports the tokens actually spent.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relatedvideos/... -v`
Expected: PASS

- [ ] **Step 5: Fix the `internal/api` call site broken by this signature change**

Update `internal/api/related_videos.go`'s interface and handler to match — this endpoint is a debug/internal one, not `/v1/generate`, so per the spec's non-goals it doesn't track cost and just discards the usage value:

```go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type RelatedVideosProvider interface {
	FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, relatedvideos.Usage, error)
}

const defaultRelatedVideosLimit = 5

func handleRelatedVideos(provider RelatedVideosProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := chi.URLParam(r, "channelID")
		topic := r.URL.Query().Get("topic")
		if channelID == "" || topic == "" {
			writeJSONError(w, http.StatusBadRequest, "channel id and topic are required")
			return
		}

		videos, _, err := provider.FindRelated(r.Context(), channelID, topic, defaultRelatedVideosLimit)
		if err != nil {
			log.Printf("related videos: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to find related videos")
			return
		}

		related := make([]map[string]string, len(videos))
		for i, v := range videos {
			related[i] = map[string]string{"video_id": v.VideoID, "title": v.Title}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"channel_id":     channelID,
			"topic":          topic,
			"related_videos": related,
		})
	}
}
```

Update `internal/api/router_test.go`'s `fakeRelatedVideosProvider` to match (only the fake's signature changes — no test assertions change, since none of the existing tests check usage on this endpoint):

```go
type fakeRelatedVideosProvider struct {
	videos []storage.ChannelVideo
	err    error
}

func (f *fakeRelatedVideosProvider) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, relatedvideos.Usage, error) {
	return f.videos, relatedvideos.Usage{}, f.err
}
```

(This requires adding `"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"` to `router_test.go`'s imports.)

- [ ] **Step 6: Run the api package's tests to verify they still pass**

Run: `go test ./internal/api/... -run TestRelatedVideos -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/relatedvideos internal/api/related_videos.go internal/api/router_test.go
git commit -m "$(cat <<'EOF'
internal/relatedvideos: aggregate embedding call count and token usage

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 5: `internal/generation` accumulates total usage

**Files:**
- Modify: `internal/generation/orchestrator.go`
- Modify: `internal/generation/orchestrator_test.go`

**Interfaces:**
- Consumes: `claude.Usage` (Task 2), `relatedvideos.Usage` (Task 4).
- Produces: `generation.Usage{LLMInputTokens, LLMOutputTokens int64, EmbeddingCalls int, EmbeddingTokens int64}` on `generation.Output.Usage` — consumed by Task 12 (`handleGenerate`).

- [ ] **Step 1: Update the test to the new signatures and add a usage-accumulation test**

Rewrite `internal/generation/orchestrator_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/generation/... -v`
Expected: FAIL — build fails.

- [ ] **Step 3: Update the implementation**

Rewrite `internal/generation/orchestrator.go`'s type declarations and `Generate` method (the rest of the file — `looksMalformed`, `sanitize`, `sanitizeDraft`, `needsRetry`, `toGeneratedContent`, `assembleDescription`, `forceCompliance`, `truncate`, `truncateTags` — is unchanged):

```go
package generation

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/juansecalvinio/ytpublisher-api/internal/claude"
	"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"
	"github.com/juansecalvinio/ytpublisher-api/internal/rules"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type StyleProvider interface {
	GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error)
}

type RelatedVideosFinder interface {
	FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, relatedvideos.Usage, error)
}

type ContentGenerator interface {
	Generate(ctx context.Context, input claude.GenerateInput) (claude.ContentDraft, claude.Usage, error)
	Repair(ctx context.Context, draft claude.ContentDraft, violations []rules.Violation) (claude.ContentDraft, claude.Usage, error)
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

type Usage struct {
	LLMInputTokens  int64
	LLMOutputTokens int64
	EmbeddingCalls  int
	EmbeddingTokens int64
}

type Output struct {
	Title         string
	Description   string
	Tags          []string
	RelatedVideos []RelatedVideo
	Warnings      []string
	Usage         Usage
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
	var usage Usage

	styleSummary, err := o.style.GetStyle(ctx, input.ChannelID)
	if err != nil {
		return Output{}, fmt.Errorf("generation: getting style: %w", err)
	}

	relatedVideos, relatedUsage, err := o.related.FindRelated(ctx, input.ChannelID, input.Topic, relatedVideosLimit)
	usage.EmbeddingCalls += relatedUsage.EmbeddingCalls
	usage.EmbeddingTokens += relatedUsage.EmbeddingTokens
	if err != nil {
		return Output{Usage: usage}, fmt.Errorf("generation: finding related videos: %w", err)
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

	draft, genUsage, err := o.llm.Generate(ctx, llmInput)
	usage.LLMInputTokens += genUsage.InputTokens
	usage.LLMOutputTokens += genUsage.OutputTokens
	if err != nil {
		return Output{Usage: usage}, fmt.Errorf("generation: generating content: %w", err)
	}

	for attempt := 1; attempt < maxGenerationAttempts && needsRetry(draft); attempt++ {
		log.Printf("generation: draft needs retry (attempt %d/%d): malformed=%v emptyTitle=%v emptyTags=%v",
			attempt, maxGenerationAttempts-1, looksMalformed(draft), draft.Title == "", len(draft.Tags) == 0)
		draft, genUsage, err = o.llm.Generate(ctx, llmInput)
		usage.LLMInputTokens += genUsage.InputTokens
		usage.LLMOutputTokens += genUsage.OutputTokens
		if err != nil {
			return Output{Usage: usage}, fmt.Errorf("generation: generating content (retry %d): %w", attempt, err)
		}
	}
	// Sanitize unconditionally, whether or not a retry happened: the retry
	// reduces how often this leaks, but doesn't guarantee a clean response,
	// so this is the deterministic backstop that always runs.
	draft = sanitizeDraft(draft)

	violations := rules.Validate(toGeneratedContent(draft))

	if len(violations) > 0 {
		log.Printf("generation: initial draft violated rules, repairing: %v", violations)
		repaired, repairUsage, err := o.llm.Repair(ctx, draft, violations)
		usage.LLMInputTokens += repairUsage.InputTokens
		usage.LLMOutputTokens += repairUsage.OutputTokens
		if err != nil {
			return Output{Usage: usage}, fmt.Errorf("generation: repairing content: %w", err)
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
		Usage:         usage,
	}, nil
}
```

Every usage accumulation happens *before* its error check, so a partially-successful request (e.g. the second `Generate` retry fails after the first one succeeded) still reports the tokens genuinely spent, matching the spec's requirement that a failed request which burned tokens shows a nonzero cost.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/generation/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/generation
git commit -m "$(cat <<'EOF'
internal/generation: accumulate Claude and embedding usage into Output

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 6: `storage.UpdateUsageEvent`

**Files:**
- Modify: `internal/storage/usage.go`
- Modify: `internal/storage/usage_test.go`

**Interfaces:**
- Produces: `storage.UsageUpdate{LLMInputTokens, LLMOutputTokens int64, EmbeddingCalls int, EstimatedCostUSD float64}`, `Store.UpdateUsageEvent(ctx, requestID string, update UsageUpdate) error` — consumed by Task 9 (`api.UsageRecorder`) and Task 12 (`handleGenerate`).

- [ ] **Step 1: Write the failing test**

Add to `internal/storage/usage_test.go`:

```go
func TestUpdateUsageEvent_UpdatesCostFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	client, err := store.CreateClient(ctx, "Usage Update Test Client", "usage-update-test@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), client.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})
	requestID := randomHash(t)
	if err := store.InsertUsageEvent(ctx, UsageEvent{ClientID: client.ID, RequestID: requestID, Endpoint: "/v1/generate"}); err != nil {
		t.Fatalf("InsertUsageEvent() returned unexpected error: %v", err)
	}
	// Registered after the DeleteClient cleanup above, so it runs first
	// (t.Cleanup is LIFO), avoiding a foreign key violation.
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), `DELETE FROM usage_events WHERE client_id = $1`, client.ID); err != nil {
			t.Errorf("cleanup delete usage_events returned error: %v", err)
		}
	})

	err = store.UpdateUsageEvent(ctx, requestID, UsageUpdate{
		LLMInputTokens:   1000,
		LLMOutputTokens:  500,
		EmbeddingCalls:   2,
		EstimatedCostUSD: 0.007,
	})
	if err != nil {
		t.Fatalf("UpdateUsageEvent() returned unexpected error: %v", err)
	}

	var inputTokens, outputTokens, embeddingCalls int
	var cost float64
	err = store.pool.QueryRow(ctx,
		`SELECT llm_input_tokens, llm_output_tokens, embedding_calls, estimated_cost_usd FROM usage_events WHERE request_id = $1`,
		requestID,
	).Scan(&inputTokens, &outputTokens, &embeddingCalls, &cost)
	if err != nil {
		t.Fatalf("querying updated row returned unexpected error: %v", err)
	}
	if inputTokens != 1000 || outputTokens != 500 || embeddingCalls != 2 {
		t.Errorf("got (%d, %d, %d), want (1000, 500, 2)", inputTokens, outputTokens, embeddingCalls)
	}
	if cost != 0.007 {
		t.Errorf("cost = %v, want 0.007", cost)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `DATABASE_URL=<your test db url> go test ./internal/storage/... -run TestUpdateUsageEvent -v`
Expected: FAIL — build fails (`UsageUpdate`/`UpdateUsageEvent` undefined).

- [ ] **Step 3: Write the implementation**

Rewrite `internal/storage/usage.go`:

```go
package storage

import "context"

type UsageEvent struct {
	ClientID  string
	RequestID string
	Endpoint  string
}

type UsageUpdate struct {
	LLMInputTokens   int64
	LLMOutputTokens  int64
	EmbeddingCalls   int
	EstimatedCostUSD float64
}

func (s *Store) InsertUsageEvent(ctx context.Context, event UsageEvent) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage_events (client_id, request_id, endpoint) VALUES ($1, $2, $3)`,
		event.ClientID, event.RequestID, event.Endpoint,
	)
	return err
}

func (s *Store) UpdateUsageEvent(ctx context.Context, requestID string, update UsageUpdate) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE usage_events
		 SET llm_input_tokens = $2, llm_output_tokens = $3, embedding_calls = $4, estimated_cost_usd = $5
		 WHERE request_id = $1`,
		requestID, update.LLMInputTokens, update.LLMOutputTokens, update.EmbeddingCalls, update.EstimatedCostUSD,
	)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `DATABASE_URL=<your test db url> go test ./internal/storage/... -run TestUpdateUsageEvent -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/storage/usage.go internal/storage/usage_test.go
git commit -m "$(cat <<'EOF'
internal/storage: add UpdateUsageEvent to persist real cost data

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 7: `client_daily_usage` table and `IncrementDailyGenerateCount`

**Files:**
- Create: `migrations/00007_create_client_daily_usage.sql`
- Create: `internal/storage/ratelimit.go`
- Create: `internal/storage/ratelimit_test.go`

**Interfaces:**
- Produces: `Store.IncrementDailyGenerateCount(ctx, clientID string) (int, error)` — consumed by Task 10 (`api.DailyLimiter`) and Task 13 (`main.go`).

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
CREATE TABLE client_daily_usage (
    client_id UUID NOT NULL REFERENCES api_clients(id),
    date DATE NOT NULL,
    request_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (client_id, date)
);

-- +goose Down
DROP TABLE client_daily_usage;
```

Apply it against your local/test database the same way prior migrations were applied (e.g. `goose -dir migrations postgres "$DATABASE_URL" up`).

- [ ] **Step 2: Write the failing test**

Create `internal/storage/ratelimit_test.go`:

```go
package storage

import (
	"context"
	"testing"
)

func TestIncrementDailyGenerateCount_AccumulatesAcrossCalls(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	client, err := store.CreateClient(ctx, "Rate Limit Test Client", "ratelimit-test@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), client.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})
	// Registered after DeleteClient, so it runs first (LIFO), avoiding a
	// foreign key violation.
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), `DELETE FROM client_daily_usage WHERE client_id = $1`, client.ID); err != nil {
			t.Errorf("cleanup delete client_daily_usage returned error: %v", err)
		}
	})

	first, err := store.IncrementDailyGenerateCount(ctx, client.ID)
	if err != nil {
		t.Fatalf("IncrementDailyGenerateCount() returned unexpected error: %v", err)
	}
	if first != 1 {
		t.Errorf("first count = %d, want 1", first)
	}

	second, err := store.IncrementDailyGenerateCount(ctx, client.ID)
	if err != nil {
		t.Fatalf("IncrementDailyGenerateCount() (second call) returned unexpected error: %v", err)
	}
	if second != 2 {
		t.Errorf("second count = %d, want 2", second)
	}
}

func TestIncrementDailyGenerateCount_TracksClientsIndependently(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	clientA, err := store.CreateClient(ctx, "Rate Limit Client A", "ratelimit-a@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() (A) returned unexpected error: %v", err)
	}
	clientB, err := store.CreateClient(ctx, "Rate Limit Client B", "ratelimit-b@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() (B) returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), clientA.ID); err != nil {
			t.Errorf("cleanup DeleteClient(A) returned error: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), clientB.ID); err != nil {
			t.Errorf("cleanup DeleteClient(B) returned error: %v", err)
		}
	})
	// Registered after both DeleteClient cleanups, so it runs first (LIFO).
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), `DELETE FROM client_daily_usage WHERE client_id IN ($1, $2)`, clientA.ID, clientB.ID); err != nil {
			t.Errorf("cleanup delete client_daily_usage returned error: %v", err)
		}
	})

	if _, err := store.IncrementDailyGenerateCount(ctx, clientA.ID); err != nil {
		t.Fatalf("IncrementDailyGenerateCount(A) returned unexpected error: %v", err)
	}

	countB, err := store.IncrementDailyGenerateCount(ctx, clientB.ID)
	if err != nil {
		t.Fatalf("IncrementDailyGenerateCount(B) returned unexpected error: %v", err)
	}
	if countB != 1 {
		t.Errorf("clientB count = %d, want 1 (independent from clientA)", countB)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `DATABASE_URL=<your test db url> go test ./internal/storage/... -run TestIncrementDailyGenerateCount -v`
Expected: FAIL — build fails (`IncrementDailyGenerateCount` undefined), or a "relation does not exist" error if the migration from Step 1 wasn't applied yet.

- [ ] **Step 4: Write the implementation**

Create `internal/storage/ratelimit.go`:

```go
package storage

import "context"

func (s *Store) IncrementDailyGenerateCount(ctx context.Context, clientID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`INSERT INTO client_daily_usage (client_id, date, request_count)
		 VALUES ($1, CURRENT_DATE, 1)
		 ON CONFLICT (client_id, date)
		 DO UPDATE SET request_count = client_daily_usage.request_count + 1
		 RETURNING request_count`,
		clientID,
	).Scan(&count)
	return count, err
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `DATABASE_URL=<your test db url> go test ./internal/storage/... -run TestIncrementDailyGenerateCount -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add migrations/00007_create_client_daily_usage.sql internal/storage/ratelimit.go internal/storage/ratelimit_test.go
git commit -m "$(cat <<'EOF'
storage: add client_daily_usage table and daily counter increment

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 8: `internal/ratelimit` in-memory per-minute limiter

**Files:**
- Create: `internal/ratelimit/limiter.go`
- Create: `internal/ratelimit/limiter_test.go`

**Interfaces:**
- Produces: `ratelimit.NewLimiter(max int, window time.Duration) *Limiter`, `(*Limiter).Allow(clientID string) bool` — consumed by Task 10 (`api.MinuteLimiter`) and Task 13 (`main.go`).

- [ ] **Step 1: Write the failing test**

```go
package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter_AllowsUpToMaxWithinWindow(t *testing.T) {
	l := NewLimiter(3, time.Minute)

	for i := 0; i < 3; i++ {
		if !l.Allow("client-1") {
			t.Fatalf("Allow() call %d = false, want true (under max)", i+1)
		}
	}
}

func TestLimiter_RejectsOverMaxWithinWindow(t *testing.T) {
	l := NewLimiter(2, time.Minute)

	l.Allow("client-1")
	l.Allow("client-1")

	if l.Allow("client-1") {
		t.Error("Allow() 3rd call = true, want false (over max within window)")
	}
}

func TestLimiter_TracksClientsIndependently(t *testing.T) {
	l := NewLimiter(1, time.Minute)

	l.Allow("client-1")

	if !l.Allow("client-2") {
		t.Error("Allow() for a different client = false, want true (limits are per-client)")
	}
}

func TestLimiter_AllowsAgainAfterWindowExpires(t *testing.T) {
	l := NewLimiter(1, 20*time.Millisecond)

	if !l.Allow("client-1") {
		t.Fatal("first Allow() = false, want true")
	}
	if l.Allow("client-1") {
		t.Fatal("second Allow() within window = true, want false")
	}

	time.Sleep(30 * time.Millisecond)

	if !l.Allow("client-1") {
		t.Error("Allow() after window expired = false, want true")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ratelimit/... -v`
Expected: FAIL — build fails, package doesn't exist yet.

- [ ] **Step 3: Write the implementation**

```go
package ratelimit

import (
	"sync"
	"time"
)

// Limiter is a per-client sliding-window rate limiter, held in memory. It is
// correct only for a single process — see the design spec's non-goals for
// what changes if this API ever runs as more than one instance.
type Limiter struct {
	mu     sync.Mutex
	max    int
	window time.Duration
	hits   map[string][]time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{
		max:    max,
		window: window,
		hits:   make(map[string][]time.Time),
	}
}

func (l *Limiter) Allow(clientID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	var recent []time.Time
	for _, t := range l.hits[clientID] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}

	if len(recent) >= l.max {
		l.hits[clientID] = recent
		return false
	}

	l.hits[clientID] = append(recent, now)
	return true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ratelimit/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ratelimit
git commit -m "$(cat <<'EOF'
internal/ratelimit: add in-memory per-client sliding window limiter

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 9: `RequireAPIKey` stores the request ID in context

**Files:**
- Modify: `internal/api/middleware.go`
- Modify: `internal/api/middleware_test.go`

**Interfaces:**
- Consumes: `storage.UsageUpdate` (Task 6, via the widened `UsageRecorder` interface).
- Produces: `api.RequestIDFromContext(ctx) (string, bool)` — consumed by Task 12 (`handleGenerate`). Widens `api.UsageRecorder` to also require `UpdateUsageEvent` — consumed by Task 12 and Task 13 (`main.go`, since `*storage.Store` must satisfy it, which it already does after Task 6).

- [ ] **Step 1: Update the test**

In `internal/api/middleware_test.go`, widen `fakeUsageRecorder` and add a new test. Replace the existing `fakeUsageRecorder` type and its method with:

```go
type usageEventUpdate struct {
	requestID string
	update    storage.UsageUpdate
}

type fakeUsageRecorder struct {
	events        []storage.UsageEvent
	updatedEvents []usageEventUpdate
}

func (f *fakeUsageRecorder) InsertUsageEvent(ctx context.Context, event storage.UsageEvent) error {
	f.events = append(f.events, event)
	return nil
}

func (f *fakeUsageRecorder) UpdateUsageEvent(ctx context.Context, requestID string, update storage.UsageUpdate) error {
	f.updatedEvents = append(f.updatedEvents, usageEventUpdate{requestID: requestID, update: update})
	return nil
}
```

Then append this test to the file:

```go
func TestRequireAPIKey_StoresRequestIDInContext(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1"}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	recorder := &fakeUsageRecorder{}

	var gotRequestID string
	var gotOK bool
	handler := RequireAPIKey(finder, recorder)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRequestID, gotOK = RequestIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !gotOK {
		t.Fatal("RequestIDFromContext() ok = false, want true")
	}
	if gotRequestID == "" {
		t.Error("RequestIDFromContext() returned empty string")
	}
	if gotRequestID != rec.Header().Get("X-Request-Id") {
		t.Errorf("context request ID = %q, want it to match the X-Request-Id header %q", gotRequestID, rec.Header().Get("X-Request-Id"))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -v`
Expected: FAIL — build fails (`RequestIDFromContext` undefined, `fakeUsageRecorder` missing `UpdateUsageEvent`).

- [ ] **Step 3: Update the implementation**

Rewrite `internal/api/middleware.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type ClientFinder interface {
	FindClientByAPIKeyHash(ctx context.Context, hash string) (storage.Client, error)
}

type UsageRecorder interface {
	InsertUsageEvent(ctx context.Context, event storage.UsageEvent) error
	UpdateUsageEvent(ctx context.Context, requestID string, update storage.UsageUpdate) error
}

type contextKey int

const (
	clientContextKey contextKey = iota
	requestIDContextKey
)

func ClientFromContext(ctx context.Context) (storage.Client, bool) {
	c, ok := ctx.Value(clientContextKey).(storage.Client)
	return c, ok
}

func RequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDContextKey).(string)
	return id, ok
}

func RequireAPIKey(finder ClientFinder, recorder UsageRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}

			client, err := finder.FindClientByAPIKeyHash(r.Context(), apikey.Hash(key))
			if err != nil {
				if !errors.Is(err, storage.ErrClientNotFound) {
					log.Printf("auth: lookup failed: %v", err)
				}
				writeJSONError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}

			requestID := NewRequestID()
			w.Header().Set("X-Request-Id", requestID)

			if err := recorder.InsertUsageEvent(r.Context(), storage.UsageEvent{
				ClientID:  client.ID,
				RequestID: requestID,
				Endpoint:  r.URL.Path,
			}); err != nil {
				log.Printf("auth: failed to record usage event: %v", err)
			}

			ctx := context.WithValue(r.Context(), clientContextKey, client)
			ctx = context.WithValue(ctx, requestIDContextKey, requestID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
```

- [ ] **Step 4: Run test to verify it passes**

`router_test.go` isn't modified until Task 12, but it already uses the same `fakeUsageRecorder` type defined in `middleware_test.go`, so it picks up the new `UpdateUsageEvent` method automatically and needs no changes yet. The whole package should compile and pass as-is.

Run: `go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/middleware.go internal/api/middleware_test.go
git commit -m "$(cat <<'EOF'
internal/api: store the generated request ID in request context

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 10: `RequireRateLimit` middleware

**Files:**
- Create: `internal/api/ratelimit.go`
- Create: `internal/api/ratelimit_test.go`

**Interfaces:**
- Consumes: `storage.Client` (via `ClientFromContext`, Task 9's file).
- Produces: `api.MinuteLimiter`, `api.DailyLimiter` interfaces, `api.RequireRateLimit(minuteLimiter MinuteLimiter, dailyLimiter DailyLimiter, dailyCap int) func(http.Handler) http.Handler` — consumed by Task 12 (`router.go`).

- [ ] **Step 1: Write the failing test**

Create `internal/api/ratelimit_test.go`:

```go
package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type fakeMinuteLimiter struct {
	allow bool
}

func (f *fakeMinuteLimiter) Allow(clientID string) bool {
	return f.allow
}

type fakeDailyLimiter struct {
	count int
	err   error
}

func (f *fakeDailyLimiter) IncrementDailyGenerateCount(ctx context.Context, clientID string) (int, error) {
	return f.count, f.err
}

func withClient(r *http.Request, client storage.Client) *http.Request {
	ctx := context.WithValue(r.Context(), clientContextKey, client)
	return r.WithContext(ctx)
}

func TestRequireRateLimit_AllowsWhenUnderBothLimits(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{count: 5}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRateLimit_RejectsWhenPerMinuteLimitExceeded(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: false}, &fakeDailyLimiter{count: 5}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRequireRateLimit_RejectsWhenDailyCapExceeded(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{count: 201}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRequireRateLimit_AllowsExactlyAtDailyCap(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{count: 200}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (count == cap is still allowed; cap+1 is what's rejected)", rec.Code, http.StatusOK)
	}
}

func TestRequireRateLimit_AllowsWhenDailyIncrementErrors(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{err: errors.New("db down")}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (a tracking failure should not block the request)", rec.Code, http.StatusOK)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestRequireRateLimit -v`
Expected: FAIL — build fails, `RequireRateLimit` undefined.

- [ ] **Step 3: Write the implementation**

Create `internal/api/ratelimit.go`:

```go
package api

import (
	"context"
	"log"
	"net/http"
)

type MinuteLimiter interface {
	Allow(clientID string) bool
}

type DailyLimiter interface {
	IncrementDailyGenerateCount(ctx context.Context, clientID string) (int, error)
}

func RequireRateLimit(minuteLimiter MinuteLimiter, dailyLimiter DailyLimiter, dailyCap int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client, ok := ClientFromContext(r.Context())
			if !ok {
				writeJSONError(w, http.StatusInternalServerError, "internal error: client not found in context")
				return
			}

			if !minuteLimiter.Allow(client.ID) {
				writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded: too many requests per minute")
				return
			}

			count, err := dailyLimiter.IncrementDailyGenerateCount(r.Context(), client.ID)
			if err != nil {
				log.Printf("ratelimit: failed to increment daily count: %v", err)
			} else if count > dailyCap {
				writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded: too many requests per day")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/... -run TestRequireRateLimit -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/ratelimit.go internal/api/ratelimit_test.go
git commit -m "$(cat <<'EOF'
internal/api: add RequireRateLimit middleware (per-minute + daily cap)

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 11: `internal/config` rate limit settings

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.RateLimitPerMinute int`, `Config.RateLimitPerDay int` — consumed by Task 13 (`main.go`).

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go` (and add the two new env vars, initialized to `""`, inside `setRequiredEnv`):

```go
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")
	t.Setenv("VOYAGE_API_KEY", "test-voyage-key")
	t.Setenv("VOYAGE_MODEL", "")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_dummy")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_dummy")
	t.Setenv("STRIPE_METERED_PRICE_ID", "price_dummy")
	t.Setenv("RESEND_API_KEY", "re_dummy")
	t.Setenv("RESEND_FROM_EMAIL", "")
	t.Setenv("PUBLIC_BASE_URL", "http://localhost:8081")
	t.Setenv("RATE_LIMIT_PER_MINUTE", "")
	t.Setenv("RATE_LIMIT_PER_DAY", "")
}
```

```go
func TestLoad_UsesDefaultRateLimitPerMinuteWhenUnset(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.RateLimitPerMinute != 10 {
		t.Errorf("RateLimitPerMinute = %d, want 10", cfg.RateLimitPerMinute)
	}
}

func TestLoad_ReadsCustomRateLimitPerMinute(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RATE_LIMIT_PER_MINUTE", "20")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.RateLimitPerMinute != 20 {
		t.Errorf("RateLimitPerMinute = %d, want 20", cfg.RateLimitPerMinute)
	}
}

func TestLoad_ErrorsWhenRateLimitPerMinuteInvalid(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RATE_LIMIT_PER_MINUTE", "not-a-number")

	_, err := Load()
	if !errors.Is(err, ErrInvalidRateLimitPerMinute) {
		t.Errorf("err = %v, want ErrInvalidRateLimitPerMinute", err)
	}
}

func TestLoad_UsesDefaultRateLimitPerDayWhenUnset(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.RateLimitPerDay != 200 {
		t.Errorf("RateLimitPerDay = %d, want 200", cfg.RateLimitPerDay)
	}
}

func TestLoad_ReadsCustomRateLimitPerDay(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RATE_LIMIT_PER_DAY", "500")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.RateLimitPerDay != 500 {
		t.Errorf("RateLimitPerDay = %d, want 500", cfg.RateLimitPerDay)
	}
}

func TestLoad_ErrorsWhenRateLimitPerDayInvalid(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RATE_LIMIT_PER_DAY", "not-a-number")

	_, err := Load()
	if !errors.Is(err, ErrInvalidRateLimitPerDay) {
		t.Errorf("err = %v, want ErrInvalidRateLimitPerDay", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL — build fails (`RateLimitPerMinute`, `RateLimitPerDay`, `ErrInvalidRateLimitPerMinute`, `ErrInvalidRateLimitPerDay` undefined).

- [ ] **Step 3: Update the implementation**

In `internal/config/config.go`:

```go
type Config struct {
	Port                 string
	DatabaseURL          string
	YouTubeAPIKey        string
	YouTubeDailyQuotaCap int
	StyleCacheTTLHours   int
	VoyageAPIKey         string
	VoyageModel          string
	AnthropicAPIKey      string
	AnthropicModel       string
	StripeSecretKey      string
	StripeWebhookSecret  string
	StripeMeteredPriceID string
	ResendAPIKey         string
	ResendFromEmail      string
	PublicBaseURL        string
	RateLimitPerMinute   int
	RateLimitPerDay      int
}

var (
	ErrMissingDatabaseURL          = errors.New("config: DATABASE_URL is required")
	ErrMissingYouTubeAPIKey        = errors.New("config: YOUTUBE_API_KEY is required")
	ErrInvalidQuotaCap             = errors.New("config: YOUTUBE_DAILY_QUOTA_CAP must be a positive integer")
	ErrInvalidStyleCacheTTL        = errors.New("config: STYLE_CACHE_TTL_HOURS must be a positive integer")
	ErrMissingVoyageAPIKey         = errors.New("config: VOYAGE_API_KEY is required")
	ErrMissingAnthropicAPIKey      = errors.New("config: ANTHROPIC_API_KEY is required")
	ErrMissingStripeSecretKey      = errors.New("config: STRIPE_SECRET_KEY is required")
	ErrMissingStripeWebhookSecret  = errors.New("config: STRIPE_WEBHOOK_SECRET is required")
	ErrMissingStripeMeteredPriceID = errors.New("config: STRIPE_METERED_PRICE_ID is required")
	ErrMissingResendAPIKey         = errors.New("config: RESEND_API_KEY is required")
	ErrMissingPublicBaseURL        = errors.New("config: PUBLIC_BASE_URL is required")
	ErrInvalidRateLimitPerMinute   = errors.New("config: RATE_LIMIT_PER_MINUTE must be a positive integer")
	ErrInvalidRateLimitPerDay      = errors.New("config: RATE_LIMIT_PER_DAY must be a positive integer")
)

const (
	defaultYouTubeDailyQuotaCap = 9000
	defaultStyleCacheTTLHours   = 48
	defaultVoyageModel          = "voyage-3.5-lite"
	defaultAnthropicModel       = "claude-sonnet-5"
	defaultResendFromEmail      = "onboarding@resend.dev"
	defaultRateLimitPerMinute   = 10
	defaultRateLimitPerDay      = 200
)
```

Then, at the end of `Load()`, right before `return cfg, nil`:

```go
	cfg.RateLimitPerMinute = defaultRateLimitPerMinute
	if raw := os.Getenv("RATE_LIMIT_PER_MINUTE"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return Config{}, ErrInvalidRateLimitPerMinute
		}
		cfg.RateLimitPerMinute = limit
	}

	cfg.RateLimitPerDay = defaultRateLimitPerDay
	if raw := os.Getenv("RATE_LIMIT_PER_DAY"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return Config{}, ErrInvalidRateLimitPerDay
		}
		cfg.RateLimitPerDay = limit
	}

	return cfg, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "$(cat <<'EOF'
internal/config: add RATE_LIMIT_PER_MINUTE and RATE_LIMIT_PER_DAY

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 12: Wire cost recording and rate limiting into `handleGenerate` and the router

**Files:**
- Modify: `internal/api/generate.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`

**Interfaces:**
- Consumes: `pricing.ClaudeCost`/`EmbeddingCost` (Task 1), `generation.Output.Usage` (Task 5), `storage.UsageUpdate` (Task 6), `api.RequestIDFromContext` (Task 9), `api.MinuteLimiter`/`DailyLimiter`/`RequireRateLimit` (Task 10), `Config.RateLimitPerDay` (Task 11, used by Task 13).
- Produces: `Dependencies.RateLimiter`, `Dependencies.DailyUsageLimiter`, `Dependencies.RateLimitPerDay` fields — consumed by Task 13 (`main.go`).

- [ ] **Step 1: Update `router_test.go`**

Four existing tests reach `handleGenerate` past `RequireAPIKey` and will now also hit `RequireRateLimit` — they need rate-limit fakes added so the middleware doesn't nil-panic. Update each of these to add the three new fields (`RateLimiter: &fakeMinuteLimiter{allow: true}, DailyUsageLimiter: &fakeDailyLimiter{count: 1}, RateLimitPerDay: 200`) to their `Dependencies{...}` literal:

- `TestGenerate_ReturnsGeneratedContentWithValidKey`
- `TestGenerate_ReturnsBadRequestForMissingFields`
- `TestGenerate_ReportsUsageForClientWithStripeCustomerID`
- `TestGenerate_SkipsUsageReportingForClientWithoutStripeCustomerID`

For example, `TestGenerate_ReturnsGeneratedContentWithValidKey` becomes:

```go
	NewRouter(Dependencies{
		Finder: finder, Recorder: recorder, Generator: orchestrator,
		RateLimiter: &fakeMinuteLimiter{allow: true}, DailyUsageLimiter: &fakeDailyLimiter{count: 1}, RateLimitPerDay: 200,
	}).ServeHTTP(rec, req)
```

`TestGenerate_ReturnsUnauthorizedWithoutKey` needs no change — it's rejected by `RequireAPIKey` before `RequireRateLimit` ever runs.

Also widen `fakeGenerationOrchestrator` to count calls, so new tests can assert generation never ran when a request is rejected for rate limiting:

```go
type fakeGenerationOrchestrator struct {
	output generation.Output
	err    error
	calls  int
}

func (f *fakeGenerationOrchestrator) Generate(ctx context.Context, input generation.Input) (generation.Output, error) {
	f.calls++
	return f.output, f.err
}
```

Then append these new tests to `router_test.go`:

```go
func TestGenerate_ReturnsTooManyRequestsWhenPerMinuteLimitExceeded(t *testing.T) {
	validKey := "ytpub_generatekey4"
	client := storage.Client{ID: "client-4", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	orchestrator := &fakeGenerationOrchestrator{output: generation.Output{Title: "T"}}

	req := httptest.NewRequest(http.MethodPost, "/v1/generate", strings.NewReader(`{"channel_id":"UC1","topic":"t"}`))
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		Finder: finder, Recorder: &fakeUsageRecorder{}, Generator: orchestrator,
		RateLimiter: &fakeMinuteLimiter{allow: false}, DailyUsageLimiter: &fakeDailyLimiter{count: 1}, RateLimitPerDay: 200,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if orchestrator.calls != 0 {
		t.Errorf("orchestrator.calls = %d, want 0 (rejected before generation ran)", orchestrator.calls)
	}
}

func TestGenerate_ReturnsTooManyRequestsWhenDailyCapExceeded(t *testing.T) {
	validKey := "ytpub_generatekey5"
	client := storage.Client{ID: "client-5", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	orchestrator := &fakeGenerationOrchestrator{output: generation.Output{Title: "T"}}

	req := httptest.NewRequest(http.MethodPost, "/v1/generate", strings.NewReader(`{"channel_id":"UC1","topic":"t"}`))
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		Finder: finder, Recorder: &fakeUsageRecorder{}, Generator: orchestrator,
		RateLimiter: &fakeMinuteLimiter{allow: true}, DailyUsageLimiter: &fakeDailyLimiter{count: 201}, RateLimitPerDay: 200,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if orchestrator.calls != 0 {
		t.Errorf("orchestrator.calls = %d, want 0 (rejected before generation ran)", orchestrator.calls)
	}
}

func TestGenerate_RecordsRealCostOnSuccess(t *testing.T) {
	validKey := "ytpub_generatekey6"
	client := storage.Client{ID: "client-6", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	recorder := &fakeUsageRecorder{}
	orchestrator := &fakeGenerationOrchestrator{output: generation.Output{
		Title: "T",
		Usage: generation.Usage{LLMInputTokens: 1_000_000, LLMOutputTokens: 1_000_000, EmbeddingCalls: 2, EmbeddingTokens: 1_000_000},
	}}

	req := httptest.NewRequest(http.MethodPost, "/v1/generate", strings.NewReader(`{"channel_id":"UC1","topic":"t"}`))
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		Finder: finder, Recorder: recorder, Generator: orchestrator,
		RateLimiter: &fakeMinuteLimiter{allow: true}, DailyUsageLimiter: &fakeDailyLimiter{count: 1}, RateLimitPerDay: 200,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(recorder.updatedEvents) != 1 {
		t.Fatalf("len(updatedEvents) = %d, want 1", len(recorder.updatedEvents))
	}
	update := recorder.updatedEvents[0].update
	if update.LLMInputTokens != 1_000_000 || update.LLMOutputTokens != 1_000_000 || update.EmbeddingCalls != 2 {
		t.Errorf("update = %+v, want tokens/calls to match the orchestrator's output usage", update)
	}
	// $2/MTok in + $10/MTok out + $0.02/MTok embedding, each at exactly 1M tokens.
	wantCost := 2.0 + 10.0 + 0.02
	if update.EstimatedCostUSD != wantCost {
		t.Errorf("EstimatedCostUSD = %v, want %v", update.EstimatedCostUSD, wantCost)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -v`
Expected: FAIL — build fails (`Dependencies.RateLimiter` etc. undefined; `handleGenerate` doesn't yet take a recorder).

- [ ] **Step 3: Update `generate.go`**

```go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
	"github.com/juansecalvinio/ytpublisher-api/internal/pricing"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type GenerationOrchestrator interface {
	Generate(ctx context.Context, input generation.Input) (generation.Output, error)
}

type UsageReporter interface {
	ReportUsage(ctx context.Context, stripeCustomerID string) error
}

type generateRequest struct {
	ChannelID string   `json:"channel_id"`
	Topic     string   `json:"topic"`
	Notes     string   `json:"notes"`
	Language  string   `json:"language"`
	Links     []string `json:"links"`
	Mentions  []string `json:"mentions"`
	Tone      string   `json:"tone"`
}

func handleGenerate(orchestrator GenerationOrchestrator, reporter UsageReporter, recorder UsageRecorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.ChannelID == "" || req.Topic == "" {
			writeJSONError(w, http.StatusBadRequest, "channel_id and topic are required")
			return
		}

		output, err := orchestrator.Generate(r.Context(), generation.Input{
			ChannelID: req.ChannelID,
			Topic:     req.Topic,
			Notes:     req.Notes,
			Language:  req.Language,
			Links:     req.Links,
			Mentions:  req.Mentions,
			Tone:      req.Tone,
		})

		// Record whatever usage actually happened, even on failure — a
		// request that burned tokens before erroring still has a real cost.
		if requestID, ok := RequestIDFromContext(r.Context()); ok {
			cost := pricing.ClaudeCost(output.Usage.LLMInputTokens, output.Usage.LLMOutputTokens) +
				pricing.EmbeddingCost(output.Usage.EmbeddingTokens)
			if updateErr := recorder.UpdateUsageEvent(r.Context(), requestID, storage.UsageUpdate{
				LLMInputTokens:   output.Usage.LLMInputTokens,
				LLMOutputTokens:  output.Usage.LLMOutputTokens,
				EmbeddingCalls:   output.Usage.EmbeddingCalls,
				EstimatedCostUSD: cost,
			}); updateErr != nil {
				log.Printf("generate: failed to update usage event %s: %v", requestID, updateErr)
			}
		}

		if err != nil {
			log.Printf("generate: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to generate content")
			return
		}

		if client, ok := ClientFromContext(r.Context()); ok && client.StripeCustomerID != "" {
			if err := reporter.ReportUsage(r.Context(), client.StripeCustomerID); err != nil {
				// The customer already has their content; a billing report
				// failure shouldn't turn into a failed request for them.
				log.Printf("generate: failed to report usage for %s: %v", client.StripeCustomerID, err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"title":          output.Title,
			"description":    output.Description,
			"tags":           output.Tags,
			"related_videos": output.RelatedVideos,
			"warnings":       output.Warnings,
		})
	}
}
```

- [ ] **Step 4: Update `router.go`**

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Dependencies struct {
	Finder               ClientFinder
	Recorder             UsageRecorder
	Syncer               ChannelSyncer
	StyleProvider        StyleProvider
	RelatedVideos        RelatedVideosProvider
	Generator            GenerationOrchestrator
	UsageReporter        UsageReporter
	RateLimiter          MinuteLimiter
	DailyUsageLimiter    DailyLimiter
	RateLimitPerDay      int
	CheckoutCreator      CheckoutSessionCreator
	ClientProvisioner    ClientProvisioner
	KeyMailer            KeyMailer
	StripeMeteredPriceID string
	StripeWebhookSecret  string
	BillingSuccessURL    string
	BillingCancelURL     string
}

func NewRouter(deps Dependencies) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)
	r.Get("/v1/billing/signup", handleBillingSignup(deps.CheckoutCreator, deps.StripeMeteredPriceID, deps.BillingSuccessURL, deps.BillingCancelURL))
	r.Get("/v1/billing/success", handleBillingSuccess)
	r.Get("/v1/billing/cancel", handleBillingCancel)
	r.Post("/v1/stripe/webhook", handleStripeWebhook(deps.ClientProvisioner, deps.KeyMailer, deps.StripeWebhookSecret))

	r.Group(func(r chi.Router) {
		r.Use(RequireAPIKey(deps.Finder, deps.Recorder))
		r.Get("/v1/whoami", handleWhoami)
		r.Post("/v1/internal/channels/{channelID}/sync", handleChannelSync(deps.Syncer))
		r.Get("/v1/internal/channels/{channelID}/style", handleChannelStyle(deps.StyleProvider))
		r.Get("/v1/internal/channels/{channelID}/related-videos", handleRelatedVideos(deps.RelatedVideos))
		r.With(RequireRateLimit(deps.RateLimiter, deps.DailyUsageLimiter, deps.RateLimitPerDay)).
			Post("/v1/generate", handleGenerate(deps.Generator, deps.UsageReporter, deps.Recorder))
	})

	return r
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleWhoami(w http.ResponseWriter, r *http.Request) {
	client, ok := ClientFromContext(r.Context())
	if !ok {
		http.Error(w, "internal error: client not found in context", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"client_id": client.ID,
		"name":      client.Name,
	})
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api/generate.go internal/api/router.go internal/api/router_test.go
git commit -m "$(cat <<'EOF'
internal/api: record real generation cost and enforce rate limits

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 13: Wire everything into `cmd/api/main.go`

**Files:**
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: `ratelimit.NewLimiter` (Task 8), `store.IncrementDailyGenerateCount` (Task 7), `cfg.RateLimitPerMinute`/`cfg.RateLimitPerDay` (Task 11), `api.Dependencies.RateLimiter`/`DailyUsageLimiter`/`RateLimitPerDay` (Task 12).

- [ ] **Step 1: Update the implementation**

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/joho/godotenv"
	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/billing"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/claude"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/email"
	"github.com/juansecalvinio/ytpublisher-api/internal/embeddings"
	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
	"github.com/juansecalvinio/ytpublisher-api/internal/ratelimit"
	"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/stylecache"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

const maxVideosPerChannel = 25

func main() {
	// .env is only present in local dev; in production the real
	// environment variables are set directly (systemd), so a missing
	// file here is expected and not an error.
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := storage.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer pool.Close()
	log.Println("connected to database")

	store := storage.NewStore(pool)

	youtubeClient, err := youtube.NewClient(ctx, cfg.YouTubeAPIKey)
	if err != nil {
		log.Fatalf("youtube: %v", err)
	}
	syncer := channelsync.NewSyncer(youtubeClient, store, store, maxVideosPerChannel, cfg.YouTubeDailyQuotaCap)

	styleTTL := time.Duration(cfg.StyleCacheTTLHours) * time.Hour
	styleProvider := stylecache.NewProvider(store, store, styleTTL)

	embeddingsClient := embeddings.NewClient(cfg.VoyageAPIKey, cfg.VoyageModel)
	relatedVideosProvider := relatedvideos.NewProvider(store, embeddingsClient)

	claudeClient := claude.NewClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
	generator := generation.NewOrchestrator(styleProvider, relatedVideosProvider, claudeClient)

	billingClient := billing.NewClient(cfg.StripeSecretKey)
	emailClient := email.NewClient(cfg.ResendAPIKey, cfg.ResendFromEmail)

	rateLimiter := ratelimit.NewLimiter(cfg.RateLimitPerMinute, time.Minute)

	router := api.NewRouter(api.Dependencies{
		Finder:               store,
		Recorder:             store,
		Syncer:               syncer,
		StyleProvider:        styleProvider,
		RelatedVideos:        relatedVideosProvider,
		Generator:            generator,
		CheckoutCreator:      billingClient,
		ClientProvisioner:    store,
		KeyMailer:            emailClient,
		UsageReporter:        billingClient,
		RateLimiter:          rateLimiter,
		DailyUsageLimiter:    store,
		RateLimitPerDay:      cfg.RateLimitPerDay,
		StripeMeteredPriceID: cfg.StripeMeteredPriceID,
		StripeWebhookSecret:  cfg.StripeWebhookSecret,
		BillingSuccessURL:    cfg.PublicBaseURL + "/v1/billing/success",
		BillingCancelURL:     cfg.PublicBaseURL + "/v1/billing/cancel",
	})

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: succeeds with no errors — this is the point where every package's interfaces finally line up end to end.

- [ ] **Step 3: Commit**

```bash
git add cmd/api/main.go
git commit -m "$(cat <<'EOF'
cmd/api: wire the rate limiter and daily cap into the server

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01LtEmxcWCJrb2fsoggDZ1UT
EOF
)"
```

---

### Task 14: Full-suite verification

**Files:** none (verification only).

- [ ] **Step 1: Run the full test suite**

Run: `DATABASE_URL=<test db> ANTHROPIC_API_KEY=<key> VOYAGE_API_KEY=<key> go test ./...`
Expected: PASS across all packages (integration tests against Postgres/Anthropic/Voyage run for real if the env vars are set; otherwise they SKIP cleanly, which is also acceptable per this project's existing convention).

- [ ] **Step 2: Manually verify against a running server (recommended, matches Session 8's practice)**

Start the server locally and:
1. Call `/v1/generate` with a valid key and confirm the `usage_events` row for that request has nonzero `llm_input_tokens`, `llm_output_tokens`, `embedding_calls`, and `estimated_cost_usd` after the response returns.
2. Call `/v1/generate` more than `RATE_LIMIT_PER_MINUTE` times in under a minute with the same key and confirm the extra calls get `429` with the per-minute message, and that no new `usage_events` rows appear for the rejected calls.
3. (Optional, slower) Manually set `client_daily_usage.request_count` for a test client above `RATE_LIMIT_PER_DAY` and confirm the next `/v1/generate` call gets `429` with the per-day message.

- [ ] **Step 3: Report and hand off**

Once verification passes, this plan's work is complete. The next step is the `finishing-a-development-branch` skill (run tests, present the merge/PR/keep menu) — not part of this plan, invoked separately per the standing workflow.
