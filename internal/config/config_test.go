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
