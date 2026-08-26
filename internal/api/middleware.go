package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type ClientFinder interface {
	FindClientByAPIKeyHash(ctx context.Context, hash string) (storage.Client, error)
}

type UsageRecorder interface {
	InsertUsageEvent(ctx context.Context, event storage.UsageEvent) error
}

type contextKey int

const clientContextKey contextKey = iota

func ClientFromContext(ctx context.Context) (storage.Client, bool) {
	c, ok := ctx.Value(clientContextKey).(storage.Client)
	return c, ok
}

func RequireAPIKey(finder ClientFinder, recorder UsageRecorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key, ok := bearerToken(r.Header.Get("Authorization"))
			if !ok {
				writeJSONError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}

			client, err := finder.FindClientByAPIKeyHash(r.Context(), apikey.Hash(key))
			if err != nil {
				if !errors.Is(err, storage.ErrClientNotFound) {
					log.Printf("auth: lookup failed: %v", err)
				}
				writeJSONError(w, http.StatusUnauthorized, "invalid or missing API key")
				return
			}

			requestID := NewRequestID()
			w.Header().Set("X-Request-Id", requestID)

			if err := recorder.InsertUsageEvent(r.Context(), storage.UsageEvent{
				ClientID:  client.ID,
				RequestID: requestID,
				Endpoint:  r.URL.Path,
			}); err != nil {
				log.Printf("auth: failed to record usage event: %v", err)
			}

			ctx := context.WithValue(r.Context(), clientContextKey, client)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if token == "" {
		return "", false
	}
	return token, true
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
