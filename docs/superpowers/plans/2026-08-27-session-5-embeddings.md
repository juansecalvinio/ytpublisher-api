# Session 5: Voyage AI Embeddings, pgvector Storage, Related Video Search — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Compute and store embeddings for a channel's cached videos using Voyage AI, and expose a search endpoint that finds the most semantically similar cached videos to a given topic via pgvector cosine similarity — building on Session 3's `channel_videos` cache.

**Architecture:** `internal/embeddings` wraps the Voyage AI HTTP API behind `EmbedDocuments`/`EmbedQuery` (asymmetric embeddings: videos are embedded as documents, search topics as queries, per Voyage's own guidance for retrieval quality). `internal/storage` gains an `embedding vector(1024)` column on `channel_videos` (pgvector) and methods to write/search it. `internal/relatedvideos` orchestrates: list a channel's cached videos, embed any that don't have a vector yet, embed the search topic, then delegate to a pgvector cosine-distance query — unit-tested entirely with fakes.

**Tech Stack:** Adds `github.com/pgvector/pgvector-go` (pgvector Postgres extension bindings for `pgx`). Everything else matches Sessions 1-4 (Go 1.22+, chi, pgx/v5, goose). No embeddings SDK needed — Voyage's HTTP API is called directly with the standard library.

**Spec:** `docs/superpowers/specs/2026-08-24-ytpublisher-api-v1-design.md`

## Global Constraints

- Go version: 1.22+
- Module path: `github.com/juansecalvinio/ytpublisher-api`
- Embeddings provider: Voyage AI, `POST https://api.voyageai.com/v1/embeddings`, `Authorization: Bearer $VOYAGE_API_KEY` (verified against Voyage's official API reference)
- Default model: `voyage-3.5-lite`, 1024-dimension output (verified against Voyage's model table) — configurable via `VOYAGE_MODEL`
- Videos are embedded with `input_type: "document"`; search topics with `input_type: "query"` — Voyage's documented recommendation for asymmetric retrieval quality
- Vector storage: pgvector extension on the existing Supabase Postgres, no dedicated vector DB (per the spec) — no HNSW index yet, since the per-channel video volume doesn't justify one (per the spec's own note)
- pgvector Go bindings verified against `pkg.go.dev/github.com/pgvector/pgvector-go`: `pgvector.NewVector([]float32) Vector`, `(Vector) Slice() []float32`, `pgxvec.RegisterTypes(ctx, conn) error` (package `github.com/pgvector/pgvector-go/pgx`)

---

## Prerequisites

- Sessions 1-4 merged (config, storage, auth, YouTube sync, style cache all present in `main`)
- `DATABASE_URL` and `YOUTUBE_API_KEY` exported in your shell
- `VOYAGE_API_KEY` exported in your shell (Voyage AI account created)
- At least one channel already synced (e.g. `UC_x5XG1OV2P6uZZ5FSM9Ttw` from Sessions 3-4) with cached videos to embed
- **Important ordering note:** once this session's migration is applied, `storage.NewPool` will register the `vector` type on every connection — this requires the `vector` extension to already exist in the database. Always run migrations *before* starting the app from this session onward (already true in practice, but now it's a hard requirement, not just good hygiene).

---

### Task 1: Migration — enable pgvector, add `embedding` column

**Files:**
- Create: `migrations/00006_add_channel_videos_embedding.sql`

**Interfaces:**
- Produces: `vector` Postgres extension enabled; `channel_videos.embedding vector(1024)` column (nullable)

- [ ] **Step 1: Write the migration**

```sql
-- migrations/00006_add_channel_videos_embedding.sql

-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
ALTER TABLE channel_videos ADD COLUMN embedding vector(1024);

-- +goose Down
ALTER TABLE channel_videos DROP COLUMN embedding;
DROP EXTENSION IF EXISTS vector;
```

- [ ] **Step 2: Apply it**

```bash
~/go/bin/goose -dir migrations postgres "$DATABASE_URL" up
```

Expected: `OK   00006_add_channel_videos_embedding.sql`

- [ ] **Step 3: Verify**

```bash
psql "$DATABASE_URL" -c "\d channel_videos"
psql "$DATABASE_URL" -c "SELECT extname FROM pg_extension WHERE extname = 'vector';"
```

Expected: `channel_videos` now lists an `embedding` column of type `vector(1024)`; the extension query returns one row.

- [ ] **Step 4: Commit**

```bash
git add migrations/00006_add_channel_videos_embedding.sql
git commit -m "feat: enable pgvector and add embedding column to channel_videos"
```

---

### Task 2: Config — Voyage API key and model

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config` gains `VoyageAPIKey string` and `VoyageModel string`; new sentinel `config.ErrMissingVoyageAPIKey`

- [ ] **Step 1: Update the tests**

```go
// internal/config/config_test.go
package config

import (
	"errors"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")
	t.Setenv("VOYAGE_API_KEY", "test-voyage-key")
	t.Setenv("VOYAGE_MODEL", "")
}

func TestLoad_UsesDefaultPortWhenUnset(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want %q", cfg.Port, "8080")
	}
}

