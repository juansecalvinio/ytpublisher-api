# Session 9: Real Cost Tracking and Rate Limiting for /v1/generate

## Context

`usage_events` already has columns for cost/usage data (`youtube_units_used`,
`embedding_calls`, `llm_input_tokens`, `llm_output_tokens`,
`estimated_cost_usd`), but `storage.InsertUsageEvent` only ever writes
`client_id`, `request_id`, and `endpoint` — every cost column is left at its
zero default. Separately, `/v1/generate` has no protection against a client
hammering it: no rate limiting exists anywhere in the codebase today (only
false-positive hits on the substring "generate" turn up when grepping for
"rate").

This session closes both gaps for `/v1/generate` only (the only endpoint
that spends AI tokens): it records the real cost of each request, and it
stops a client from running up unbounded cost via request volume.

## Goals

- After every `/v1/generate` call, `usage_events` holds the real Claude
  token counts, real embedding token count (converted to a call count),
  and a computed `estimated_cost_usd` for that request.
- A client is capped at a configurable number of `/v1/generate` requests
  per minute and per day. Exceeding either returns `429` before any Claude
  or Voyage API call is made (so no cost is incurred on a rejected
  request).
- No new tables for cost tracking (the schema already supports it). One
  new table for the daily rate-limit cap, mirroring the existing
  `youtube_quota_usage` pattern.
