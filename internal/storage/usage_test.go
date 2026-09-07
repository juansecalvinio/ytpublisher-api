package storage

import (
	"context"
	"testing"
)

func TestInsertUsageEvent_Succeeds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	client, err := store.CreateClient(ctx, "Usage Test Client", "usage-test@example.com", randomHash(t), "")
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

func TestUpdateUsageEvent_UpdatesCostFields(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	client, err := store.CreateClient(ctx, "Usage Update Test Client", "usage-update-test@example.com", randomHash(t), "")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), client.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})
	requestID := randomHash(t)
	if err := store.InsertUsageEvent(ctx, UsageEvent{ClientID: client.ID, RequestID: requestID, Endpoint: "/v1/generate"}); err != nil {
		t.Fatalf("InsertUsageEvent() returned unexpected error: %v", err)
	}
	// Registered after the DeleteClient cleanup above, so it runs first
	// (t.Cleanup is LIFO), avoiding a foreign key violation.
	t.Cleanup(func() {
		if _, err := store.pool.Exec(context.Background(), `DELETE FROM usage_events WHERE client_id = $1`, client.ID); err != nil {
			t.Errorf("cleanup delete usage_events returned error: %v", err)
		}
	})

	err = store.UpdateUsageEvent(ctx, requestID, UsageUpdate{
		LLMInputTokens:   1000,
		LLMOutputTokens:  500,
		EmbeddingCalls:   2,
		EstimatedCostUSD: 0.007,
	})
	if err != nil {
		t.Fatalf("UpdateUsageEvent() returned unexpected error: %v", err)
	}

	var inputTokens, outputTokens, embeddingCalls int
	var cost float64
	err = store.pool.QueryRow(ctx,
		`SELECT llm_input_tokens, llm_output_tokens, embedding_calls, estimated_cost_usd FROM usage_events WHERE request_id = $1`,
		requestID,
	).Scan(&inputTokens, &outputTokens, &embeddingCalls, &cost)
	if err != nil {
		t.Fatalf("querying updated row returned unexpected error: %v", err)
	}
	if inputTokens != 1000 || outputTokens != 500 || embeddingCalls != 2 {
		t.Errorf("got (%d, %d, %d), want (1000, 500, 2)", inputTokens, outputTokens, embeddingCalls)
	}
	if cost != 0.007 {
		t.Errorf("cost = %v, want 0.007", cost)
	}
}