func TestLoad_ReadsCustomPort(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PORT", "9090")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want %q", cfg.Port, "9090")
	}
}

func TestLoad_ReadsDatabaseURL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_URL", "postgres://user:pass@host:5432/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://user:pass@host:5432/db" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://user:pass@host:5432/db")
	}
}

func TestLoad_ErrorsWhenDatabaseURLMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Errorf("err = %v, want ErrMissingDatabaseURL", err)
	}
}

func TestLoad_ErrorsWhenYouTubeAPIKeyMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("YOUTUBE_API_KEY", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingYouTubeAPIKey) {
		t.Errorf("err = %v, want ErrMissingYouTubeAPIKey", err)
	}
}

func TestLoad_UsesDefaultQuotaCapWhenUnset(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.YouTubeDailyQuotaCap != 9000 {
		t.Errorf("YouTubeDailyQuotaCap = %d, want 9000", cfg.YouTubeDailyQuotaCap)
	}
}

func TestLoad_ReadsCustomQuotaCap(t *testing.T) {
	setRequiredEnv(t)
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
	setRequiredEnv(t)
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "not-a-number")

	_, err := Load()
	if !errors.Is(err, ErrInvalidQuotaCap) {
		t.Errorf("err = %v, want ErrInvalidQuotaCap", err)
	}
}

func TestLoad_UsesDefaultStyleCacheTTLWhenUnset(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.StyleCacheTTLHours != 48 {
		t.Errorf("StyleCacheTTLHours = %d, want 48", cfg.StyleCacheTTLHours)
	}
}

func TestLoad_ReadsCustomStyleCacheTTL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STYLE_CACHE_TTL_HOURS", "72")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.StyleCacheTTLHours != 72 {
		t.Errorf("StyleCacheTTLHours = %d, want 72", cfg.StyleCacheTTLHours)
	}
}

func TestLoad_ErrorsWhenStyleCacheTTLInvalid(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STYLE_CACHE_TTL_HOURS", "not-a-number")

	_, err := Load()
	if !errors.Is(err, ErrInvalidStyleCacheTTL) {
		t.Errorf("err = %v, want ErrInvalidStyleCacheTTL", err)
	}
}

func TestLoad_ErrorsWhenVoyageAPIKeyMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("VOYAGE_API_KEY", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingVoyageAPIKey) {
		t.Errorf("err = %v, want ErrMissingVoyageAPIKey", err)
	}
}

func TestLoad_UsesDefaultVoyageModelWhenUnset(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.VoyageModel != "voyage-3.5-lite" {
		t.Errorf("VoyageModel = %q, want %q", cfg.VoyageModel, "voyage-3.5-lite")
	}
}

