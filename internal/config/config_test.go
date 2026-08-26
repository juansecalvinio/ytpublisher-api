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
