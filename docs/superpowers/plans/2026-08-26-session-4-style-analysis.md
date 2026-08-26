# Session 4: Channel Style Heuristics, Cache with TTL — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect a channel's style (title patterns, tag usage, description structure) from its cached videos using pure statistical heuristics — no LLM — and cache the result with a TTL, exposed through a new protected endpoint.

**Architecture:** `internal/styleanalysis` is a pure, dependency-free package: `Analyze([]storage.ChannelVideo) Summary` — fully unit-tested with fixture data, no I/O. `internal/stylecache` orchestrates caching: check `channel_style_cache` for a fresh entry, else recompute from `channel_videos` and store the result with an expiry. `internal/api`'s `NewRouter` is refactored to take a `Dependencies` struct instead of a growing positional argument list, now that it's gaining its 4th dependency.

**Tech Stack:** No new external dependencies. Same stack as Sessions 1-3 (Go 1.22+, chi, pgx/v5, goose).

**Spec:** `docs/superpowers/specs/2026-08-24-ytpublisher-api-v1-design.md`

## Global Constraints

- Go version: 1.22+
- Module path: `github.com/juansecalvinio/ytpublisher-api`
- Style detection is a pure statistical heuristic (no LLM call) — confirmed approach from the spec's brainstorming
- Channels with fewer than 5 analyzed videos get `confidence: "low"` in the summary (per the spec's risk table)
- Style cache TTL is configurable via `STYLE_CACHE_TTL_HOURS`, default 48
- `internal/api.NewRouter` takes a `Dependencies` struct (not positional args) — this session is the point where a 4th dependency (`StyleProvider`) would otherwise be added to an already-growing argument list

---

## Prerequisites

- Sessions 1-3 merged (config, storage, auth, YouTube sync all present in `main`)
- `DATABASE_URL` exported in your shell
- At least one channel already synced via `POST /v1/internal/channels/{channelID}/sync` (Session 3), so there are real cached videos to analyze — the Google Developers channel (`UC_x5XG1OV2P6uZZ5FSM9Ttw`) from Session 3's verification works fine

---

### Task 1: Migration — `channel_style_cache`

**Files:**
- Create: `migrations/00005_create_channel_style_cache.sql`

**Interfaces:**
- Produces: `channel_style_cache` table (PK `channel_id`)

- [ ] **Step 1: Write the migration**

```sql
-- migrations/00005_create_channel_style_cache.sql

-- +goose Up
CREATE TABLE channel_style_cache (
    channel_id TEXT PRIMARY KEY,
    style_summary_json JSONB NOT NULL,
    video_count_analyzed INTEGER NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE channel_style_cache;
```

- [ ] **Step 2: Apply it**

```bash
~/go/bin/goose -dir migrations postgres "$DATABASE_URL" up
```

Expected: `OK   00005_create_channel_style_cache.sql`

- [ ] **Step 3: Verify**

```bash
psql "$DATABASE_URL" -c "\d channel_style_cache"
```

Expected: 5 columns with `channel_id` as primary key.

- [ ] **Step 4: Commit**

```bash
git add migrations/00005_create_channel_style_cache.sql
git commit -m "feat: add channel_style_cache migration"
```

---

### Task 2: Config — style cache TTL

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config` gains `StyleCacheTTLHours int`; new sentinel `config.ErrInvalidStyleCacheTTL`

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

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
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

	_, err := Load()
	if !errors.Is(err, ErrInvalidQuotaCap) {
		t.Errorf("err = %v, want ErrInvalidQuotaCap", err)
	}
}

func TestLoad_UsesDefaultStyleCacheTTLWhenUnset(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.StyleCacheTTLHours != 48 {
		t.Errorf("StyleCacheTTLHours = %d, want 48", cfg.StyleCacheTTLHours)
	}
}

func TestLoad_ReadsCustomStyleCacheTTL(t *testing.T) {
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")
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
	t.Setenv("PORT", "8080")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")
	t.Setenv("STYLE_CACHE_TTL_HOURS", "not-a-number")

	_, err := Load()
	if !errors.Is(err, ErrInvalidStyleCacheTTL) {
		t.Errorf("err = %v, want ErrInvalidStyleCacheTTL", err)
	}
}
```

- [ ] **Step 2: Run tests to verify the new ones fail**

Run: `go test ./internal/config/...`
Expected: FAIL — compile error, `ErrInvalidStyleCacheTTL` and `StyleCacheTTLHours` undefined.

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
}

