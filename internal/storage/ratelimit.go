package storage

import "context"

func (s *Store) IncrementDailyGenerateCount(ctx context.Context, clientID string) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`INSERT INTO client_daily_usage (client_id, date, request_count)
		 VALUES ($1, CURRENT_DATE, 1)
		 ON CONFLICT (client_id, date)
		 DO UPDATE SET request_count = client_daily_usage.request_count + 1
		 RETURNING request_count`,
		clientID,
	).Scan(&count)
	return count, err
}
