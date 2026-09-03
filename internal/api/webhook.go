package api

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/billing"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type ClientProvisioner interface {
	FindClientByStripeCustomerID(ctx context.Context, stripeCustomerID string) (storage.Client, error)
	CreateClient(ctx context.Context, name, email, apiKeyHash, stripeCustomerID string) (storage.Client, error)
}

type KeyMailer interface {
	SendAPIKeyEmail(ctx context.Context, toEmail, toName, apiKey string) error
}

func handleStripeWebhook(provisioner ClientProvisioner, mailer KeyMailer, webhookSecret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "failed to read request body")
			return
		}

		event, err := billing.ParseWebhookEvent(payload, r.Header.Get("Stripe-Signature"), webhookSecret)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid signature")
			return
		}

		if event.Type != "checkout.session.completed" {
			w.WriteHeader(http.StatusOK)
			return
		}

		completed, err := billing.ParseCheckoutCompleted(event.Data)
		if err != nil {
			log.Printf("webhook: failed to parse checkout.session.completed: %v", err)
			writeJSONError(w, http.StatusBadRequest, "malformed event payload")
			return
		}

		_, err = provisioner.FindClientByStripeCustomerID(r.Context(), completed.CustomerID)
		if err == nil {
			// Already provisioned — Stripe retried this event. Idempotent no-op.
			w.WriteHeader(http.StatusOK)
			return
		}
		if !errors.Is(err, storage.ErrClientNotFound) {
			log.Printf("webhook: lookup failed: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "lookup failed")
			return
		}

		plainKey, err := apikey.Generate()
		if err != nil {
			log.Printf("webhook: failed to generate API key: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to provision client")
			return
		}

		client, err := provisioner.CreateClient(r.Context(), completed.Name, completed.Email, apikey.Hash(plainKey), completed.CustomerID)
		if err != nil {
			log.Printf("webhook: failed to create client: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to provision client")
			return
		}

		if err := mailer.SendAPIKeyEmail(r.Context(), client.Email, client.Name, plainKey); err != nil {
			log.Printf("webhook: failed to email API key to %s: %v", client.Email, err)
			// The client row and key already exist — don't fail the webhook
			// (Stripe would retry and we'd create a second client, since the
			// idempotency check above only prevents duplicate CreateClient
			// calls, not duplicate-attempt-with-email-failure loops). Session 9
			// or a support request is the practical recovery path for now.
		}

		w.WriteHeader(http.StatusOK)
	}
}