var (
	ErrMissingDatabaseURL   = errors.New("config: DATABASE_URL is required")
	ErrMissingYouTubeAPIKey = errors.New("config: YOUTUBE_API_KEY is required")
	ErrInvalidQuotaCap      = errors.New("config: YOUTUBE_DAILY_QUOTA_CAP must be a positive integer")
	ErrInvalidStyleCacheTTL = errors.New("config: STYLE_CACHE_TTL_HOURS must be a positive integer")
)

const (
	defaultYouTubeDailyQuotaCap = 9000
	defaultStyleCacheTTLHours   = 48
)

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

	cfg.StyleCacheTTLHours = defaultStyleCacheTTLHours
	if raw := os.Getenv("STYLE_CACHE_TTL_HOURS"); raw != "" {
		hours, err := strconv.Atoi(raw)
		if err != nil || hours <= 0 {
			return Config{}, ErrInvalidStyleCacheTTL
		}
		cfg.StyleCacheTTLHours = hours
	}

	return cfg, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: all 11 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config
git commit -m "feat: add style cache TTL to config"
```

---

### Task 3: Style heuristics (pure, no I/O)

**Files:**
- Create: `internal/styleanalysis/analyze.go`
- Test: `internal/styleanalysis/analyze_test.go`

**Interfaces:**
- Consumes: `storage.ChannelVideo` (Session 3)
- Produces: `styleanalysis.Summary` (JSON-taggable struct), `styleanalysis.Analyze(videos []storage.ChannelVideo) Summary`

- [ ] **Step 1: Write the failing tests**

```go
// internal/styleanalysis/analyze_test.go
package styleanalysis

import (
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func TestAnalyze_EmptyVideos_ReturnsLowConfidenceZeroSummary(t *testing.T) {
	summary := Analyze(nil)

	if summary.VideoCountAnalyzed != 0 {
		t.Errorf("VideoCountAnalyzed = %d, want 0", summary.VideoCountAnalyzed)
	}
	if summary.Confidence != "low" {
		t.Errorf("Confidence = %q, want %q", summary.Confidence, "low")
	}
}

func TestAnalyze_SetsLowConfidenceBelowThreshold(t *testing.T) {
	videos := make([]storage.ChannelVideo, 4)
	for i := range videos {
		videos[i] = storage.ChannelVideo{Title: "Video"}
	}

	summary := Analyze(videos)

	if summary.Confidence != "low" {
		t.Errorf("Confidence = %q, want %q for 4 videos", summary.Confidence, "low")
	}
}

func TestAnalyze_SetsHighConfidenceAtThreshold(t *testing.T) {
	videos := make([]storage.ChannelVideo, 5)
	for i := range videos {
		videos[i] = storage.ChannelVideo{Title: "Video"}
	}

	summary := Analyze(videos)

	if summary.Confidence != "high" {
		t.Errorf("Confidence = %q, want %q for 5 videos", summary.Confidence, "high")
	}
}

func TestAnalyze_ComputesAverageTitleLength(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Title: "1234567890"}, // 10 chars
		{Title: "12345"},      // 5 chars
	}

	summary := Analyze(videos)

	if summary.AverageTitleLength != 7.5 {
		t.Errorf("AverageTitleLength = %v, want 7.5", summary.AverageTitleLength)
	}
}

