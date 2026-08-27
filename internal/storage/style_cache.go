package storage

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type StyleCache struct {
	ChannelID          string
	SummaryJSON        []byte
	VideoCountAnalyzed int
	ComputedAt         time.Time
	ExpiresAt          time.Time
}

var ErrStyleNotFound = errors.New("storage: channel style not found")

func (s *Store) GetChannelStyle(ctx context.Context, channelID string) (StyleCache, error) {
	c := StyleCache{ChannelID: channelID}
	err := s.pool.QueryRow(ctx,
		`SELECT style_summary_json, video_count_analyzed, computed_at, expires_at
		 FROM channel_style_cache WHERE channel_id = $1`,
		channelID,
	).Scan(&c.SummaryJSON, &c.VideoCountAnalyzed, &c.ComputedAt, &c.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return StyleCache{}, ErrStyleNotFound
	}
	if err != nil {
		return StyleCache{}, err
	}
	return c, nil
}

func (s *Store) UpsertChannelStyle(ctx context.Context, channelID string, summaryJSON []byte, videoCountAnalyzed int, computedAt, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO channel_style_cache (channel_id, style_summary_json, video_count_analyzed, computed_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (channel_id) DO UPDATE SET
			style_summary_json = EXCLUDED.style_summary_json,
			video_count_analyzed = EXCLUDED.video_count_analyzed,
			computed_at = EXCLUDED.computed_at,
			expires_at = EXCLUDED.expires_at`,
		channelID, summaryJSON, videoCountAnalyzed, computedAt, expiresAt,
	)
	return err
}

func (s *Store) DeleteChannelStyle(ctx context.Context, channelID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM channel_style_cache WHERE channel_id = $1`, channelID)
	return err
}