func TestLoad_ReadsCustomVoyageModel(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("VOYAGE_MODEL", "voyage-3-large")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.VoyageModel != "voyage-3-large" {
		t.Errorf("VoyageModel = %q, want %q", cfg.VoyageModel, "voyage-3-large")
	}
}
```

- [ ] **Step 2: Run tests to verify the new ones fail**

Run: `go test ./internal/config/...`
Expected: FAIL — compile error, `ErrMissingVoyageAPIKey` and `VoyageModel` undefined.

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
	StyleCacheTTLHours   int
	VoyageAPIKey         string
	VoyageModel          string
}

var (
	ErrMissingDatabaseURL   = errors.New("config: DATABASE_URL is required")
	ErrMissingYouTubeAPIKey = errors.New("config: YOUTUBE_API_KEY is required")
	ErrInvalidQuotaCap      = errors.New("config: YOUTUBE_DAILY_QUOTA_CAP must be a positive integer")
	ErrInvalidStyleCacheTTL = errors.New("config: STYLE_CACHE_TTL_HOURS must be a positive integer")
	ErrMissingVoyageAPIKey  = errors.New("config: VOYAGE_API_KEY is required")
)

const (
	defaultYouTubeDailyQuotaCap = 9000
	defaultStyleCacheTTLHours   = 48
	defaultVoyageModel          = "voyage-3.5-lite"
)

func Load() (Config, error) {
	cfg := Config{
		Port:          os.Getenv("PORT"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		YouTubeAPIKey: os.Getenv("YOUTUBE_API_KEY"),
		VoyageAPIKey:  os.Getenv("VOYAGE_API_KEY"),
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
	if cfg.VoyageAPIKey == "" {
		return Config{}, ErrMissingVoyageAPIKey
	}

	cfg.YouTubeDailyQuotaCap = defaultYouTubeDailyQuotaCap
	if raw := os.Getenv("YOUTUBE_DAILY_QUOTA_CAP"); raw != "" {
		cap, err := strconv.Atoi(raw)
		if err != nil || cap <= 0 {
			return Config{}, ErrInvalidQuotaCap
		}
		cfg.YouTubeDailyQuotaCap = cap
	}

	cfg.StyleCacheTTLHours = defaultStyleCacheTTLHours
	if raw := os.Getenv("STYLE_CACHE_TTL_HOURS"); raw != "" {
		hours, err := strconv.Atoi(raw)
		if err != nil || hours <= 0 {
			return Config{}, ErrInvalidStyleCacheTTL
		}
		cfg.StyleCacheTTLHours = hours
	}

	cfg.VoyageModel = os.Getenv("VOYAGE_MODEL")
	if cfg.VoyageModel == "" {
		cfg.VoyageModel = defaultVoyageModel
	}

	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: all 14 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: add Voyage API key and model to config"
```

---

### Task 3: Storage — pgvector registration and embedding persistence

**Files:**
- Modify: `internal/storage/db.go`
- Modify: `internal/storage/channel_video.go`
- Create: `internal/storage/embedding_test.go`

**Interfaces:**
- Produces: `storage.ChannelVideo` gains `Embedding []float32`; `(*Store) UpdateChannelVideoEmbedding(ctx, channelID, videoID string, embedding []float32) error`; `(*Store) FindSimilarVideos(ctx, channelID string, queryEmbedding []float32, limit int) ([]ChannelVideo, error)`; `ListChannelVideos` now also populates `Embedding` when present

- [ ] **Step 1: Add the pgvector-go dependency**

```bash
go get github.com/pgvector/pgvector-go
```

- [ ] **Step 2: Write the failing tests**