func TestAnalyze_DetectsQuestionMarksNumbersAndAllCapsInTitles(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Title: "How to code in Go?"},
		{Title: "10 tips for beginners"},
		{Title: "THIS IS HUGE news"},
		{Title: "a plain title"},
	}

	summary := Analyze(videos)

	if summary.TitlesWithQuestionMarkPct != 25 {
		t.Errorf("TitlesWithQuestionMarkPct = %v, want 25", summary.TitlesWithQuestionMarkPct)
	}
	if summary.TitlesWithNumberPct != 25 {
		t.Errorf("TitlesWithNumberPct = %v, want 25", summary.TitlesWithNumberPct)
	}
	if summary.TitlesWithAllCapsWordPct != 25 {
		t.Errorf("TitlesWithAllCapsWordPct = %v, want 25", summary.TitlesWithAllCapsWordPct)
	}
}

func TestAnalyze_ComputesTagFrequencyAndTopTags(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Tags: []string{"go", "programming"}},
		{Tags: []string{"go", "backend"}},
		{Tags: []string{"go"}},
	}

	summary := Analyze(videos)

	wantAvg := float64(2+2+1) / 3
	if summary.AverageTagsPerVideo != wantAvg {
		t.Errorf("AverageTagsPerVideo = %v, want %v", summary.AverageTagsPerVideo, wantAvg)
	}
	if len(summary.TopTags) == 0 || summary.TopTags[0] != "go" {
		t.Errorf("TopTags[0] = %v, want %q (most frequent)", summary.TopTags, "go")
	}
}

