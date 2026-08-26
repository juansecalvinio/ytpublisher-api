# Session 3: YouTube Data API Client, Channel Video Cache, Quota Governance — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fetch and cache a channel's latest videos from the real YouTube Data API, governed by a daily quota cap, exposed through a new protected endpoint — building on Sessions 1-2 (config, storage, auth middleware).

**Architecture:** `internal/youtube` wraps the official Google API client behind a small `FetchLatestVideos` method returning both the videos and the exact quota units consumed (even on partial failure). `internal/channelsync` orchestrates the sync: check quota → fetch → record actual usage → upsert into `channel_videos`. It depends only on three small interfaces it defines itself (fetcher, quota tracker, video store), so it's unit-tested entirely with fakes — no network or database in those tests. `internal/api` gains a new protected endpoint that calls the syncer and maps its errors to HTTP status codes (404 channel not found, 429 quota exceeded).

**Tech Stack:** Adds `google.golang.org/api/youtube/v3` (official Google API client) as a new dependency. Everything else matches Sessions 1-2 (Go 1.22+, chi, pgx/v5, goose).

**Spec:** `docs/superpowers/specs/2026-08-24-ytpublisher-api-v1-design.md`

## Global Constraints

- Go version: 1.22+
- Module path: `github.com/juansecalvinio/ytpublisher-api`
- YouTube Data API: API key only (no OAuth), via `google.golang.org/api/youtube/v3` and `google.golang.org/api/option`
- Quota costs (verified against Google's published quota table): `channels.list` = 1 unit, `playlistItems.list` = 1 unit, `videos.list` = 1 unit — a full channel sync costs 3 units in the happy path
- Daily quota cap: configurable via `YOUTUBE_DAILY_QUOTA_CAP` env var, default 9000 (leaves a 1000-unit buffer under the real 10,000/day limit)
- Videos cached per channel per sync: 25 (fits in one page of both `playlistItems.list` and `videos.list`, no pagination needed this session)
- External dependencies are consumed by their callers through small interfaces defined by the consumer — same principle as Sessions 1-2's storage/auth design

---

## Prerequisites

- Sessions 1 and 2 merged (config, storage, auth middleware, `api_clients`/`usage_events` tables all present in `main`)
- `DATABASE_URL` exported in your shell (Supabase session pooler string, `sslmode=require`)
- `YOUTUBE_API_KEY` exported in your shell (from a Google Cloud project with YouTube Data API v3 enabled)
- `goose` available at `~/go/bin/goose`

---

### Task 1: Migrations — `channel_videos` and `youtube_quota_usage`

**Files:**
- Create: `migrations/00003_create_channel_videos.sql`
- Create: `migrations/00004_create_youtube_quota_usage.sql`

**Interfaces:**
- Produces: `channel_videos` table (PK `(channel_id, video_id)`), `youtube_quota_usage` table (PK `date`)

- [ ] **Step 1: Write the `channel_videos` migration**

```sql
-- migrations/00003_create_channel_videos.sql

-- +goose Up
CREATE TABLE channel_videos (
    channel_id TEXT NOT NULL,
    video_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    tags_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    published_at TIMESTAMPTZ NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (channel_id, video_id)
);

-- +goose Down
DROP TABLE channel_videos;
```

Note: the `embedding vector(1024)` column from the spec's original schema sketch is intentionally deferred to Session 5, when the embeddings feature that populates it actually exists — adding it now would mean enabling `pgvector` and carrying an unused column for two sessions.

- [ ] **Step 2: Write the `youtube_quota_usage` migration**

```sql
-- migrations/00004_create_youtube_quota_usage.sql

-- +goose Up
CREATE TABLE youtube_quota_usage (
    date DATE PRIMARY KEY,
    units_used INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE youtube_quota_usage;
```

- [ ] **Step 3: Apply both migrations**

```bash
~/go/bin/goose -dir migrations postgres "$DATABASE_URL" up
```

Expected: `OK   00003_create_channel_videos.sql` then `OK   00004_create_youtube_quota_usage.sql`

- [ ] **Step 4: Verify**

```bash
psql "$DATABASE_URL" -c "\d channel_videos"
psql "$DATABASE_URL" -c "\d youtube_quota_usage"
```

Expected: `channel_videos` shows the 6 columns with a composite primary key; `youtube_quota_usage` shows `date` (PK) and `units_used`.

- [ ] **Step 5: Commit**

```bash
git add migrations/00003_create_channel_videos.sql migrations/00004_create_youtube_quota_usage.sql
git commit -m "feat: add channel_videos and youtube_quota_usage migrations"
```

---

### Task 2: Config — YouTube API key and daily quota cap

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config` gains `YouTubeAPIKey string` and `YouTubeDailyQuotaCap int`; new sentinels `config.ErrMissingYouTubeAPIKey`, `config.ErrInvalidQuotaCap`

- [ ] **Step 1: Update the tests**

```go
// internal/config/config_test.go
package config

import (
	"errors"
	"testing"
)

func TestLoad_UsesDefaultPortWhenUnset(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
}

func TestLoad_ReadsCustomPort(t *testing.T) {
	t.Setenv("PORT", "9090")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
}

func TestLoad_ReadsDatabaseURL(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://user:pass@host:5432/db")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://user:pass@host:5432/db" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://user:pass@host:5432/db")
	}
}