```go
// internal/storage/embedding_test.go
package storage

import (
	"context"
	"testing"
	"time"
)

func TestUpdateChannelVideoEmbedding_ThenFindSimilarVideos(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-embed-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	videos := []ChannelVideo{
		{ChannelID: channelID, VideoID: "close", Title: "Close", PublishedAt: time.Now()},
		{ChannelID: channelID, VideoID: "far", Title: "Far", PublishedAt: time.Now()},
	}
	if err := store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		t.Fatalf("UpsertChannelVideos() returned unexpected error: %v", err)
	}

	closeEmbedding := make([]float32, 1024)
	farEmbedding := make([]float32, 1024)
	queryEmbedding := make([]float32, 1024)
	for i := range closeEmbedding {
		closeEmbedding[i] = 1.0
		farEmbedding[i] = -1.0
		queryEmbedding[i] = 1.0
	}

	if err := store.UpdateChannelVideoEmbedding(ctx, channelID, "close", closeEmbedding); err != nil {
		t.Fatalf("UpdateChannelVideoEmbedding() (close) returned unexpected error: %v", err)
	}
	if err := store.UpdateChannelVideoEmbedding(ctx, channelID, "far", farEmbedding); err != nil {
		t.Fatalf("UpdateChannelVideoEmbedding() (far) returned unexpected error: %v", err)
	}

	results, err := store.FindSimilarVideos(ctx, channelID, queryEmbedding, 1)
	if err != nil {
		t.Fatalf("FindSimilarVideos() returned unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].VideoID != "close" {
		t.Errorf("results[0].VideoID = %q, want %q (most similar to the query direction)", results[0].VideoID, "close")
	}
}

func TestListChannelVideos_IncludesEmbeddingWhenPresent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-embed-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	videos := []ChannelVideo{{ChannelID: channelID, VideoID: "v1", Title: "V1", PublishedAt: time.Now()}}
	if err := store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		t.Fatalf("UpsertChannelVideos() returned unexpected error: %v", err)
	}

	found, err := store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() returned unexpected error: %v", err)
	}
	if len(found[0].Embedding) != 0 {
		t.Errorf("Embedding = %v, want empty (not yet computed)", found[0].Embedding)
	}

	embedding := make([]float32, 1024)
	embedding[0] = 0.5
	if err := store.UpdateChannelVideoEmbedding(ctx, channelID, "v1", embedding); err != nil {
		t.Fatalf("UpdateChannelVideoEmbedding() returned unexpected error: %v", err)
	}

	found, err = store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() (after) returned unexpected error: %v", err)
	}
	if len(found[0].Embedding) != 1024 {
		t.Fatalf("len(Embedding) = %d, want 1024", len(found[0].Embedding))
	}
	if found[0].Embedding[0] != 0.5 {
		t.Errorf("Embedding[0] = %v, want 0.5", found[0].Embedding[0])
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/storage/... -run 'TestUpdateChannelVideoEmbedding|TestListChannelVideos_IncludesEmbedding'`
Expected: FAIL — compile error, `UpdateChannelVideoEmbedding`, `FindSimilarVideos` undefined, `Embedding` field undefined.

- [ ] **Step 4: Register the pgvector type in `db.go`**

```go
// internal/storage/db.go
package storage

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvec "github.com/pgvector/pgvector-go/pgx"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvec.RegisterTypes(ctx, conn)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
```

- [ ] **Step 5: Update `channel_video.go`**

