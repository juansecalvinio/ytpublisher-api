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
