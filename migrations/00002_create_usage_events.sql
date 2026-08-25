-- +goose Up
CREATE TABLE usage_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id UUID NOT NULL REFERENCES api_clients(id),
    request_id TEXT NOT NULL,
    endpoint TEXT NOT NULL,
    youtube_units_used INTEGER NOT NULL DEFAULT 0,
    embedding_calls INTEGER NOT NULL DEFAULT 0,
    llm_input_tokens INTEGER NOT NULL DEFAULT 0,
    llm_output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd NUMERIC(10,6) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_usage_events_client_id ON usage_events(client_id);

-- +goose Down
DROP TABLE usage_events;
