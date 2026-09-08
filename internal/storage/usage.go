package storage

import "context"

type UsageEvent struct {
	ClientID  string
	RequestID string
	Endpoint  string
}

type UsageUpdate struct {
	LLMInputTokens   int64
	LLMOutputTokens  int64
	EmbeddingCalls   int
	EstimatedCostUSD float64
}

func (s *Store) InsertUsageEvent(ctx context.Context, event UsageEvent) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage_events (client_id, request_id, endpoint) VALUES ($1, $2, $3)`,
		event.ClientID, event.RequestID, event.Endpoint,
	)
	return err
}

func (s *Store) UpdateUsageEvent(ctx context.Context, requestID string, update UsageUpdate) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE usage_events
		 SET llm_input_tokens = $2, llm_output_tokens = $3, embedding_calls = $4, estimated_cost_usd = $5
		 WHERE request_id = $1`,
		requestID, update.LLMInputTokens, update.LLMOutputTokens, update.EmbeddingCalls, update.EstimatedCostUSD,
	)
	return err
}
