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

func Load() (Config, error) {
	cfg := Config{
		Port:            os.Getenv("PORT"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		YouTubeAPIKey:   os.Getenv("YOUTUBE_API_KEY"),
		VoyageAPIKey:    os.Getenv("VOYAGE_API_KEY"),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
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
	if cfg.AnthropicAPIKey == "" {
		return Config{}, ErrMissingAnthropicAPIKey
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

	cfg.AnthropicModel = os.Getenv("ANTHROPIC_MODEL")
	if cfg.AnthropicModel == "" {
		cfg.AnthropicModel = defaultAnthropicModel
	}

	cfg.StripeSecretKey = os.Getenv("STRIPE_SECRET_KEY")
	if cfg.StripeSecretKey == "" {
		return Config{}, ErrMissingStripeSecretKey
	}

	cfg.StripeWebhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	if cfg.StripeWebhookSecret == "" {
		return Config{}, ErrMissingStripeWebhookSecret
	}

	cfg.StripeMeteredPriceID = os.Getenv("STRIPE_METERED_PRICE_ID")
	if cfg.StripeMeteredPriceID == "" {
		return Config{}, ErrMissingStripeMeteredPriceID
	}

	cfg.ResendAPIKey = os.Getenv("RESEND_API_KEY")
	if cfg.ResendAPIKey == "" {
		return Config{}, ErrMissingResendAPIKey
	}

	cfg.ResendFromEmail = os.Getenv("RESEND_FROM_EMAIL")
	if cfg.ResendFromEmail == "" {
		cfg.ResendFromEmail = defaultResendFromEmail
	}

	cfg.PublicBaseURL = os.Getenv("PUBLIC_BASE_URL")
	if cfg.PublicBaseURL == "" {
		return Config{}, ErrMissingPublicBaseURL
	}

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
