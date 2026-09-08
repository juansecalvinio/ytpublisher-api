package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type fakeMinuteLimiter struct {
	allow bool
}

func (f *fakeMinuteLimiter) Allow(clientID string) bool {
	return f.allow
}

type fakeDailyLimiter struct {
	count int
	err   error
}

func (f *fakeDailyLimiter) IncrementDailyGenerateCount(ctx context.Context, clientID string) (int, error) {
	return f.count, f.err
}

func withClient(r *http.Request, client storage.Client) *http.Request {
	ctx := context.WithValue(r.Context(), clientContextKey, client)
	return r.WithContext(ctx)
}

func TestRequireRateLimit_AllowsWhenUnderBothLimits(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{count: 5}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRequireRateLimit_RejectsWhenPerMinuteLimitExceeded(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: false}, &fakeDailyLimiter{count: 5}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRequireRateLimit_RejectsWhenDailyCapExceeded(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{count: 201}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
}

func TestRequireRateLimit_AllowsExactlyAtDailyCap(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{count: 200}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (count == cap is still allowed; cap+1 is what's rejected)", rec.Code, http.StatusOK)
	}
}

func TestRequireRateLimit_AllowsWhenDailyIncrementErrors(t *testing.T) {
	handler := RequireRateLimit(&fakeMinuteLimiter{allow: true}, &fakeDailyLimiter{err: errors.New("db down")}, 200)(okHandler())

	req := withClient(httptest.NewRequest(http.MethodPost, "/v1/generate", nil), storage.Client{ID: "client-1"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (a tracking failure should not block the request)", rec.Code, http.StatusOK)
	}
}