func TestAnalyze_DetectsDescriptionStructure(t *testing.T) {
	videos := []storage.ChannelVideo{
		{Description: "Intro\n0:00 Start\n1:30 Middle\n3:00 End\nCheck https://example.com and #golang"},
		{Description: "No structure here at all"},
	}

	summary := Analyze(videos)

	if summary.DescriptionsWithTimestampsPct != 50 {
		t.Errorf("DescriptionsWithTimestampsPct = %v, want 50", summary.DescriptionsWithTimestampsPct)
	}
	if summary.DescriptionsWithLinksPct != 50 {
		t.Errorf("DescriptionsWithLinksPct = %v, want 50", summary.DescriptionsWithLinksPct)
	}
	if summary.DescriptionsWithHashtagsPct != 50 {
		t.Errorf("DescriptionsWithHashtagsPct = %v, want 50", summary.DescriptionsWithHashtagsPct)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/styleanalysis/...`
Expected: FAIL — compile error, `Analyze` undefined.

- [ ] **Step 3: Write `analyze.go`**

```go
// internal/styleanalysis/analyze.go
package styleanalysis

import (
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type Summary struct {
	VideoCountAnalyzed int    `json:"video_count_analyzed"`
	Confidence         string `json:"confidence"`

	AverageTitleLength        float64 `json:"average_title_length"`
	TitlesWithQuestionMarkPct float64 `json:"titles_with_question_mark_pct"`
	TitlesWithNumberPct       float64 `json:"titles_with_number_pct"`
	TitlesWithEmojiPct        float64 `json:"titles_with_emoji_pct"`
	TitlesWithAllCapsWordPct  float64 `json:"titles_with_all_caps_word_pct"`

	AverageTagsPerVideo float64  `json:"average_tags_per_video"`
	TopTags             []string `json:"top_tags"`

	AverageDescriptionLength      float64 `json:"average_description_length"`
	DescriptionsWithTimestampsPct float64 `json:"descriptions_with_timestamps_pct"`
	DescriptionsWithLinksPct      float64 `json:"descriptions_with_links_pct"`
	DescriptionsWithHashtagsPct   float64 `json:"descriptions_with_hashtags_pct"`
}

const lowConfidenceThreshold = 5

var (
	timestampPattern = regexp.MustCompile(`\b\d{1,2}:\d{2}(:\d{2})?\b`)
	urlPattern        = regexp.MustCompile(`https?://\S+`)
	hashtagPattern    = regexp.MustCompile(`#\w+`)
)

func Analyze(videos []storage.ChannelVideo) Summary {
	n := len(videos)
	summary := Summary{VideoCountAnalyzed: n}
	if n < lowConfidenceThreshold {
		summary.Confidence = "low"
	} else {
		summary.Confidence = "high"
	}
	if n == 0 {
		return summary
	}

	var (
		totalTitleLen     int
		questionMarkCount int
		numberCount       int
		emojiCount        int
		allCapsCount      int
		totalTags         int
		tagFrequency      = map[string]int{}
		totalDescLen      int
		timestampCount    int
		linkCount         int
		hashtagCount      int
	)

	for _, v := range videos {
		totalTitleLen += len(v.Title)
		if strings.Contains(v.Title, "?") {
			questionMarkCount++
		}
		if containsDigit(v.Title) {
			numberCount++
		}
		if containsEmoji(v.Title) {
			emojiCount++
		}
		if containsAllCapsWord(v.Title) {
			allCapsCount++
		}

		totalTags += len(v.Tags)
		for _, tag := range v.Tags {
			tagFrequency[strings.ToLower(tag)]++
		}

		totalDescLen += len(v.Description)
		if len(timestampPattern.FindAllString(v.Description, -1)) >= 2 {
			timestampCount++
		}
		if urlPattern.MatchString(v.Description) {
			linkCount++
		}
		if hashtagPattern.MatchString(v.Description) {
			hashtagCount++
		}
	}

	fn := float64(n)
	summary.AverageTitleLength = float64(totalTitleLen) / fn
	summary.TitlesWithQuestionMarkPct = percentage(questionMarkCount, n)
	summary.TitlesWithNumberPct = percentage(numberCount, n)
	summary.TitlesWithEmojiPct = percentage(emojiCount, n)
	summary.TitlesWithAllCapsWordPct = percentage(allCapsCount, n)

	summary.AverageTagsPerVideo = float64(totalTags) / fn
	summary.TopTags = topTags(tagFrequency, 10)

	summary.AverageDescriptionLength = float64(totalDescLen) / fn
	summary.DescriptionsWithTimestampsPct = percentage(timestampCount, n)
	summary.DescriptionsWithLinksPct = percentage(linkCount, n)
	summary.DescriptionsWithHashtagsPct = percentage(hashtagCount, n)

	return summary
}

func percentage(count, total int) float64 {
	return float64(count) / float64(total) * 100
}

func containsDigit(s string) bool {
	for _, r := range s {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

func containsEmoji(s string) bool {
	for _, r := range s {
		if r >= 0x1F300 && r <= 0x1FAFF {
			return true
		}
	}
	return false
}

func containsAllCapsWord(s string) bool {
	for _, word := range strings.Fields(s) {
		if isAllCapsWord(word) {
			return true
		}
	}
	return false
}

func isAllCapsWord(word string) bool {
	letters := 0
	for _, r := range word {
		if unicode.IsLetter(r) {
			letters++
			if !unicode.IsUpper(r) {
				return false
			}
		}
	}
	return letters >= 2
}

func topTags(freq map[string]int, limit int) []string {
	type kv struct {
		tag   string
		count int
	}
	pairs := make([]kv, 0, len(freq))
	for tag, count := range freq {
		pairs = append(pairs, kv{tag, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].tag < pairs[j].tag
	})
	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	result := make([]string, len(pairs))
	for i, p := range pairs {
		result[i] = p.tag
	}
	return result
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/styleanalysis/... -v`
Expected: all 7 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/styleanalysis
git commit -m "feat: add channel style heuristics"
```

---

### Task 4: Storage — channel style cache

**Files:**
- Create: `internal/storage/style_cache.go`
- Test: `internal/storage/style_cache_test.go`

**Interfaces:**
- Produces: `storage.StyleCache{ChannelID string; SummaryJSON []byte; VideoCountAnalyzed int; ComputedAt, ExpiresAt time.Time}`, `storage.ErrStyleNotFound`, `(*Store) GetChannelStyle(ctx, channelID string) (StyleCache, error)`, `(*Store) UpsertChannelStyle(ctx, channelID string, summaryJSON []byte, videoCountAnalyzed int, computedAt, expiresAt time.Time) error`, `(*Store) DeleteChannelStyle(ctx, channelID string) error`

- [ ] **Step 1: Write the failing tests**

```go
// internal/storage/style_cache_test.go
package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestUpsertChannelStyle_ThenGetChannelStyle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-style-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelStyle(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelStyle() returned error: %v", err)
		}
	})

	computedAt := time.Now().Truncate(time.Second)
	expiresAt := computedAt.Add(48 * time.Hour)
	summaryJSON := []byte(`{"video_count_analyzed":10,"confidence":"high"}`)

	if err := store.UpsertChannelStyle(ctx, channelID, summaryJSON, 10, computedAt, expiresAt); err != nil {
		t.Fatalf("UpsertChannelStyle() returned unexpected error: %v", err)
	}

	found, err := store.GetChannelStyle(ctx, channelID)
	if err != nil {
		t.Fatalf("GetChannelStyle() returned unexpected error: %v", err)
	}
	if found.VideoCountAnalyzed != 10 {
		t.Errorf("VideoCountAnalyzed = %d, want 10", found.VideoCountAnalyzed)
	}
	if string(found.SummaryJSON) != string(summaryJSON) {
		t.Errorf("SummaryJSON = %s, want %s", found.SummaryJSON, summaryJSON)
	}
}

func TestUpsertChannelStyle_UpdatesOnConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-style-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelStyle(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelStyle() returned error: %v", err)
		}
	})

	now := time.Now().Truncate(time.Second)
	if err := store.UpsertChannelStyle(ctx, channelID, []byte(`{"video_count_analyzed":1}`), 1, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("UpsertChannelStyle() (first) returned unexpected error: %v", err)
	}
	if err := store.UpsertChannelStyle(ctx, channelID, []byte(`{"video_count_analyzed":2}`), 2, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("UpsertChannelStyle() (second) returned unexpected error: %v", err)
	}

	found, err := store.GetChannelStyle(ctx, channelID)
	if err != nil {
		t.Fatalf("GetChannelStyle() returned unexpected error: %v", err)
	}
	if found.VideoCountAnalyzed != 2 {
		t.Errorf("VideoCountAnalyzed = %d, want 2 (update, not duplicate)", found.VideoCountAnalyzed)
	}
}

func TestGetChannelStyle_ReturnsErrNotFoundForUnknownChannel(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetChannelStyle(context.Background(), "UC-does-not-exist-"+randomHash(t)[:8])
	if !errors.Is(err, ErrStyleNotFound) {
		t.Errorf("err = %v, want ErrStyleNotFound", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/... -run 'TestUpsertChannelStyle|TestGetChannelStyle'`
Expected: FAIL — compile error, `StyleCache`, `ErrStyleNotFound`, `GetChannelStyle`, `UpsertChannelStyle`, `DeleteChannelStyle` undefined.

- [ ] **Step 3: Write `style_cache.go`**

```go
// internal/storage/style_cache.go
package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type StyleCache struct {
	ChannelID          string
	SummaryJSON        []byte
	VideoCountAnalyzed int
	ComputedAt         time.Time
	ExpiresAt          time.Time
}

var ErrStyleNotFound = errors.New("storage: channel style not found")

func (s *Store) GetChannelStyle(ctx context.Context, channelID string) (StyleCache, error) {
	c := StyleCache{ChannelID: channelID}
	err := s.pool.QueryRow(ctx,
		`SELECT style_summary_json, video_count_analyzed, computed_at, expires_at
		 FROM channel_style_cache WHERE channel_id = $1`,
		channelID,
	).Scan(&c.SummaryJSON, &c.VideoCountAnalyzed, &c.ComputedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return StyleCache{}, ErrStyleNotFound
	}
	if err != nil {
		return StyleCache{}, err
	}
	return c, nil
}

func (s *Store) UpsertChannelStyle(ctx context.Context, channelID string, summaryJSON []byte, videoCountAnalyzed int, computedAt, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_style_cache (channel_id, style_summary_json, video_count_analyzed, computed_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (channel_id) DO UPDATE SET
			style_summary_json = EXCLUDED.style_summary_json,
			video_count_analyzed = EXCLUDED.video_count_analyzed,
			computed_at = EXCLUDED.computed_at,
			expires_at = EXCLUDED.expires_at`,
		channelID, summaryJSON, videoCountAnalyzed, computedAt, expiresAt,
	)
	return err
}

func (s *Store) DeleteChannelStyle(ctx context.Context, channelID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM channel_style_cache WHERE channel_id = $1`, channelID)
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/... -run 'TestUpsertChannelStyle|TestGetChannelStyle' -v`
Expected: all 3 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/storage/style_cache.go internal/storage/style_cache_test.go
git commit -m "feat: add channel style cache storage"
```

---

### Task 5: Style cache provider (TTL orchestration)

**Files:**
- Create: `internal/stylecache/provider.go`
- Test: `internal/stylecache/provider_test.go`

**Interfaces:**
- Consumes: `storage.ChannelVideo`, `storage.StyleCache`, `storage.ErrStyleNotFound` (Sessions 3-4); `styleanalysis.Summary`, `styleanalysis.Analyze` (Task 3)
- Produces: `stylecache.VideoLister`, `stylecache.StyleStore` interfaces, `stylecache.NewProvider(videos VideoLister, styles StyleStore, ttl time.Duration) *Provider`, `(*Provider) GetStyle(ctx, channelID string) (styleanalysis.Summary, error)`

- [ ] **Step 1: Write the failing tests**

```go
// internal/stylecache/provider_test.go
package stylecache

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type fakeVideoLister struct {
	videos []storage.ChannelVideo
	calls  int
}

func (f *fakeVideoLister) ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error) {
	f.calls++
	return f.videos, nil
}

type fakeStyleStore struct {
	cached    storage.StyleCache
	hasCached bool
	upserted  bool
}

func (f *fakeStyleStore) GetChannelStyle(ctx context.Context, channelID string) (storage.StyleCache, error) {
	if !f.hasCached {
		return storage.StyleCache{}, storage.ErrStyleNotFound
	}
	return f.cached, nil
}

func (f *fakeStyleStore) UpsertChannelStyle(ctx context.Context, channelID string, summaryJSON []byte, videoCountAnalyzed int, computedAt, expiresAt time.Time) error {
	f.upserted = true
	f.cached = storage.StyleCache{ChannelID: channelID, SummaryJSON: summaryJSON, VideoCountAnalyzed: videoCountAnalyzed, ComputedAt: computedAt, ExpiresAt: expiresAt}
	f.hasCached = true
	return nil
}

func TestGetStyle_ComputesAndCachesWhenNothingCached(t *testing.T) {
	videos := &fakeVideoLister{videos: []storage.ChannelVideo{
		{Title: "How to code in Go?", Tags: []string{"go", "programming"}},
	}}
	styles := &fakeStyleStore{}
	provider := NewProvider(videos, styles, time.Hour)

	summary, err := provider.GetStyle(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("GetStyle() returned unexpected error: %v", err)
	}
	if summary.VideoCountAnalyzed != 1 {
		t.Errorf("VideoCountAnalyzed = %d, want 1", summary.VideoCountAnalyzed)
	}
	if videos.calls != 1 {
		t.Errorf("videos.calls = %d, want 1", videos.calls)
	}
	if !styles.upserted {
		t.Error("expected style to be cached via UpsertChannelStyle")
	}
}

func TestGetStyle_ReturnsCachedValueWithoutRecomputing(t *testing.T) {
	videos := &fakeVideoLister{}
	cachedSummary := styleanalysis.Summary{VideoCountAnalyzed: 42, Confidence: "high"}
	summaryJSON, err := json.Marshal(cachedSummary)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	styles := &fakeStyleStore{
		hasCached: true,
		cached: storage.StyleCache{
			SummaryJSON: summaryJSON,
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	provider := NewProvider(videos, styles, time.Hour)

	summary, err := provider.GetStyle(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("GetStyle() returned unexpected error: %v", err)
	}
	if summary.VideoCountAnalyzed != 42 {
		t.Errorf("VideoCountAnalyzed = %d, want 42 (from cache)", summary.VideoCountAnalyzed)
	}
	if videos.calls != 0 {
		t.Errorf("videos.calls = %d, want 0 (should not recompute)", videos.calls)
	}
}

func TestGetStyle_RecomputesWhenCacheExpired(t *testing.T) {
	videos := &fakeVideoLister{videos: []storage.ChannelVideo{{Title: "Fresh video"}}}
	staleJSON, err := json.Marshal(styleanalysis.Summary{VideoCountAnalyzed: 1})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	styles := &fakeStyleStore{
		hasCached: true,
		cached: storage.StyleCache{
			SummaryJSON: staleJSON,
			ExpiresAt:   time.Now().Add(-time.Hour),
		},
	}
	provider := NewProvider(videos, styles, time.Hour)

	_, err = provider.GetStyle(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("GetStyle() returned unexpected error: %v", err)
	}
	if videos.calls != 1 {
		t.Errorf("videos.calls = %d, want 1 (should recompute since cache expired)", videos.calls)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/stylecache/...`
Expected: FAIL — compile error, `NewProvider` undefined.

- [ ] **Step 3: Write `provider.go`**

```go
// internal/stylecache/provider.go
package stylecache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type VideoLister interface {
	ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error)
}

type StyleStore interface {
	GetChannelStyle(ctx context.Context, channelID string) (storage.StyleCache, error)
	UpsertChannelStyle(ctx context.Context, channelID string, summaryJSON []byte, videoCountAnalyzed int, computedAt, expiresAt time.Time) error
}

type Provider struct {
	videos VideoLister
	styles StyleStore
	ttl    time.Duration
}

func NewProvider(videos VideoLister, styles StyleStore, ttl time.Duration) *Provider {
	return &Provider{videos: videos, styles: styles, ttl: ttl}
}

func (p *Provider) GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error) {
	cached, err := p.styles.GetChannelStyle(ctx, channelID)
	if err == nil && time.Now().Before(cached.ExpiresAt) {
		var summary styleanalysis.Summary
		if err := json.Unmarshal(cached.SummaryJSON, &summary); err != nil {
			return styleanalysis.Summary{}, err
		}
		return summary, nil
	}
	if err != nil && !errors.Is(err, storage.ErrStyleNotFound) {
		return styleanalysis.Summary{}, err
	}

	videos, err := p.videos.ListChannelVideos(ctx, channelID)
	if err != nil {
		return styleanalysis.Summary{}, err
	}

	summary := styleanalysis.Analyze(videos)

	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return styleanalysis.Summary{}, err
	}

	computedAt := time.Now()
	if err := p.styles.UpsertChannelStyle(ctx, channelID, summaryJSON, summary.VideoCountAnalyzed, computedAt, computedAt.Add(p.ttl)); err != nil {
		return styleanalysis.Summary{}, err
	}

	return summary, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/stylecache/... -v`
Expected: all 3 tests PASS (no network, no database)

- [ ] **Step 5: Commit**

```bash
git add internal/stylecache
git commit -m "feat: add style cache provider with TTL"
```

---

### Task 6: Router refactor to `Dependencies` struct, and the style endpoint

**Files:**
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`
- Create: `internal/api/channel_style.go`

**Interfaces:**
- Consumes: `styleanalysis.Summary` (Task 3)
- Produces: `api.Dependencies{Finder ClientFinder; Recorder UsageRecorder; Syncer ChannelSyncer; StyleProvider StyleProvider}`, `NewRouter(deps Dependencies) *chi.Mux` (replaces Session 3's 3-positional-argument `NewRouter`), `api.StyleProvider` interface

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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/...`
Expected: FAIL — `NewRouter(Dependencies{})` compile error (existing `NewRouter` takes three positional arguments), `StyleProvider`/`handleChannelStyle` undefined once that's fixed.

- [ ] **Step 3: Write `channel_style.go`**

```go
// internal/api/channel_style.go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type StyleProvider interface {
	GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error)
}

func handleChannelStyle(provider StyleProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := chi.URLParam(r, "channelID")
		if channelID == "" {
			writeJSONError(w, http.StatusBadRequest, "channel id is required")
			return
		}

		summary, err := provider.GetStyle(r.Context(), channelID)
		if err != nil {
			log.Printf("channel style: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to compute channel style")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
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
}

func NewRouter(deps Dependencies) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)

	r.Group(func(r chi.Router) {
		r.Use(RequireAPIKey(deps.Finder, deps.Recorder))
		r.Get("/v1/whoami", handleWhoami)
		r.Post("/v1/internal/channels/{channelID}/sync", handleChannelSync(deps.Syncer))
		r.Get("/v1/internal/channels/{channelID}/style", handleChannelStyle(deps.StyleProvider))
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
git commit -m "feat: refactor router to Dependencies struct, add style endpoint"
```

---

### Task 7: Wire `cmd/api/main.go` and verify end-to-end

**Files:**
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: `stylecache.NewProvider`, `api.Dependencies` (Tasks 5-6)

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

	router := api.NewRouter(api.Dependencies{
		Finder:        store,
		Recorder:      store,
		Syncer:        syncer,
		StyleProvider: styleProvider,
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

Expected: no build errors; all tests PASS (with `DATABASE_URL` exported)

- [ ] **Step 3: Run the server**

```bash
export PORT=8081
go run ./cmd/api
```

- [ ] **Step 4: Fetch the style summary for the channel synced in Session 3**

```bash
curl -s http://localhost:8081/v1/internal/channels/UC_x5XG1OV2P6uZZ5FSM9Ttw/style \
  -H "Authorization: Bearer <your key>" | python3 -m json.tool
```

Expected: `200 OK` with a JSON body showing `video_count_analyzed: 25`, `confidence: "high"`, and populated title/tag/description stats.

- [ ] **Step 5: Call it again immediately and confirm it's cached**

```bash
psql "$DATABASE_URL" -c "SELECT channel_id, computed_at, expires_at FROM channel_style_cache;"
```

Then repeat the Step 4 curl. Expected: the response is identical and `computed_at` in the table does not change between the two calls (proving the second call hit the cache instead of recomputing). Stop the server (Ctrl+C) after confirming.

- [ ] **Step 6: Commit**

```bash
git add cmd/api
git commit -m "feat: wire style cache provider into main"
```

---

## Session 4 done when

- `go test ./...` passes locally (with `DATABASE_URL` exported)
- `GET /v1/internal/channels/{channelID}/style` for a previously-synced channel returns a populated style summary
- The summary is persisted in `channel_style_cache` with a `48h`-out `expires_at` (or your configured TTL)
- A second call within the TTL window returns the same `computed_at`, proving the cache is used instead of recomputing
- A channel with fewer than 5 cached videos gets `confidence: "low"` in its summary
