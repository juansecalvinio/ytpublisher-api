package storage

import (
	"context"
	"testing"
)

func TestIncrementDailyGenerateCount_AccumulatesAcrossCalls(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	client, err := store.CreateClient(ctx, "Rate Limit Test Client", "ratelimit-test@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), client.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})
	// Registered after DeleteClient, so it runs first (LIFO), avoiding a
	// foreign key violation.
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), `DELETE FROM client_daily_usage WHERE client_id = $1`, client.ID); err != nil {
			t.Errorf("cleanup delete client_daily_usage returned error: %v", err)
		}
	})

	first, err := store.IncrementDailyGenerateCount(ctx, client.ID)
	if err != nil {
		t.Fatalf("IncrementDailyGenerateCount() returned unexpected error: %v", err)
	}
	if first != 1 {
		t.Errorf("first count = %d, want 1", first)
	}

	second, err := store.IncrementDailyGenerateCount(ctx, client.ID)
	if err != nil {
		t.Fatalf("IncrementDailyGenerateCount() (second call) returned unexpected error: %v", err)
	}
	if second != 2 {
		t.Errorf("second count = %d, want 2", second)
	}
}

func TestIncrementDailyGenerateCount_TracksClientsIndependently(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	clientA, err := store.CreateClient(ctx, "Rate Limit Client A", "ratelimit-a@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() (A) returned unexpected error: %v", err)
	}
	clientB, err := store.CreateClient(ctx, "Rate Limit Client B", "ratelimit-b@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() (B) returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), clientA.ID); err != nil {
			t.Errorf("cleanup DeleteClient(A) returned error: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), clientB.ID); err != nil {
			t.Errorf("cleanup DeleteClient(B) returned error: %v", err)
		}
	})
	// Registered after both DeleteClient cleanups, so it runs first (LIFO).
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), `DELETE FROM client_daily_usage WHERE client_id IN ($1, $2)`, clientA.ID, clientB.ID); err != nil {
			t.Errorf("cleanup delete client_daily_usage returned error: %v", err)
		}
	})

	if _, err := store.IncrementDailyGenerateCount(ctx, clientA.ID); err != nil {
		t.Fatalf("IncrementDailyGenerateCount(A) returned unexpected error: %v", err)
	}

	countB, err := store.IncrementDailyGenerateCount(ctx, clientB.ID)
	if err != nil {
		t.Fatalf("IncrementDailyGenerateCount(B) returned unexpected error: %v", err)
	}
	if countB != 1 {
		t.Errorf("clientB count = %d, want 1 (independent from clientA)", countB)
	}
}
