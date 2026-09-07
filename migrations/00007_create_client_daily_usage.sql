-- +goose Up
CREATE TABLE client_daily_usage (
    client_id UUID NOT NULL REFERENCES api_clients(id),
    date DATE NOT NULL,
    request_count INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (client_id, date)
);

-- +goose Down
DROP TABLE client_daily_usage;