```go
// internal/storage/channel_video.go
package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pgvector/pgvector-go"
)

type ChannelVideo struct {
	ChannelID   string
	VideoID     string
	Title       string
	Description string
	Tags        []string
	PublishedAt time.Time
	Embedding   []float32
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
		`SELECT video_id, title, description, tags_json, published_at, embedding
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
		var embedding *pgvector.Vector
		if err := rows.Scan(&v.VideoID, &v.Title, &v.Description, &tagsJSON, &v.PublishedAt, &embedding); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tagsJSON, &v.Tags); err != nil {
			return nil, err
		}
		if embedding != nil {
			v.Embedding = embedding.Slice()
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

func (s *Store) UpdateChannelVideoEmbedding(ctx context.Context, channelID, videoID string, embedding []float32) error {
	vec := pgvector.NewVector(embedding)
	_, err := s.pool.Exec(ctx,
		`UPDATE channel_videos SET embedding = $1 WHERE channel_id = $2 AND video_id = $3`,
		vec, channelID, videoID,
	)
	return err
}

func (s *Store) FindSimilarVideos(ctx context.Context, channelID string, queryEmbedding []float32, limit int) ([]ChannelVideo, error) {
	vec := pgvector.NewVector(queryEmbedding)
	rows, err := s.pool.Query(ctx,
		`SELECT video_id, title, description, tags_json, published_at
		 FROM channel_videos
		 WHERE channel_id = $1 AND embedding IS NOT NULL
		 ORDER BY embedding <=> $2
		 LIMIT $3`,
		channelID, vec, limit,
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
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/storage/... -v`
Expected: all tests PASS, including the two new ones and every pre-existing storage test (proves the `NewPool` change didn't break anything)

- [ ] **Step 7: Commit**

```bash
git add internal/storage go.mod go.sum
git commit -m "feat: register pgvector and add embedding storage for channel videos"
```

---

### Task 4: Voyage AI embeddings client

**Files:**
- Create: `internal/embeddings/client.go`
- Test: `internal/embeddings/client_test.go`

**Interfaces:**
- Produces: `embeddings.NewClient(apiKey, model string) *Client`, `(*Client) EmbedDocuments(ctx, texts []string) ([][]float32, error)`, `(*Client) EmbedQuery(ctx, text string) ([]float32, error)`

- [ ] **Step 1: Write the failing integration tests**

```go
// internal/embeddings/client_test.go
package embeddings

import (
	"context"
	"os"
	"testing"
)

func TestEmbedDocuments_ReturnsVectorsWithExpectedDimension(t *testing.T) {
	apiKey := os.Getenv("VOYAGE_API_KEY")
	if apiKey == "" {
		t.Skip("VOYAGE_API_KEY not set; skipping integration test against the real Voyage API")
	}

	client := NewClient(apiKey, "voyage-3.5-lite")

	vectors, err := client.EmbedDocuments(context.Background(), []string{"Go programming tutorial", "How to bake bread"})
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
}

func TestEmbedQuery_ReturnsSingleVector(t *testing.T) {
	apiKey := os.Getenv("VOYAGE_API_KEY")
	if apiKey == "" {
		t.Skip("VOYAGE_API_KEY not set; skipping integration test against the real Voyage API")
	}

	client := NewClient(apiKey, "voyage-3.5-lite")

	vector, err := client.EmbedQuery(context.Background(), "Go programming")
	if err != nil {
		t.Fatalf("EmbedQuery() returned unexpected error: %v", err)
	}
	if len(vector) != 1024 {
		t.Errorf("len(vector) = %d, want 1024", len(vector))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/embeddings/...`
Expected: FAIL — compile error, `NewClient` undefined.

- [ ] **Step 3: Write `client.go`**

```go
// internal/embeddings/client.go
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

func (c *Client) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	return c.embed(ctx, texts, "document")
}

func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	vectors, err := c.embed(ctx, []string{text}, "query")
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
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
}

func (c *Client) embed(ctx context.Context, texts []string, inputType string) ([][]float32, error) {
	reqBody, err := json.Marshal(embedRequest{Input: texts, Model: c.model, InputType: inputType})
	if err != nil {
		return nil, fmt.Errorf("embeddings: encoding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, embeddingsURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("embeddings: building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embeddings: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embeddings: voyage returned status %d: %s", resp.StatusCode, body)
	}

	var parsed embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("embeddings: decoding response: %w", err)
	}

	result := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		if d.Index < 0 || d.Index >= len(result) {
			return nil, fmt.Errorf("embeddings: response index %d out of range", d.Index)
		}
		result[d.Index] = d.Embedding
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
export VOYAGE_API_KEY="<your key>"
go test ./internal/embeddings/... -v
```

Expected: both tests PASS against the real Voyage API, each vector has exactly 1024 dimensions

- [ ] **Step 5: Commit**

```bash
git add internal/embeddings
git commit -m "feat: add Voyage AI embeddings client"
```

---

### Task 5: Related videos orchestrator

**Files:**
- Create: `internal/relatedvideos/provider.go`
- Test: `internal/relatedvideos/provider_test.go`

**Interfaces:**
- Consumes: `storage.ChannelVideo` (Sessions 3, 5)
- Produces: `relatedvideos.VideoStore`, `relatedvideos.Embedder` interfaces, `relatedvideos.NewProvider(videos VideoStore, embedder Embedder) *Provider`, `(*Provider) FindRelated(ctx, channelID, topic string, limit int) ([]storage.ChannelVideo, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/relatedvideos/provider_test.go
package relatedvideos

import (
	"context"
	"testing"

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

func (f *fakeEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	f.documentCalls = append(f.documentCalls, texts)
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{float32(i)}
	}
	return result, nil
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	f.queryCalls = append(f.queryCalls, text)
	return []float32{1}, nil
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

	results, err := provider.FindRelated(context.Background(), "UC123", "some topic", 5)
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
}

func TestFindRelated_SkipsEmbeddingWhenAllVideosAlreadyEmbedded(t *testing.T) {
	videoStore := &fakeVideoStore{
		videos: []storage.ChannelVideo{
			{VideoID: "v1", Title: "Video 1", Embedding: []float32{0.1}},
		},
	}
	embedder := &fakeEmbedder{}
	provider := NewProvider(videoStore, embedder)

	_, err := provider.FindRelated(context.Background(), "UC123", "some topic", 5)
	if err != nil {
		t.Fatalf("FindRelated() returned unexpected error: %v", err)
	}
	if len(embedder.documentCalls) != 0 {
		t.Errorf("documentCalls = %v, want none (all videos already embedded)", embedder.documentCalls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/relatedvideos/...`
Expected: FAIL — compile error, `NewProvider` undefined.

- [ ] **Step 3: Write `provider.go`**

```go
// internal/relatedvideos/provider.go
package relatedvideos

import (
	"context"
	"fmt"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type VideoStore interface {
	ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error)
	UpdateChannelVideoEmbedding(ctx context.Context, channelID, videoID string, embedding []float32) error
	FindSimilarVideos(ctx context.Context, channelID string, queryEmbedding []float32, limit int) ([]storage.ChannelVideo, error)
}

type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

type Provider struct {
	videos   VideoStore
	embedder Embedder
}

func NewProvider(videos VideoStore, embedder Embedder) *Provider {
	return &Provider{videos: videos, embedder: embedder}
}

func (p *Provider) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, error) {
	videos, err := p.videos.ListChannelVideos(ctx, channelID)
	if err != nil {
		return nil, fmt.Errorf("relatedvideos: listing videos: %w", err)
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
		newEmbeddings, err := p.embedder.EmbedDocuments(ctx, texts)
		if err != nil {
			return nil, fmt.Errorf("relatedvideos: embedding videos: %w", err)
		}
		for i, v := range missing {
			if err := p.videos.UpdateChannelVideoEmbedding(ctx, channelID, v.VideoID, newEmbeddings[i]); err != nil {
				return nil, fmt.Errorf("relatedvideos: storing embedding: %w", err)
			}
		}
	}

	queryEmbedding, err := p.embedder.EmbedQuery(ctx, topic)
	if err != nil {
		return nil, fmt.Errorf("relatedvideos: embedding topic: %w", err)
	}

	results, err := p.videos.FindSimilarVideos(ctx, channelID, queryEmbedding, limit)
	if err != nil {
		return nil, fmt.Errorf("relatedvideos: searching similar videos: %w", err)
	}
	return results, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/relatedvideos/... -v`
Expected: both tests PASS (no network, no database)

- [ ] **Step 5: Commit**

```bash
git add internal/relatedvideos
git commit -m "feat: add related videos orchestrator"
```

---

### Task 6: Protected related-videos endpoint

**Files:**
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`
- Create: `internal/api/related_videos.go`

**Interfaces:**
- Consumes: `storage.ChannelVideo` (Session 3)
- Produces: `api.RelatedVideosProvider` interface, `Dependencies` gains `RelatedVideos RelatedVideosProvider`, new route `GET /v1/internal/channels/{channelID}/related-videos?topic=...`

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
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

type fakeChannelSyncer struct {
	result channelsync.Result
	err    error
}

func (f *fakeChannelSyncer) SyncChannel(ctx context.Context, channelID string) (channelsync.Result, error) {
	return f.result, f.err
}

type fakeStyleProvider struct {
	summary styleanalysis.Summary
	err     error
}

func (f *fakeStyleProvider) GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error) {
	return f.summary, f.err
}

type fakeRelatedVideosProvider struct {
	videos []storage.ChannelVideo
	err    error
}

func (f *fakeRelatedVideosProvider) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, error) {
	return f.videos, f.err
}

func TestHealthz_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{}).ServeHTTP(rec, req)

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

	NewRouter(Dependencies{Finder: finder, Recorder: recorder}).ServeHTTP(rec, req)

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

	NewRouter(Dependencies{Finder: &fakeClientFinder{}, Recorder: &fakeUsageRecorder{}}).ServeHTTP(rec, req)

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

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, Syncer: syncer}).ServeHTTP(rec, req)

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

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, Syncer: syncer}).ServeHTTP(rec, req)

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

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, Syncer: syncer}).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header not set")
	}
}

