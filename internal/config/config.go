package config

import (
	"errors"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
}

var ErrMissingDatabaseURL = errors.New("config: DATABASE_URL is required")

func Load() (Config, error) {
	cfg := Config{
		Port:        os.Getenv("PORT"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.DatabaseURL == "" {
		return Config{}, ErrMissingDatabaseURL
	}
	return cfg, nil
}