func TestLoad_ErrorsWhenDatabaseURLMissing(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Errorf("err = %v, want ErrMissingDatabaseURL", err)
	}
}

func TestLoad_ErrorsWhenYouTubeAPIKeyMissing(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingYouTubeAPIKey) {
		t.Errorf("err = %v, want ErrMissingYouTubeAPIKey", err)
	}
}

func TestLoad_UsesDefaultQuotaCapWhenUnset(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.YouTubeDailyQuotaCap != 9000 {
		t.Errorf("YouTubeDailyQuotaCap = %d, want 9000", cfg.YouTubeDailyQuotaCap)
	}
}

func TestLoad_ReadsCustomQuotaCap(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "5000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.YouTubeDailyQuotaCap != 5000 {
		t.Errorf("YouTubeDailyQuotaCap = %d, want 5000", cfg.YouTubeDailyQuotaCap)
	}
}

func TestLoad_ErrorsWhenQuotaCapInvalid(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "not-a-number")

	_, err := Load()
	if !errors.Is(err, ErrInvalidQuotaCap) {
		t.Errorf("err = %v, want ErrInvalidQuotaCap", err)
	}
}
```

- [ ] **Step 2: Run tests to verify the new ones fail**

Run: `go test ./internal/config/...`
Expected: FAIL — compile error, `ErrMissingYouTubeAPIKey` and `ErrInvalidQuotaCap` undefined.

- [ ] **Step 3: Update `config.go`**

```go
// internal/config/config.go
package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	Port                 string
	DatabaseURL          string
	YouTubeAPIKey        string
	YouTubeDailyQuotaCap int
}

var (
	ErrMissingDatabaseURL   = errors.New("config: DATABASE_URL is required")
	ErrMissingYouTubeAPIKey = errors.New("config: YOUTUBE_API_KEY is required")
	ErrInvalidQuotaCap      = errors.New("config: YOUTUBE_DAILY_QUOTA_CAP must be a positive integer")
)

const defaultYouTubeDailyQuotaCap = 9000