func TestChannelStyle_ReturnsSummaryWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	provider := &fakeStyleProvider{summary: styleanalysis.Summary{VideoCountAnalyzed: 10, Confidence: "high"}}

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/style", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, StyleProvider: provider}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body styleanalysis.Summary
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.VideoCountAnalyzed != 10 {
		t.Errorf("VideoCountAnalyzed = %d, want 10", body.VideoCountAnalyzed)
	}
}

func TestChannelStyle_ReturnsUnauthorizedWithoutKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/style", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: &fakeClientFinder{}, Recorder: &fakeUsageRecorder{}}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRelatedVideos_ReturnsResultsWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	provider := &fakeRelatedVideosProvider{videos: []storage.ChannelVideo{{VideoID: "v1", Title: "Video 1"}}}

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/related-videos?topic=go+programming", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, RelatedVideos: provider}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["topic"] != "go programming" {
		t.Errorf("topic = %v, want %q", body["topic"], "go programming")
	}
	related, ok := body["related_videos"].([]any)
	if !ok || len(related) != 1 {
		t.Errorf("related_videos = %v, want a 1-element list", body["related_videos"])
	}
}

func TestRelatedVideos_ReturnsBadRequestWithoutTopic(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/related-videos", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRelatedVideos_ReturnsUnauthorizedWithoutKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/related-videos?topic=x", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: &fakeClientFinder{}, Recorder: &fakeUsageRecorder{}}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/...`
Expected: FAIL — `Dependencies` has no field `RelatedVideos`, `handleRelatedVideos`/`RelatedVideosProvider` undefined.

- [ ] **Step 3: Write `related_videos.go`**

```go
// internal/api/related_videos.go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type RelatedVideosProvider interface {
	FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, error)
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

		videos, err := provider.FindRelated(r.Context(), channelID, topic, defaultRelatedVideosLimit)
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

