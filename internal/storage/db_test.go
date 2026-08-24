package storage

import (
	"context"
	"os"
	"testing"
)

func TestNewPool_ConnectsAndPings(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test against Supabase")
	}

	pool, err := NewPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		t.Errorf("Ping() after NewPool() returned error: %v", err)
	}
}

func TestNewPool_ReturnsErrorForInvalidConfig(t *testing.T) {
	_, err := NewPool(context.Background(), "postgres://user:pass@localhost:5432/db?sslmode=notarealmode")
	if err == nil {
		t.Error("expected error for invalid sslmode, got nil")
	}
}
