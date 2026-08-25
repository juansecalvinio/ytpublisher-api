package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type Client struct {
	ID       string
	Name     string
	Email    string
	IsActive bool
}

var ErrClientNotFound = errors.New("storage: client not found")

func (s *Store) CreateClient(ctx context.Context, name, email, apiKeyHash string) (Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_clients (name, email, api_key_hash) VALUES ($1, $2, $3)
		 RETURNING id, name, email, is_active`,
		name, email, apiKeyHash,
	).Scan(&c.ID, &c.Name, &c.Email, &c.IsActive)
	if err != nil {
		return Client{}, err
	}
	return c, nil
}

func (s *Store) FindClientByAPIKeyHash(ctx context.Context, apiKeyHash string) (Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, email, is_active FROM api_clients WHERE api_key_hash = $1 AND is_active = true`,
		apiKeyHash,
	).Scan(&c.ID, &c.Name, &c.Email, &c.IsActive)
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrClientNotFound
	}
	if err != nil {
		return Client{}, err
	}
	return c, nil
}

func (s *Store) DeleteClient(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM api_clients WHERE id = $1`, id)
	return err
}