- [ ] **Step 4: Update `router.go`**

```go
// internal/api/router.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Dependencies struct {
	Finder        ClientFinder
	Recorder      UsageRecorder
	Syncer        ChannelSyncer
	StyleProvider StyleProvider
	RelatedVideos RelatedVideosProvider
}

func NewRouter(deps Dependencies) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)

	r.Group(func(r chi.Router) {
		r.Use(RequireAPIKey(deps.Finder, deps.Recorder))
		r.Get("/v1/whoami", handleWhoami)
		r.Post("/v1/internal/channels/{channelID}/sync", handleChannelSync(deps.Syncer))
		r.Get("/v1/internal/channels/{channelID}/style", handleChannelStyle(deps.StyleProvider))
		r.Get("/v1/internal/channels/{channelID}/related-videos", handleRelatedVideos(deps.RelatedVideos))
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

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: all tests PASS

- [ ] **Step 6: Commit**

```bash
git add internal/api
git commit -m "feat: add protected related-videos endpoint"
```

---

### Task 7: Wire `cmd/api/main.go` and verify end-to-end

**Files:**
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: `embeddings.NewClient`, `relatedvideos.NewProvider` (Tasks 4-5)

- [ ] **Step 1: Update `main.go`**

```go
// cmd/api/main.go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/embeddings"
	"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/stylecache"
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

	styleTTL := time.Duration(cfg.StyleCacheTTLHours) * time.Hour
	styleProvider := stylecache.NewProvider(store, store, styleTTL)

	embeddingsClient := embeddings.NewClient(cfg.VoyageAPIKey, cfg.VoyageModel)
	relatedVideosProvider := relatedvideos.NewProvider(store, embeddingsClient)

	router := api.NewRouter(api.Dependencies{
		Finder:        store,
		Recorder:      store,
		Syncer:        syncer,
		StyleProvider: styleProvider,
		RelatedVideos: relatedVideosProvider,
	})

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: Build and run the full test suite**

