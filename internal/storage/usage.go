package storage

import "context"

type UsageEvent struct {
	ClientID  string
	RequestID string
	Endpoint  string
}

func (s *Store) InsertUsageEvent(ctx context.Context, event UsageEvent) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage_events (client_id, request_id, endpoint) VALUES ($1, $2, $3)`,
		event.ClientID, event.RequestID, event.Endpoint,
	)
	return err
}