func Load() (Config, error) {
	cfg := Config{
		Port:          os.Getenv("PORT"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		YouTubeAPIKey: os.Getenv("YOUTUBE_API_KEY"),
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.DatabaseURL == "" {
		return Config{}, ErrMissingDatabaseURL
	}
	if cfg.YouTubeAPIKey == "" {
		return Config{}, ErrMissingYouTubeAPIKey
	}

	cfg.YouTubeDailyQuotaCap = defaultYouTubeDailyQuotaCap
	if raw := os.Getenv("YOUTUBE_DAILY_QUOTA_CAP"); raw != "" {
		cap, err := strconv.Atoi(raw)
		if err != nil || cap <= 0 {
			return Config{}, ErrInvalidQuotaCap
		}
		cfg.YouTubeDailyQuotaCap = cap
	}

	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: all 8 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: add YouTube API key and daily quota cap to config"
```

---

### Task 3: Storage — channel video cache

**Files:**
- Create: `internal/storage/channel_video.go`
- Test: `internal/storage/channel_video_test.go`

**Interfaces:**
- Produces: `storage.ChannelVideo{ChannelID, VideoID, Title, Description string; Tags []string; PublishedAt time.Time}`, `(*Store) UpsertChannelVideos(ctx, channelID string, videos []ChannelVideo) error`, `(*Store) ListChannelVideos(ctx, channelID string) ([]ChannelVideo, error)`, `(*Store) DeleteChannelVideos(ctx, channelID string) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/storage/channel_video_test.go
package storage

import (
	"context"
	"testing"
	"time"
)

func TestUpsertChannelVideos_ThenListChannelVideos(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	videos := []ChannelVideo{
		{
			ChannelID:   channelID,
			VideoID:     "v1",
			Title:       "First",
			Description: "desc1",
			Tags:        []string{"go", "test"},
			PublishedAt: time.Now().Truncate(time.Second),
		},
	}

	if err := store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		t.Fatalf("UpsertChannelVideos() returned unexpected error: %v", err)
	}

	found, err := store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() returned unexpected error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len(found) = %d, want 1", len(found))
	}
	if found[0].Title != "First" {
		t.Errorf("Title = %q, want %q", found[0].Title, "First")
	}
	if len(found[0].Tags) != 2 || found[0].Tags[0] != "go" {
		t.Errorf("Tags = %v, want [go test]", found[0].Tags)
	}
}

func TestUpsertChannelVideos_UpdatesOnConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	first := []ChannelVideo{{ChannelID: channelID, VideoID: "v1", Title: "Old Title", PublishedAt: time.Now().Truncate(time.Second)}}
	if err := store.UpsertChannelVideos(ctx, channelID, first); err != nil {
		t.Fatalf("UpsertChannelVideos() (first) returned unexpected error: %v", err)
	}

	second := []ChannelVideo{{ChannelID: channelID, VideoID: "v1", Title: "New Title", PublishedAt: time.Now().Truncate(time.Second)}}
	if err := store.UpsertChannelVideos(ctx, channelID, second); err != nil {
		t.Fatalf("UpsertChannelVideos() (second) returned unexpected error: %v", err)
	}

	found, err := store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() returned unexpected error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len(found) = %d, want 1 (update, not duplicate)", len(found))
	}
	if found[0].Title != "New Title" {
		t.Errorf("Title = %q, want %q", found[0].Title, "New Title")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/... -run TestUpsertChannelVideos`
Expected: FAIL — compile error, `ChannelVideo`, `UpsertChannelVideos`, `ListChannelVideos`, `DeleteChannelVideos` undefined.

- [ ] **Step 3: Write `channel_video.go`**

```go
// internal/storage/channel_video.go
package storage

import (
	"context"
	"encoding/json"
	"time"
)

type ChannelVideo struct {
	ChannelID   string
	VideoID     string
	Title       string
	Description string
	Tags        []string
	PublishedAt time.Time
}

func (s *Store) UpsertChannelVideos(ctx context.Context, channelID string, videos []ChannelVideo) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, v := range videos {
		tagsJSON, err := json.Marshal(v.Tags)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO channel_videos (channel_id, video_id, title, description, tags_json, published_at, fetched_at)
			 VALUES ($1, $2, $3, $4, $5, $6, now())
			 ON CONFLICT (channel_id, video_id) DO UPDATE SET
				title = EXCLUDED.title,
				description = EXCLUDED.description,
				tags_json = EXCLUDED.tags_json,
				published_at = EXCLUDED.published_at,
				fetched_at = now()`,
			channelID, v.VideoID, v.Title, v.Description, tagsJSON, v.PublishedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) ListChannelVideos(ctx context.Context, channelID string) ([]ChannelVideo, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT video_id, title, description, tags_json, published_at
		 FROM channel_videos WHERE channel_id = $1 ORDER BY published_at DESC`,
		channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []ChannelVideo
	for rows.Next() {
		var v ChannelVideo
		var tagsJSON []byte
		if err := rows.Scan(&v.VideoID, &v.Title, &v.Description, &tagsJSON, &v.PublishedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tagsJSON, &v.Tags); err != nil {
			return nil, err
		}
		v.ChannelID = channelID
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

func (s *Store) DeleteChannelVideos(ctx context.Context, channelID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM channel_videos WHERE channel_id = $1`, channelID)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/... -run TestUpsertChannelVideos -v`
Expected: both tests PASS against the real Supabase database

- [ ] **Step 5: Commit**

```bash
git add internal/storage/channel_video.go internal/storage/channel_video_test.go
git commit -m "feat: add channel video cache storage"
```

---

### Task 4: Storage — YouTube quota tracking

**Files:**
- Create: `internal/storage/quota.go`
- Test: `internal/storage/quota_test.go`

**Interfaces:**
- Produces: `(*Store) UnitsUsedToday(ctx) (int, error)`, `(*Store) IncrementUnitsUsed(ctx, units int) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/storage/quota_test.go
package storage

import (
	"context"
	"testing"
)

func TestUnitsUsedToday_ReturnsNonNegativeBaseline(t *testing.T) {
	store := newTestStore(t)

	units, err := store.UnitsUsedToday(context.Background())
	if err != nil {
		t.Fatalf("UnitsUsedToday() returned unexpected error: %v", err)
	}
	if units < 0 {
		t.Errorf("UnitsUsedToday() = %d, want >= 0", units)
	}
}

// This test intentionally does not roll back its increment: today's quota
// counter is shared, cumulative state (there is no meaningful "undo" for a
// usage counter without risking corrupting real same-day usage). A few
// units added by test runs is negligible against a 9000+ unit daily cap.
func TestIncrementUnitsUsed_AccumulatesAcrossCalls(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	before, err := store.UnitsUsedToday(ctx)
	if err != nil {
		t.Fatalf("UnitsUsedToday() returned unexpected error: %v", err)
	}

	if err := store.IncrementUnitsUsed(ctx, 3); err != nil {
		t.Fatalf("IncrementUnitsUsed() returned unexpected error: %v", err)
	}

	after, err := store.UnitsUsedToday(ctx)
	if err != nil {
		t.Fatalf("UnitsUsedToday() (after) returned unexpected error: %v", err)
	}
	if after != before+3 {
		t.Errorf("after = %d, want %d", after, before+3)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/... -run 'TestUnitsUsedToday|TestIncrementUnitsUsed'`
Expected: FAIL — compile error, `UnitsUsedToday` and `IncrementUnitsUsed` undefined.

- [ ] **Step 3: Write `quota.go`**

```go
// internal/storage/quota.go
package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *Store) UnitsUsedToday(ctx context.Context) (int, error) {
	var units int
	err := s.pool.QueryRow(ctx,
		`SELECT units_used FROM youtube_quota_usage WHERE date = CURRENT_DATE`,
	).Scan(&units)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return units, nil
}

func (s *Store) IncrementUnitsUsed(ctx context.Context, units int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO youtube_quota_usage (date, units_used) VALUES (CURRENT_DATE, $1)
		 ON CONFLICT (date) DO UPDATE SET units_used = youtube_quota_usage.units_used + $1`,
		units,
	)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/... -run 'TestUnitsUsedToday|TestIncrementUnitsUsed' -v`
Expected: both tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/storage/quota.go internal/storage/quota_test.go
git commit -m "feat: add YouTube quota usage tracking"
```

---

### Task 5: YouTube Data API client

**Files:**
- Create: `internal/youtube/client.go`
- Test: `internal/youtube/client_test.go`

**Interfaces:**
- Produces: `youtube.Video{ID, Title, Description string; Tags []string; PublishedAt time.Time}`, `youtube.FetchResult{Videos []Video; QuotaUsed int}`, `youtube.ErrChannelNotFound`, `youtube.NewClient(ctx, apiKey string) (*Client, error)`, `(*Client) FetchLatestVideos(ctx, channelID string, maxResults int) (FetchResult, error)`

- [ ] **Step 1: Add the YouTube API dependency**

```bash
go get google.golang.org/api/youtube/v3
```

- [ ] **Step 2: Write the failing integration tests**

```go
// internal/youtube/client_test.go
package youtube

import (
	"context"
	"errors"
	"os"
	"testing"
)

// The official Google Developers channel — stable, public, used in Google's
// own API examples, so it won't disappear or go private under us.
const testChannelID = "UC_x5XG1OV2P6uZZ5FSM9Ttw"

func TestFetchLatestVideos_ReturnsRealVideos(t *testing.T) {
	apiKey := os.Getenv("YOUTUBE_API_KEY")
	if apiKey == "" {
		t.Skip("YOUTUBE_API_KEY not set; skipping integration test against the real YouTube Data API")
	}

	client, err := NewClient(context.Background(), apiKey)
	if err != nil {
		t.Fatalf("NewClient() returned unexpected error: %v", err)
	}

	result, err := client.FetchLatestVideos(context.Background(), testChannelID, 5)
	if err != nil {
		t.Fatalf("FetchLatestVideos() returned unexpected error: %v", err)
	}
	if result.QuotaUsed != 3 {
		t.Errorf("QuotaUsed = %d, want 3", result.QuotaUsed)
	}
	if len(result.Videos) == 0 {
		t.Fatal("expected at least one video, got none")
	}
	for _, v := range result.Videos {
		if v.ID == "" {
			t.Error("video has empty ID")
		}
		if v.Title == "" {
			t.Error("video has empty Title")
		}
	}
}

func TestFetchLatestVideos_ReturnsErrChannelNotFoundForInvalidID(t *testing.T) {
	apiKey := os.Getenv("YOUTUBE_API_KEY")
	if apiKey == "" {
		t.Skip("YOUTUBE_API_KEY not set; skipping integration test against the real YouTube Data API")
	}

	client, err := NewClient(context.Background(), apiKey)
	if err != nil {
		t.Fatalf("NewClient() returned unexpected error: %v", err)
	}

	_, err = client.FetchLatestVideos(context.Background(), "UC_this_channel_does_not_exist_00000", 5)
	if !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/youtube/...`
Expected: FAIL — compile error, `NewClient`, `FetchLatestVideos`, `ErrChannelNotFound` undefined.

- [ ] **Step 4: Write `client.go`**

```go
// internal/youtube/client.go
package youtube

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/api/option"
	youtubeapi "google.golang.org/api/youtube/v3"
)

type Video struct {
	ID          string
	Title       string
	Description string
	Tags        []string
	PublishedAt time.Time
}

type FetchResult struct {
	Videos    []Video
	QuotaUsed int
}

var ErrChannelNotFound = errors.New("youtube: channel not found")

type Client struct {
	service *youtubeapi.Service
}

func NewClient(ctx context.Context, apiKey string) (*Client, error) {
	service, err := youtubeapi.NewService(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("youtube: creating service: %w", err)
	}
	return &Client{service: service}, nil
}

func (c *Client) FetchLatestVideos(ctx context.Context, channelID string, maxResults int) (FetchResult, error) {
	var result FetchResult

	channelsResp, err := c.service.Channels.List([]string{"contentDetails"}).Id(channelID).Context(ctx).Do()
	result.QuotaUsed++
	if err != nil {
		return result, fmt.Errorf("youtube: channels.list: %w", err)
	}
	if len(channelsResp.Items) == 0 {
		return result, ErrChannelNotFound
	}
	uploadsPlaylistID := channelsResp.Items[0].ContentDetails.RelatedPlaylists.Uploads

	playlistResp, err := c.service.PlaylistItems.List([]string{"snippet"}).
		PlaylistId(uploadsPlaylistID).MaxResults(int64(maxResults)).Context(ctx).Do()
	result.QuotaUsed++
	if err != nil {
		return result, fmt.Errorf("youtube: playlistItems.list: %w", err)
	}

	videoIDs := make([]string, 0, len(playlistResp.Items))
	for _, item := range playlistResp.Items {
		videoIDs = append(videoIDs, item.Snippet.ResourceId.VideoId)
	}
	if len(videoIDs) == 0 {
		return result, nil
	}

	videosResp, err := c.service.Videos.List([]string{"snippet"}).Id(strings.Join(videoIDs, ",")).Context(ctx).Do()
	result.QuotaUsed++
	if err != nil {
		return result, fmt.Errorf("youtube: videos.list: %w", err)
	}

	for _, v := range videosResp.Items {
		publishedAt, err := time.Parse(time.RFC3339, v.Snippet.PublishedAt)
		if err != nil {
			return result, fmt.Errorf("youtube: parsing publishedAt for video %s: %w", v.Id, err)
		}
		result.Videos = append(result.Videos, Video{
			ID:          v.Id,
			Title:       v.Snippet.Title,
			Description: v.Snippet.Description,
			Tags:        v.Snippet.Tags,
			PublishedAt: publishedAt,
		})
	}
	return result, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
export YOUTUBE_API_KEY="<your key>"
go test ./internal/youtube/... -v
```

Expected: both tests PASS against the real YouTube Data API (each call costs a total of 3 quota units)

- [ ] **Step 6: Commit**

```bash
git add internal/youtube go.mod go.sum
git commit -m "feat: add YouTube Data API client"
```

---

### Task 6: Channel sync orchestrator

**Files:**
- Create: `internal/channelsync/syncer.go`
- Test: `internal/channelsync/syncer_test.go`

**Interfaces:**
- Consumes: `youtube.FetchResult`, `youtube.Video`, `youtube.ErrChannelNotFound` (Task 5); `storage.ChannelVideo` (Task 3)
- Produces: `channelsync.ErrQuotaExceeded`, `channelsync.Result{VideosSynced, QuotaUsed int}`, `channelsync.YouTubeFetcher`, `channelsync.QuotaTracker`, `channelsync.VideoStore` interfaces, `channelsync.NewSyncer(fetcher, quota, store, maxResults, dailyCap int) *Syncer`, `(*Syncer) SyncChannel(ctx, channelID string) (Result, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/channelsync/syncer_test.go
package channelsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

type fakeFetcher struct {
	result youtube.FetchResult
	err    error
}

func (f *fakeFetcher) FetchLatestVideos(ctx context.Context, channelID string, maxResults int) (youtube.FetchResult, error) {
	return f.result, f.err
}

type fakeQuotaTracker struct {
	used        int
	incremented int
}

func (f *fakeQuotaTracker) UnitsUsedToday(ctx context.Context) (int, error) {
	return f.used, nil
}

func (f *fakeQuotaTracker) IncrementUnitsUsed(ctx context.Context, units int) error {
	f.incremented += units
	return nil
}

type fakeVideoStore struct {
	stored []storage.ChannelVideo
}

func (f *fakeVideoStore) UpsertChannelVideos(ctx context.Context, channelID string, videos []storage.ChannelVideo) error {
	f.stored = append(f.stored, videos...)
	return nil
}

func TestSyncChannel_StoresVideosAndRecordsQuota(t *testing.T) {
	fetcher := &fakeFetcher{result: youtube.FetchResult{
		Videos:    []youtube.Video{{ID: "v1", Title: "Video 1", PublishedAt: time.Now()}},
		QuotaUsed: 3,
	}}
	quota := &fakeQuotaTracker{used: 0}
	store := &fakeVideoStore{}
	syncer := NewSyncer(fetcher, quota, store, 25, 9000)

	result, err := syncer.SyncChannel(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("SyncChannel() returned unexpected error: %v", err)
	}
	if result.VideosSynced != 1 {
		t.Errorf("VideosSynced = %d, want 1", result.VideosSynced)
	}
	if result.QuotaUsed != 3 {
		t.Errorf("QuotaUsed = %d, want 3", result.QuotaUsed)
	}
	if quota.incremented != 3 {
		t.Errorf("quota.incremented = %d, want 3", quota.incremented)
	}
	if len(store.stored) != 1 {
		t.Fatalf("len(store.stored) = %d, want 1", len(store.stored))
	}
}

func TestSyncChannel_RefusesWhenQuotaCapWouldBeExceeded(t *testing.T) {
	fetcher := &fakeFetcher{}
	quota := &fakeQuotaTracker{used: 8999}
	store := &fakeVideoStore{}
	syncer := NewSyncer(fetcher, quota, store, 25, 9000)

	_, err := syncer.SyncChannel(context.Background(), "UC123")
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("err = %v, want ErrQuotaExceeded", err)
	}
	if quota.incremented != 0 {
		t.Errorf("quota.incremented = %d, want 0 (should not call YouTube at all)", quota.incremented)
	}
}

func TestSyncChannel_RecordsPartialQuotaOnFetchError(t *testing.T) {
	fetcher := &fakeFetcher{
		result: youtube.FetchResult{QuotaUsed: 1},
		err:    youtube.ErrChannelNotFound,
	}
	quota := &fakeQuotaTracker{used: 0}
	store := &fakeVideoStore{}
	syncer := NewSyncer(fetcher, quota, store, 25, 9000)

	_, err := syncer.SyncChannel(context.Background(), "UC123")
	if !errors.Is(err, youtube.ErrChannelNotFound) {
		t.Errorf("err = %v, want youtube.ErrChannelNotFound", err)
	}
	if quota.incremented != 1 {
		t.Errorf("quota.incremented = %d, want 1 (partial cost still recorded)", quota.incremented)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/channelsync/...`
Expected: FAIL — compile error, `NewSyncer` and `ErrQuotaExceeded` undefined.

- [ ] **Step 3: Write `syncer.go`**

```go
// internal/channelsync/syncer.go
package channelsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

var ErrQuotaExceeded = errors.New("channelsync: daily YouTube quota cap reached")

type YouTubeFetcher interface {
	FetchLatestVideos(ctx context.Context, channelID string, maxResults int) (youtube.FetchResult, error)
}

type QuotaTracker interface {
	UnitsUsedToday(ctx context.Context) (int, error)
	IncrementUnitsUsed(ctx context.Context, units int) error
}

type VideoStore interface {
	UpsertChannelVideos(ctx context.Context, channelID string, videos []storage.ChannelVideo) error
}

const estimatedUnitsPerSync = 3

type Syncer struct {
	fetcher    YouTubeFetcher
	quota      QuotaTracker
	store      VideoStore
	maxResults int
	dailyCap   int
}

func NewSyncer(fetcher YouTubeFetcher, quota QuotaTracker, store VideoStore, maxResults, dailyCap int) *Syncer {
	return &Syncer{fetcher: fetcher, quota: quota, store: store, maxResults: maxResults, dailyCap: dailyCap}
}

type Result struct {
	VideosSynced int
	QuotaUsed    int
}

func (s *Syncer) SyncChannel(ctx context.Context, channelID string) (Result, error) {
	used, err := s.quota.UnitsUsedToday(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("channelsync: checking quota: %w", err)
	}
	if used+estimatedUnitsPerSync > s.dailyCap {
		return Result{}, ErrQuotaExceeded
	}

	fetchResult, fetchErr := s.fetcher.FetchLatestVideos(ctx, channelID, s.maxResults)

	if fetchResult.QuotaUsed > 0 {
		if err := s.quota.IncrementUnitsUsed(ctx, fetchResult.QuotaUsed); err != nil {
			return Result{}, fmt.Errorf("channelsync: recording quota usage: %w", err)
		}
	}

	if fetchErr != nil {
		return Result{QuotaUsed: fetchResult.QuotaUsed}, fetchErr
	}

	videos := make([]storage.ChannelVideo, 0, len(fetchResult.Videos))
	for _, v := range fetchResult.Videos {
		videos = append(videos, storage.ChannelVideo{
			ChannelID:   channelID,
			VideoID:     v.ID,
			Title:       v.Title,
			Description: v.Description,
			Tags:        v.Tags,
			PublishedAt: v.PublishedAt,
		})
	}

	if err := s.store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		return Result{QuotaUsed: fetchResult.QuotaUsed}, fmt.Errorf("channelsync: storing videos: %w", err)
	}

	return Result{VideosSynced: len(videos), QuotaUsed: fetchResult.QuotaUsed}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/channelsync/... -v`
Expected: all 3 tests PASS (no network, no database)

- [ ] **Step 5: Commit**

```bash
git add internal/channelsync
git commit -m "feat: add channel sync orchestrator with quota governance"
```

---

### Task 7: Protected sync endpoint and end-to-end wiring

**Files:**
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`
- Modify: `internal/api/middleware.go`
- Create: `internal/api/channel_sync.go`
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: `channelsync.Result`, `channelsync.ErrQuotaExceeded` (Task 6), `youtube.ErrChannelNotFound` (Task 5), `youtube.NewClient`, `channelsync.NewSyncer`, `storage.NewStore` (Sessions 1-3)
- Produces: `api.ChannelSyncer` interface, `NewRouter(finder ClientFinder, recorder UsageRecorder, syncer ChannelSyncer) *chi.Mux` (signature change from Session 2's two-argument `NewRouter`)

- [ ] **Step 1: Update the router tests**

```go
// internal/api/router_test.go
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

type fakeChannelSyncer struct {
	result channelsync.Result
	err    error
}

func (f *fakeChannelSyncer) SyncChannel(ctx context.Context, channelID string) (channelsync.Result, error) {
	return f.result, f.err
}

func TestHealthz_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestWhoami_ReturnsClientInfoWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(finder, recorder, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["client_id"] != client.ID {
		t.Errorf("client_id = %q, want %q", body["client_id"], client.ID)
	}
}

func TestWhoami_ReturnsUnauthorizedWithoutKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeClientFinder{}, &fakeUsageRecorder{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestChannelSync_ReturnsSyncResultWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	syncer := &fakeChannelSyncer{result: channelsync.Result{VideosSynced: 5, QuotaUsed: 3}}

	req := httptest.NewRequest(http.MethodPost, "/v1/internal/channels/UC123/sync", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(finder, recorder, syncer).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["channel_id"] != "UC123" {
		t.Errorf("channel_id = %v, want %q", body["channel_id"], "UC123")
	}
}

func TestChannelSync_ReturnsNotFoundForUnknownChannel(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	syncer := &fakeChannelSyncer{err: youtube.ErrChannelNotFound}

	req := httptest.NewRequest(http.MethodPost, "/v1/internal/channels/UC-does-not-exist/sync", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(finder, recorder, syncer).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestChannelSync_ReturnsTooManyRequestsWhenQuotaExceeded(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	syncer := &fakeChannelSyncer{err: channelsync.ErrQuotaExceeded}

	req := httptest.NewRequest(http.MethodPost, "/v1/internal/channels/UC123/sync", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(finder, recorder, syncer).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header not set")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/...`
Expected: FAIL — `NewRouter(nil, nil, nil)` compile error (existing `NewRouter` takes two arguments), `ChannelSyncer`/`handleChannelSync` undefined once that's fixed.

- [ ] **Step 3: Add a shared JSON error helper and use it from the auth middleware**

```go
// internal/api/middleware.go — replace the writeUnauthorized function and its call site
```

Replace:
```go
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": "invalid or missing API key"})
}
```
with:
```go
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
```

And replace both call sites of `writeUnauthorized(w)` in `middleware.go` with `writeJSONError(w, http.StatusUnauthorized, "invalid or missing API key")`.

- [ ] **Step 4: Write `channel_sync.go`**

```go
// internal/api/channel_sync.go
package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

type ChannelSyncer interface {
	SyncChannel(ctx context.Context, channelID string) (channelsync.Result, error)
}

func handleChannelSync(syncer ChannelSyncer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := chi.URLParam(r, "channelID")
		if channelID == "" {
			writeJSONError(w, http.StatusBadRequest, "channel id is required")
			return
		}

		result, err := syncer.SyncChannel(r.Context(), channelID)
		if err != nil {
			switch {
			case errors.Is(err, youtube.ErrChannelNotFound):
				writeJSONError(w, http.StatusNotFound, "channel not found")
			case errors.Is(err, channelsync.ErrQuotaExceeded):
				w.Header().Set("Retry-After", strconv.Itoa(secondsUntilNextUTCMidnight()))
				writeJSONError(w, http.StatusTooManyRequests, "daily YouTube quota reached")
			default:
				log.Printf("channel sync: %v", err)
				writeJSONError(w, http.StatusInternalServerError, "failed to sync channel")
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"channel_id":    channelID,
			"videos_synced": result.VideosSynced,
			"quota_used":    result.QuotaUsed,
		})
	}
}

func secondsUntilNextUTCMidnight() int {
	now := time.Now().UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return int(nextMidnight.Sub(now).Seconds())
}
```

- [ ] **Step 5: Update `router.go`**

```go
// internal/api/router.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(finder ClientFinder, recorder UsageRecorder, syncer ChannelSyncer) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)

	r.Group(func(r chi.Router) {
		r.Use(RequireAPIKey(finder, recorder))
		r.Get("/v1/whoami", handleWhoami)
		r.Post("/v1/internal/channels/{channelID}/sync", handleChannelSync(syncer))
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

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: all tests PASS

- [ ] **Step 7: Update `cmd/api/main.go`**

```go
// cmd/api/main.go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

const maxVideosPerChannel = 25

func main() {
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

	router := api.NewRouter(store, store, syncer)

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 8: Build and run the full test suite**

```bash
go build ./...
go test ./... -v
```

Expected: no build errors; all tests PASS (with `DATABASE_URL` and `YOUTUBE_API_KEY` exported)

- [ ] **Step 9: Issue a test API key (if you don't already have one saved from Session 2)**

```bash
go run ./cmd/issuekey -name "Session 3 Test Client" -email "session3@example.com"
```

- [ ] **Step 10: Run the server and sync a real channel**

```bash
export PORT=8081
go run ./cmd/api
```

In another terminal:

```bash
curl -i -X POST http://localhost:8081/v1/internal/channels/UC_x5XG1OV2P6uZZ5FSM9Ttw/sync \
  -H "Authorization: Bearer <your key>"
```

Expected: `HTTP/1.1 200 OK` with a JSON body like `{"channel_id":"UC_x5XG1OV2P6uZZ5FSM9Ttw","videos_synced":25,"quota_used":3}`. Stop the server (Ctrl+C) after confirming.

- [ ] **Step 11: Verify the cached videos and quota usage in Supabase**

```bash
psql "$DATABASE_URL" -c "SELECT count(*) FROM channel_videos WHERE channel_id = 'UC_x5XG1OV2P6uZZ5FSM9Ttw';"
psql "$DATABASE_URL" -c "SELECT * FROM youtube_quota_usage WHERE date = CURRENT_DATE;"
```

Expected: the video count matches `videos_synced` from Step 10; the quota row's `units_used` reflects the 3 units from this sync (plus any from earlier test runs today).

- [ ] **Step 12: Commit**

```bash
git add internal/api cmd/api
git commit -m "feat: add protected channel sync endpoint"
```

---

## Session 3 done when

- `go test ./...` passes locally (with `DATABASE_URL` and `YOUTUBE_API_KEY` exported)
- `POST /v1/internal/channels/{channelID}/sync` with a valid key and a real channel ID returns `200` with a sync summary
- The channel's videos appear in `channel_videos` in Supabase
- `youtube_quota_usage` for today reflects the units actually consumed
- A request against a nonexistent channel returns `404`; simulating an exhausted quota (e.g., temporarily setting `YOUTUBE_DAILY_QUOTA_CAP=0`) returns `429` with a `Retry-After` header