```bash
go build ./...
go test ./... -v
```

Expected: no build errors; all tests PASS (with `DATABASE_URL`, `YOUTUBE_API_KEY`, and `VOYAGE_API_KEY` exported)

- [ ] **Step 3: Run the server**

```bash
export PORT=8081
go run ./cmd/api
```

- [ ] **Step 4: Search for related videos on the channel synced in Sessions 3-4**

```bash
curl -s "http://localhost:8081/v1/internal/channels/UC_x5XG1OV2P6uZZ5FSM9Ttw/related-videos?topic=machine%20learning" \
  -H "Authorization: Bearer <your key>" | python3 -m json.tool
```

Expected: `200 OK` with a JSON body listing up to 5 `related_videos` (each with `video_id` and `title`) that are plausibly about machine learning, given the channel's real content.

- [ ] **Step 5: Verify embeddings were persisted**

```bash
psql "$DATABASE_URL" -c "SELECT count(*) FROM channel_videos WHERE channel_id = 'UC_x5XG1OV2P6uZZ5FSM9Ttw' AND embedding IS NOT NULL;"
```

Expected: matches the number of videos cached for that channel (all should now have an embedding after the first search call).

- [ ] **Step 6: Confirm the second call doesn't re-embed**

Repeat the Step 4 curl with a different topic (e.g. `topic=web%20development`). It should respond quickly and return different (or the same, depending on content) results, without needing to call Voyage again for videos already embedded. Stop the server (Ctrl+C) after confirming.

- [ ] **Step 7: Commit**

```bash
git add cmd/api
git commit -m "feat: wire embeddings and related videos into main"
```

---

## Session 5 done when

- `go test ./...` passes locally (with `DATABASE_URL`, `YOUTUBE_API_KEY`, and `VOYAGE_API_KEY` exported)
- `GET /v1/internal/channels/{channelID}/related-videos?topic=...` returns real, semantically plausible videos from the channel's own cache
- `channel_videos.embedding` is populated for every video in a searched channel
- A second search against an already-embedded channel does not need to call Voyage again for those videos (only for the new query topic)
