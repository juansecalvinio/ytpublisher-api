package api

import (
	"context"
	"log"
	"net/http"
)

type MinuteLimiter interface {
	Allow(clientID string) bool
}

type DailyLimiter interface {
	IncrementDailyGenerateCount(ctx context.Context, clientID string) (int, error)
}

func RequireRateLimit(minuteLimiter MinuteLimiter, dailyLimiter DailyLimiter, dailyCap int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client, ok := ClientFromContext(r.Context())
			if !ok {
				writeJSONError(w, http.StatusInternalServerError, "internal error: client not found in context")
				return
			}

			if !minuteLimiter.Allow(client.ID) {
				writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded: too many requests per minute")
				return
			}

			count, err := dailyLimiter.IncrementDailyGenerateCount(r.Context(), client.ID)
			if err != nil {
				log.Printf("ratelimit: failed to increment daily count: %v", err)
			} else if count > dailyCap {
				writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded: too many requests per day")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