- Pricing is hardcoded Go constants, not environment configuration. Rate
  limit thresholds ARE environment configuration (mirroring
  `YOUTUBE_DAILY_QUOTA_CAP`'s existing pattern), defaulting to 10
  requests/minute and 200 requests/day per client.

## Non-goals

- Cost tracking or rate limiting on any endpoint other than `/v1/generate`.
- Per-client custom rate limits (e.g. higher limits for paying customers)
  — out of scope for this session, all clients share the same two limits.
- Distributed/multi-instance rate limiting. The per-minute counter is
  in-memory in this process, which is correct only because the API
  currently runs as a single systemd process with no horizontal scaling.
  If that changes, the per-minute limiter needs to move to a shared store
  (e.g. Redis) — noted here so it isn't forgotten, not solved now.
- Reacting to Stripe plan/tier when picking rate limits — the cap is flat
  across all clients regardless of billing plan.

## Architecture Overview

Two independent pieces land in the same session because they share a
motivating question ("what happens if the cost data path is exploited by
volume"), but they touch different layers and can be built/tested in
either order.

**Cost tracking** flows through the existing request lifecycle rather than
changing it: `RequireAPIKey` middleware already generates a `request_id`
and inserts a zero-cost `usage_events` row before the handler runs (cost
data isn't known until deep inside the generation call chain, after one or
more Claude and Voyage calls complete). The middleware now also stores
that `request_id` in the request context. `handleGenerate` retrieves it,
sums the usage from every Claude/Voyage call the orchestrator made, prices
it via `internal/pricing`, and issues one `UPDATE usage_events SET ...
WHERE request_id = $1` after the handler's real work is done — success or
failure. This keeps `RequireAPIKey` endpoint-agnostic (it still doesn't
know or care that `/v1/generate` is special) while letting the one
endpoint that spends tokens attach its real numbers afterward.

**Rate limiting** sits in front of that entirely: a new middleware,
mounted only on the `/v1/generate` route (after `RequireAPIKey`, since it
needs the authenticated client), checks a per-minute in-memory counter and
then a per-day Postgres-backed counter. Either limit being exceeded short-
circuits the request with `429` before `RequireAPIKey`'s usage-event
insert and before any Claude/Voyage call — a rejected request costs
nothing and leaves no `usage_events` row.

## Data Flow (cost tracking)

1. `RequireAPIKey` generates `request_id`, inserts the zero-cost
   `usage_events` row (unchanged), and stores `request_id` in the request
   context (new).
2. `handleGenerate` calls the orchestrator as today.
3. The orchestrator's `Generate` now returns usage totals alongside
   `Output`: total Claude input/output tokens across every `Generate`
   call (1 initial + up to 2 retries) and the `Repair` call if it ran,
   plus the embedding side split into two numbers — the number of Voyage
   API calls made (1 if only `EmbedQuery` ran, 2 if `EmbedDocuments` also
   ran; this is what `usage_events.embedding_calls` stores) and the total
   embedding tokens across those calls (used only for pricing — there is
   no column to persist this separately, it's folded into
   `estimated_cost_usd`).
4. `handleGenerate` computes `estimated_cost_usd` via
   `pricing.ClaudeCost(inputTokens, outputTokens) +
   pricing.EmbeddingCost(embeddingTokens)` and calls
   `storage.UpdateUsageEvent` with the request ID from context and four
   fields: `llm_input_tokens`, `llm_output_tokens`, `embedding_calls`
   (the call count, not tokens), and `estimated_cost_usd`.
5. If generation fails partway (e.g. Claude errors on the final retry),
   whatever usage was accumulated up to that point is still summed and
   written — a failed request that burned tokens still shows a nonzero
   cost. If zero calls were made before the failure, the row is left at
   its zero defaults (no update needed).

## Interface Changes

```go
// internal/claude/client.go
type Usage struct {
    InputTokens  int64
    OutputTokens int64
}

func (c *Client) Generate(ctx context.Context, input GenerateInput) (ContentDraft, Usage, error)
func (c *Client) Repair(ctx context.Context, draft ContentDraft, violations []rules.Violation) (ContentDraft, Usage, error)
```

```go
// internal/embeddings/client.go
type Usage struct {
    TotalTokens int64
}

func (c *Client) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, Usage, error)
func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, Usage, error)
```

```go
// internal/relatedvideos/provider.go
type Usage struct {
    EmbeddingCalls  int   // number of Voyage API calls made (1 or 2)
    EmbeddingTokens int64 // summed total_tokens across those calls, for pricing only
}

func (p *Provider) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, Usage, error)
```

```go
// internal/generation/orchestrator.go
type ContentGenerator interface {
    Generate(ctx context.Context, input claude.GenerateInput) (claude.ContentDraft, claude.Usage, error)
    Repair(ctx context.Context, draft claude.ContentDraft, violations []rules.Violation) (claude.ContentDraft, claude.Usage, error)
}

type RelatedVideosFinder interface {
    FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, relatedvideos.Usage, error)
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
```

The orchestrator accumulates `Usage` across every `Generate`/`Repair` call
inside `Generate()`'s existing retry loop (no new control flow — the loop
already exists, it just needs to add each call's tokens to a running
total before checking `needsRetry`).

```go
// internal/pricing/pricing.go
const (
    claudeInputCostPerMTok  = 2.0  // Claude Sonnet 5, per platform.claude.com/docs pricing
    claudeOutputCostPerMTok = 10.0
    voyageCostPerMTok       = 0.02 // Voyage 3.5 Lite, per docs.voyageai.com/docs/pricing
)

func ClaudeCost(inputTokens, outputTokens int64) float64
func EmbeddingCost(totalTokens int64) float64
```

```go
// internal/storage/usage.go
type UsageUpdate struct {
    LLMInputTokens  int64
    LLMOutputTokens int64
    EmbeddingCalls  int
    EstimatedCostUSD float64
}

func (s *Store) UpdateUsageEvent(ctx context.Context, requestID string, update UsageUpdate) error
```

```go
// internal/api/middleware.go
func RequestIDFromContext(ctx context.Context) (string, bool)
// requestID is stored via context.WithValue alongside the existing
// clientContextKey pattern, using a second unexported context key.
```

## Rate Limiting Design

**Per-minute burst limit** (in-memory): `internal/ratelimit.Limiter` holds
a `map[clientID]*window` behind a mutex, where each `window` is a slice of
recent request timestamps (or an equivalent fixed-window counter reset
every 60s — implementer's choice at plan time, functionally identical for
this scale). `Allow(clientID string) bool` returns whether the request is
within the configured per-minute limit, recording the attempt as a side
effect only when it's within the client provisioning already established.
No persistence: a process restart resets everyone's burst window, which is
acceptable since it can only ever make the limit momentarily more
permissive, never less.

**Per-day cap** (Postgres): new table `client_daily_usage`, keyed by
`(client_id, date)`, incremented via an atomic upsert:

```sql
CREATE TABLE client_daily_usage (
    client_id UUID NOT NULL REFERENCES api_clients(id),
    date DATE NOT NULL,
    request_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (client_id, date)
);
```

```sql
INSERT INTO client_daily_usage (client_id, date, request_count)
VALUES ($1, CURRENT_DATE, 1)
ON CONFLICT (client_id, date)
DO UPDATE SET request_count = client_daily_usage.request_count + 1
RETURNING request_count;
```

This mirrors `youtube_quota_usage`'s check-then-increment shape but scoped
per client instead of globally, and made atomic via `ON CONFLICT ...
RETURNING` (a single round trip, no read-then-write race).

**Middleware composition**: a new `RequireRateLimit(limiter
*ratelimit.Limiter, store *storage.Store)` middleware wraps only the
`/v1/generate` route, mounted after `RequireAPIKey` in the chain (so
`ClientFromContext` is populated). It checks the in-memory per-minute
limiter first (cheap, no DB round trip) — if exceeded, respond `429`
immediately. Otherwise it performs the daily upsert — if the returned
count exceeds the daily cap, respond `429` (the increment still happened;
being over-cap for the rest of the day is the intended behavior, not a
bug — it avoids a second query to check-before-increment).

**Config**: two new env vars following the existing
`YOUTUBE_DAILY_QUOTA_CAP` pattern — `RATE_LIMIT_PER_MINUTE` (default 10)
and `RATE_LIMIT_PER_DAY` (default 200) — parsed in `internal/config` the
same way the existing quota cap is.

**Response shape**: `429` with the same JSON error envelope
`writeJSONError` already produces elsewhere (`{"error": "..."}`), message
distinguishing which limit was hit (`"rate limit exceeded: too many
requests per minute"` vs `"... per day"`) so clients can tell burst
throttling from a daily cutoff.

## Error Handling

Consistent with the existing `RequireAPIKey` pattern (where a failed
`InsertUsageEvent` is logged but never blocks the request): a failed
`UpdateUsageEvent` or a failed daily-cap upsert is logged and does not
fail the client-facing request. Accounting/tracking failures are
observability problems, not reasons to break `/v1/generate` for a
paying client. The only condition that produces a `429` is an actual,
successfully-read rate limit being exceeded.

## Testing Strategy

- `internal/pricing`: pure unit tests, no mocks — table-driven cases over
  token counts including zero.
- `internal/ratelimit`: unit tests using short, configurable windows
  (milliseconds, not real minutes) to verify burst behavior without
  sleeping in real time.
- `client_daily_usage` upsert: integration test against real Postgres
  (matching this project's existing convention of testing storage against
  a real test database), verifying concurrent increments land correctly
  and the cap comparison is correct at the boundary (exactly at cap vs.
  one over).
- End-to-end cost calculation: a fake `ContentGenerator` and
  `RelatedVideosFinder` returning known `Usage` values, asserting the
  exact `estimated_cost_usd` written to `usage_events` after a
  `/v1/generate` call, including a case with a retry (two `Generate`
  calls) to confirm accumulation, not overwrite.
- Rate limit integration test: exceed the per-minute limit and confirm
  `429` with no `usage_events` row created for the rejected request;
  separately, exceed the per-day cap and confirm the same.

## Migration

One new migration: `migrations/00005_create_client_daily_usage.sql`,
containing the `CREATE TABLE` above. No changes to `usage_events` — its
existing columns already cover the cost-tracking half of this work.
