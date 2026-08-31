package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
)

func randomHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read() returned unexpected error: %v", err)
	}
	return hex.EncodeToString(b)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test against Supabase")
	}
	pool, err := NewPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewStore(pool)
}

func TestCreateClient_ThenFindByAPIKeyHash(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hash := randomHash(t)

	created, err := store.CreateClient(ctx, "Test Client", "test@example.com", hash, "")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), created.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	if !created.IsActive {
		t.Error("created.IsActive = false, want true")
	}

	found, err := store.FindClientByAPIKeyHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindClientByAPIKeyHash() returned unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, created.ID)
	}
	if found.Name != "Test Client" {
		t.Errorf("found.Name = %q, want %q", found.Name, "Test Client")
	}
}

func TestFindClientByAPIKeyHash_ReturnsErrNotFoundForUnknownHash(t *testing.T) {
	store := newTestStore(t)

	_, err := store.FindClientByAPIKeyHash(context.Background(), randomHash(t))
	if !errors.Is(err, ErrClientNotFound) {
		t.Errorf("err = %v, want ErrClientNotFound", err)
	}
}

func TestCreateClient_StoresStripeCustomerID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hash := randomHash(t)

	created, err := store.CreateClient(ctx, "Stripe Client", "stripe@example.com", hash, "cus_test123")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), created.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	if created.StripeCustomerID != "cus_test123" {
		t.Errorf("created.StripeCustomerID = %q, want %q", created.StripeCustomerID, "cus_test123")
	}

	found, err := store.FindClientByAPIKeyHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindClientByAPIKeyHash() returned unexpected error: %v", err)
	}
	if found.StripeCustomerID != "cus_test123" {
		t.Errorf("found.StripeCustomerID = %q, want %q", found.StripeCustomerID, "cus_test123")
	}
}

func TestFindClientByStripeCustomerID_ReturnsClient(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hash := randomHash(t)

	created, err := store.CreateClient(ctx, "Stripe Client 2", "stripe2@example.com", hash, "cus_test456")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), created.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	found, err := store.FindClientByStripeCustomerID(ctx, "cus_test456")
	if err != nil {
		t.Fatalf("FindClientByStripeCustomerID() returned unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, created.ID)
	}
}

func TestFindClientByStripeCustomerID_ReturnsErrNotFoundForUnknownID(t *testing.T) {
	store := newTestStore(t)

	_, err := store.FindClientByStripeCustomerID(context.Background(), "cus_does_not_exist")
	if !errors.Is(err, ErrClientNotFound) {
		t.Errorf("err = %v, want ErrClientNotFound", err)
	}
}
