package storage

import (
	"context"
	"testing"
)

func TestInsertUsageEvent_Succeeds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	client, err := store.CreateClient(ctx, "Usage Test Client", "usage-test@example.com", randomHash(t))
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), client.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	err = store.InsertUsageEvent(ctx, UsageEvent{
		ClientID:  client.ID,
		RequestID: randomHash(t),
		Endpoint:  "/v1/whoami",
	})
	if err != nil {
		t.Errorf("InsertUsageEvent() returned unexpected error: %v", err)
	}
	// Registered after the DeleteClient cleanup above, so it runs first
	// (t.Cleanup is LIFO) and avoids violating the usage_events -> api_clients
	// foreign key when the client is deleted.
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(),
			`DELETE FROM usage_events WHERE client_id = $1`, client.ID); err != nil {
			t.Errorf("cleanup delete usage_events returned error: %v", err)
		}
	})
}
