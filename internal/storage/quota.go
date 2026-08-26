package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *Store) UnitsUsedToday(ctx context.Context) (int, error) {
	var units int
	err := s.pool.QueryRow(ctx,
		`SELECT units_used FROM youtube_quota_usage WHERE date = CURRENT_DATE`,
	).Scan(&units)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return units, nil
}

func (s *Store) IncrementUnitsUsed(ctx context.Context, units int) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO youtube_quota_usage (date, units_used) VALUES (CURRENT_DATE, $1)
		 ON CONFLICT (date) DO UPDATE SET units_used = youtube_quota_usage.units_used + $1`,
		units,
	)
	return err
}
