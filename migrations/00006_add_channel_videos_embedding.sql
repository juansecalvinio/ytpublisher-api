-- +goose Up
CREATE EXTENSION IF NOT EXISTS vector;
ALTER TABLE channel_videos ADD COLUMN embedding vector(1024);

-- +goose Down
ALTER TABLE channel_videos DROP COLUMN embedding;
DROP EXTENSION IF EXISTS vector;
