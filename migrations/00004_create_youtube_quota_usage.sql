-- +goose Up
CREATE TABLE youtube_quota_usage (
    date DATE PRIMARY KEY,
    units_used INTEGER NOT NULL DEFAULT 0
);

-- +goose Down
DROP TABLE youtube_quota_usage;
