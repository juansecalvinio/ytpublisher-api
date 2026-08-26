package storage

import (
	"context"
	"encoding/json"
	"time"
)

type ChannelVideo struct {
	ChannelID   string
	VideoID     string
	Title       string
	Description string
	Tags        []string
	PublishedAt time.Time
}

func (s *Store) UpsertChannelVideos(ctx context.Context, channelID string, videos []ChannelVideo) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	for _, v := range videos {
		tagsJSON, err := json.Marshal(v.Tags)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO channel_videos (channel_id, video_id, title, description, tags_json, published_at, fetched_at)
			 VALUES ($1, $2, $3, $4, $5, $6, now())
			 ON CONFLICT (channel_id, video_id) DO UPDATE SET
				title = EXCLUDED.title,
				description = EXCLUDED.description,
				tags_json = EXCLUDED.tags_json,
				published_at = EXCLUDED.published_at,
				fetched_at = now()`,
			channelID, v.VideoID, v.Title, v.Description, tagsJSON, v.PublishedAt,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) ListChannelVideos(ctx context.Context, channelID string) ([]ChannelVideo, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT video_id, title, description, tags_json, published_at
		 FROM channel_videos WHERE channel_id = $1 ORDER BY published_at DESC`,
		channelID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var videos []ChannelVideo
	for rows.Next() {
		var v ChannelVideo
		var tagsJSON []byte
		if err := rows.Scan(&v.VideoID, &v.Title, &v.Description, &tagsJSON, &v.PublishedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tagsJSON, &v.Tags); err != nil {
			return nil, err
		}
		v.ChannelID = channelID
		videos = append(videos, v)
	}
	return videos, rows.Err()
}

func (s *Store) DeleteChannelVideos(ctx context.Context, channelID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM channel_videos WHERE channel_id = $1`, channelID)
	return err
}
