-- +goose Up
CREATE TABLE channel_style_cache (
    channel_id TEXT PRIMARY KEY,
    style_summary_json JSONB NOT NULL,
    video_count_analyzed INTEGER NOT NULL,
    computed_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

-- +goose Down
DROP TABLE channel_style_cache;
