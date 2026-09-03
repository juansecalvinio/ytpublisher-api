package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type Client struct {
	ID               string
	Name             string
	Email            string
	IsActive         bool
	StripeCustomerID string
}

var ErrClientNotFound = errors.New("storage: client not found")

func (s *Store) CreateClient(ctx context.Context, name, email, apiKeyHash, stripeCustomerID string) (Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx,
		`INSERT INTO api_clients (name, email, api_key_hash, stripe_customer_id) VALUES ($1, $2, $3, NULLIF($4, ''))
		 RETURNING id, name, email, is_active, COALESCE(stripe_customer_id, '')`,
		name, email, apiKeyHash, stripeCustomerID,
	).Scan(&c.ID, &c.Name, &c.Email, &c.IsActive, &c.StripeCustomerID)
	if err != nil {
		return Client{}, err
	}
	return c, nil
}

func (s *Store) FindClientByAPIKeyHash(ctx context.Context, apiKeyHash string) (Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, email, is_active, COALESCE(stripe_customer_id, '') FROM api_clients WHERE api_key_hash = $1 AND is_active = true`,
		apiKeyHash,
	).Scan(&c.ID, &c.Name, &c.Email, &c.IsActive, &c.StripeCustomerID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Client{}, ErrClientNotFound
	}
	if err != nil {
		return Client{}, err
	}
	return c, nil
}

func (s *Store) FindClientByStripeCustomerID(ctx context.Context, stripeCustomerID string) (Client, error) {
	var c Client
	err := s.pool.QueryRow(ctx,
		`SELECT id, name, email, is_active, COALESCE(stripe_customer_id, '') FROM api_clients WHERE stripe_customer_id = $1`,
		stripeCustomerID,
	).Scan(&c.ID, &c.Name, &c.Email, &c.IsActive, &c.StripeCustomerID)
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
