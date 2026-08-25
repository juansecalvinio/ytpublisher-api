package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type fakeClientFinder struct {
	clientsByHash map[string]storage.Client
}

func (f *fakeClientFinder) FindClientByAPIKeyHash(ctx context.Context, hash string) (storage.Client, error) {
	c, ok := f.clientsByHash[hash]
	if !ok {
		return storage.Client{}, storage.ErrClientNotFound
	}
	return c, nil
}

type fakeUsageRecorder struct {
	events []storage.UsageEvent
}

func (f *fakeUsageRecorder) InsertUsageEvent(ctx context.Context, event storage.UsageEvent) error {
	f.events = append(f.events, event)
	return nil
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAPIKey_RejectsMissingHeader(t *testing.T) {
	handler := RequireAPIKey(&fakeClientFinder{}, &fakeUsageRecorder{})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAPIKey_RejectsMalformedHeader(t *testing.T) {
	handler := RequireAPIKey(&fakeClientFinder{}, &fakeUsageRecorder{})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "not-bearer-format")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAPIKey_RejectsUnknownKey(t *testing.T) {
	handler := RequireAPIKey(&fakeClientFinder{clientsByHash: map[string]storage.Client{}}, &fakeUsageRecorder{})(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer ytpub_wrongkey")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRequireAPIKey_AcceptsValidKeyAndRecordsUsage(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	handler := RequireAPIKey(finder, recorder)(okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header not set")
	}
	if len(recorder.events) != 1 {
		t.Fatalf("len(recorder.events) = %d, want 1", len(recorder.events))
	}
	if recorder.events[0].ClientID != client.ID {
		t.Errorf("events[0].ClientID = %q, want %q", recorder.events[0].ClientID, client.ID)
	}
}
