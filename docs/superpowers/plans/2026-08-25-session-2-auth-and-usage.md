# Session 2: API Key Auth, Usage Tracking, Key Issuance CLI — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Protect the API with API-key authentication, record a usage event per authenticated request, and add a CLI to issue new client API keys — building on Session 1's scaffold (config, router, Postgres pool, `api_clients` table).

**Architecture:** A new `internal/apikey` package handles key generation/hashing. `internal/storage` gains a `Store` type with methods for client lookup/creation and usage recording. `internal/api` gains an auth middleware that depends only on two small interfaces (`ClientFinder`, `UsageRecorder`) satisfied by `*storage.Store` — so the middleware is unit-tested with fakes, no real database needed. A new `cmd/issuekey` binary is the operator-facing tool for creating clients.

**Tech Stack:** Same as Session 1 (Go 1.22+, chi, pgx/v5, goose). No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-08-24-ytpublisher-api-v1-design.md`

## Global Constraints

- Go version: 1.22+
- Module path: `github.com/juansecalvinio/ytpublisher-api`
- Clients authenticate with `Authorization: Bearer <key>`
- API keys are prefixed `ytpub_`, generated from 24 bytes of `crypto/rand`, and stored only as a SHA-256 hash (`api_key_hash`) — the plaintext key is shown once, at issuance, and never persisted
- External dependencies (DB) are consumed by `internal/api` through small interfaces defined in that package, not concrete types — same principle as Session 1's YouTube/Voyage/Claude client design in the spec

---

## Prerequisites

- Session 1 merged (config, router, storage, `api_clients` migration all present in `main`)
- `DATABASE_URL` exported in your shell (Supabase session pooler string, `sslmode=require`) for the integration tests and manual verification steps
- `goose` available at `~/go/bin/goose` (installed in Session 1)

---

### Task 1: Migration — `usage_events` table

**Files:**
- Create: `migrations/00002_create_usage_events.sql`

**Interfaces:**
- Produces: `usage_events` table (columns: `id`, `client_id` FK → `api_clients.id`, `request_id`, `endpoint`, `youtube_units_used`, `embedding_calls`, `llm_input_tokens`, `llm_output_tokens`, `estimated_cost_usd`, `created_at`)

- [ ] **Step 1: Write the migration**

```sql
-- migrations/00002_create_usage_events.sql

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
```

- [ ] **Step 2: Apply it**

```bash
~/go/bin/goose -dir migrations postgres "$DATABASE_URL" up
```

Expected: `OK   00002_create_usage_events.sql`

- [ ] **Step 3: Verify**

```bash
psql "$DATABASE_URL" -c "\d usage_events"
```

Expected: lists the 10 columns defined above, with a foreign-key constraint on `client_id`.

- [ ] **Step 4: Commit**

```bash
git add migrations/00002_create_usage_events.sql
git commit -m "feat: add usage_events migration"
```

---

### Task 2: API key generation and hashing

**Files:**
- Create: `internal/apikey/apikey.go`
- Test: `internal/apikey/apikey_test.go`

**Interfaces:**
- Produces: `apikey.Generate() (string, error)`, `apikey.Hash(key string) string`

- [ ] **Step 1: Write the failing tests**

```go
// internal/apikey/apikey_test.go
package apikey

import (
	"strings"
	"testing"
)

func TestGenerate_HasExpectedPrefixAndLength(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if !strings.HasPrefix(key, "ytpub_") {
		t.Errorf("key = %q, want prefix %q", key, "ytpub_")
	}
	if len(key) != len("ytpub_")+48 {
		t.Errorf("len(key) = %d, want %d", len(key), len("ytpub_")+48)
	}
}

func TestGenerate_ProducesDistinctKeys(t *testing.T) {
	key1, err := Generate()
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	key2, err := Generate()
	if err != nil {
		t.Fatalf("Generate() returned unexpected error: %v", err)
	}
	if key1 == key2 {
		t.Error("Generate() returned the same key twice")
	}
}

func TestHash_IsDeterministic(t *testing.T) {
	if Hash("abc") != Hash("abc") {
		t.Error("Hash() is not deterministic for the same input")
	}
}

func TestHash_DiffersForDifferentInput(t *testing.T) {
	if Hash("abc") == Hash("xyz") {
		t.Error("Hash() produced the same output for different input")
	}
}

func TestHash_MatchesKnownSHA256Vector(t *testing.T) {
	got := Hash("")
	want := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b85"
	if got != want {
		t.Errorf("Hash(\"\") = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/apikey/...`
Expected: FAIL — compile error, `Generate` and `Hash` undefined.

- [ ] **Step 3: Write the minimal implementation**

```go
// internal/apikey/apikey.go
package apikey

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

const prefix = "ytpub_"

func Generate() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(b), nil
}

func Hash(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/apikey/... -v`
Expected: all 5 tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/apikey
git commit -m "feat: add API key generation and hashing"
```

---

### Task 3: Storage layer — clients and usage events

**Files:**
- Create: `internal/storage/store.go`
- Create: `internal/storage/client.go`
- Create: `internal/storage/usage.go`
- Test: `internal/storage/client_test.go`
- Test: `internal/storage/usage_test.go`

**Interfaces:**
- Consumes: `storage.NewPool` (Session 1)
- Produces: `storage.Store`, `storage.NewStore(pool *pgxpool.Pool) *Store`, `storage.Client{ID, Name, Email string; IsActive bool}`, `storage.ErrClientNotFound`, `(*Store) CreateClient(ctx, name, email, apiKeyHash string) (Client, error)`, `(*Store) FindClientByAPIKeyHash(ctx, apiKeyHash string) (Client, error)`, `(*Store) DeleteClient(ctx, id string) error`, `storage.UsageEvent{ClientID, RequestID, Endpoint string}`, `(*Store) InsertUsageEvent(ctx, event UsageEvent) error`

- [ ] **Step 1: Write `store.go`**

```go
// internal/storage/store.go
package storage

import "github.com/jackc/pgx/v5/pgxpool"

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
```

- [ ] **Step 2: Write the failing tests for the client methods**

```go
// internal/storage/client_test.go
package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"testing"
)

func randomHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read() returned unexpected error: %v", err)
	}
	return hex.EncodeToString(b)
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set; skipping integration test against Supabase")
	}
	pool, err := NewPool(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("NewPool() returned unexpected error: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewStore(pool)
}

func TestCreateClient_ThenFindByAPIKeyHash(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hash := randomHash(t)

	created, err := store.CreateClient(ctx, "Test Client", "test@example.com", hash)
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), created.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	if !created.IsActive {
		t.Error("created.IsActive = false, want true")
	}

	found, err := store.FindClientByAPIKeyHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindClientByAPIKeyHash() returned unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, created.ID)
	}
	if found.Name != "Test Client" {
		t.Errorf("found.Name = %q, want %q", found.Name, "Test Client")
	}
}

func TestFindClientByAPIKeyHash_ReturnsErrNotFoundForUnknownHash(t *testing.T) {
	store := newTestStore(t)

	_, err := store.FindClientByAPIKeyHash(context.Background(), randomHash(t))
	if !errors.Is(err, ErrClientNotFound) {
		t.Errorf("err = %v, want ErrClientNotFound", err)
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/storage/...`
Expected: FAIL — compile error, `Client`, `ErrClientNotFound`, `CreateClient`, `FindClientByAPIKeyHash`, `DeleteClient` undefined.

- [ ] **Step 4: Write `client.go`**

```go
// internal/storage/client.go
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
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
go test ./internal/storage/... -v -run 'TestCreateClient|TestFindClientByAPIKeyHash'
```

Expected: both tests PASS against the real Supabase database (each cleans up its own inserted row).

- [ ] **Step 6: Write the failing test for usage events**

```go
// internal/storage/usage_test.go
package storage

import (
	"context"
	"testing"
)

func TestInsertUsageEvent_Succeeds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	client, err := store.CreateClient(ctx, "Usage Test Client", "usage-test@example.com", randomHash(t))
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), client.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	err = store.InsertUsageEvent(ctx, UsageEvent{
		ClientID:  client.ID,
		RequestID: randomHash(t),
		Endpoint:  "/v1/whoami",
	})
	if err != nil {
		t.Errorf("InsertUsageEvent() returned unexpected error: %v", err)
	}
}
```

- [ ] **Step 7: Run test to verify it fails**

Run: `go test ./internal/storage/... -run TestInsertUsageEvent`
Expected: FAIL — compile error, `UsageEvent` and `InsertUsageEvent` undefined.

- [ ] **Step 8: Write `usage.go`**

```go
// internal/storage/usage.go
package storage

import "context"

type UsageEvent struct {
	ClientID  string
	RequestID string
	Endpoint  string
}

func (s *Store) InsertUsageEvent(ctx context.Context, event UsageEvent) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO usage_events (client_id, request_id, endpoint) VALUES ($1, $2, $3)`,
		event.ClientID, event.RequestID, event.Endpoint,
	)
	return err
}
```

- [ ] **Step 9: Run tests to verify they pass**

Run: `go test ./internal/storage/... -v`
Expected: all tests PASS (the Session 1 pool tests plus this session's client/usage tests)

- [ ] **Step 10: Commit**

```bash
git add internal/storage
git commit -m "feat: add Store with client lookup/creation and usage event recording"
```

---

### Task 4: Request IDs and the API-key auth middleware

**Files:**
- Create: `internal/api/requestid.go`
- Create: `internal/api/middleware.go`
- Test: `internal/api/requestid_test.go`
- Test: `internal/api/middleware_test.go`

**Interfaces:**
- Consumes: `apikey.Hash(key string) string` (Task 2), `storage.Client`, `storage.ErrClientNotFound`, `storage.UsageEvent` (Task 3)
- Produces: `api.NewRequestID() string`, `api.ClientFinder` interface, `api.UsageRecorder` interface, `api.RequireAPIKey(finder ClientFinder, recorder UsageRecorder) func(http.Handler) http.Handler`, `api.ClientFromContext(ctx) (storage.Client, bool)`. Also defines (in `middleware_test.go`, reused by Task 5's tests): `fakeClientFinder`, `fakeUsageRecorder`.

- [ ] **Step 1: Write the failing request ID test**

```go
// internal/api/requestid_test.go
package api

import "testing"

func TestNewRequestID_ProducesNonEmptyDistinctValues(t *testing.T) {
	id1 := NewRequestID()
	id2 := NewRequestID()

	if id1 == "" {
		t.Error("NewRequestID() returned an empty string")
	}
	if id1 == id2 {
		t.Error("NewRequestID() returned the same value twice")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/api/... -run TestNewRequestID`
Expected: FAIL — compile error, `NewRequestID` undefined.

- [ ] **Step 3: Write `requestid.go`**

```go
// internal/api/requestid.go
package api

import "crypto/rand"

// NewRequestID returns a short random identifier for correlating a
// request's usage tracking.
func NewRequestID() string {
	return rand.Text()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/api/... -run TestNewRequestID -v`
Expected: PASS

- [ ] **Step 5: Write the failing middleware tests**

```go
// internal/api/middleware_test.go
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
```

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/api/...`
Expected: FAIL — compile error, `RequireAPIKey` undefined.

- [ ] **Step 7: Write `middleware.go`**

```go
// internal/api/middleware.go
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
				writeUnauthorized(w)
				return
			}

			client, err := finder.FindClientByAPIKeyHash(r.Context(), apikey.Hash(key))
			if err != nil {
				if !errors.Is(err, storage.ErrClientNotFound) {
					log.Printf("auth: lookup failed: %v", err)
				}
				writeUnauthorized(w)
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

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": "invalid or missing API key"})
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: all tests PASS

- [ ] **Step 9: Commit**

```bash
git add internal/api
git commit -m "feat: add API key auth middleware with usage recording"
```

---

### Task 5: Protected `/v1/whoami` endpoint

**Files:**
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`

**Interfaces:**
- Consumes: `RequireAPIKey`, `ClientFromContext` (Task 4), `fakeClientFinder`, `fakeUsageRecorder` (defined in `middleware_test.go`, Task 4)
- Produces: `NewRouter(finder ClientFinder, recorder UsageRecorder) *chi.Mux` (signature change from Session 1's `NewRouter()`)

- [ ] **Step 1: Update the failing/changed tests**

```go
// internal/api/router_test.go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func TestHealthz_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	NewRouter(nil, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestWhoami_ReturnsClientInfoWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(finder, recorder).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["client_id"] != client.ID {
		t.Errorf("client_id = %q, want %q", body["client_id"], client.ID)
	}
}

func TestWhoami_ReturnsUnauthorizedWithoutKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	rec := httptest.NewRecorder()

	NewRouter(&fakeClientFinder{}, &fakeUsageRecorder{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/...`
Expected: FAIL — `NewRouter(nil, nil)` compile error (existing `NewRouter` takes no arguments), `TestWhoami_*` fail as well once it compiles.

- [ ] **Step 3: Update `router.go`**

```go
// internal/api/router.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(finder ClientFinder, recorder UsageRecorder) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)

	r.Group(func(r chi.Router) {
		r.Use(RequireAPIKey(finder, recorder))
		r.Get("/v1/whoami", handleWhoami)
	})

	return r
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleWhoami(w http.ResponseWriter, r *http.Request) {
	client, ok := ClientFromContext(r.Context())
	if !ok {
		http.Error(w, "internal error: client not found in context", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"client_id": client.ID,
		"name":      client.Name,
	})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api
git commit -m "feat: add protected /v1/whoami endpoint"
```

---

### Task 6: `issuekey` CLI

**Files:**
- Create: `cmd/issuekey/main.go`
- Test: `cmd/issuekey/main_test.go`

**Interfaces:**
- Consumes: `apikey.Generate`, `apikey.Hash` (Task 2), `config.Load` (Session 1), `storage.NewPool`, `storage.NewStore`, `(*Store) CreateClient` (Session 1 / Task 3)
- Produces: a runnable binary at `./cmd/issuekey`; `formatIssuedKeyMessage(client storage.Client, plainKey string) string` (unexported, tested directly since the test lives in `package main`)

- [ ] **Step 1: Write the failing test for the formatting function**

```go
// cmd/issuekey/main_test.go
package main

import (
	"strings"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func TestFormatIssuedKeyMessage_IncludesAllFields(t *testing.T) {
	client := storage.Client{ID: "abc-123", Name: "Acme", Email: "dev@acme.com"}
	msg := formatIssuedKeyMessage(client, "ytpub_secret")

	for _, want := range []string{"Acme", "dev@acme.com", "abc-123", "ytpub_secret"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/issuekey/...`
Expected: FAIL — compile error, `formatIssuedKeyMessage` undefined (and `main` package has no other files yet).

- [ ] **Step 3: Write `main.go`**

```go
// cmd/issuekey/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func main() {
	name := flag.String("name", "", "client name (required)")
	email := flag.String("email", "", "client email (required)")
	flag.Parse()

	if *name == "" || *email == "" {
		log.Fatal("both -name and -email are required")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := storage.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer pool.Close()

	plainKey, err := apikey.Generate()
	if err != nil {
		log.Fatalf("apikey: %v", err)
	}

	store := storage.NewStore(pool)
	client, err := store.CreateClient(ctx, *name, *email, apikey.Hash(plainKey))
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	fmt.Print(formatIssuedKeyMessage(client, plainKey))
}

func formatIssuedKeyMessage(client storage.Client, plainKey string) string {
	return fmt.Sprintf(
		"API key created for %s <%s>\nClient ID: %s\nAPI Key (save this now, it will not be shown again): %s\n",
		client.Name, client.Email, client.ID, plainKey,
	)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/issuekey/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/issuekey
git commit -m "feat: add issuekey CLI for creating API clients"
```

---

### Task 7: Wire `cmd/api/main.go` and verify end-to-end

**Files:**
- Modify: `cmd/api/main.go`

**Interfaces:**
- Consumes: `storage.NewStore` (Task 3), `api.NewRouter(finder, recorder)` (Task 5)

- [ ] **Step 1: Update `main.go`**

```go
// cmd/api/main.go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := storage.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer pool.Close()
	log.Println("connected to database")

	store := storage.NewStore(pool)
	router := api.NewRouter(store, store)

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: Build to verify it compiles**

Run: `go build ./...`
Expected: no errors, no output

- [ ] **Step 3: Run the full test suite**

Run: `go test ./... -v`
Expected: all tests PASS (with `DATABASE_URL` exported)

- [ ] **Step 4: Issue a real API key**

```bash
export PORT=8081
go run ./cmd/issuekey -name "Local Test Client" -email "test@example.com"
```

Expected: prints a message containing a Client ID and a key starting with `ytpub_`. Copy the key for the next steps.

- [ ] **Step 5: Run the server locally**

```bash
go run ./cmd/api
```

Expected log output: `connected to database` then `listening on :8081` (adjust the port if 8080 is still taken locally, as in Session 1).

- [ ] **Step 6: Verify unauthorized access is rejected**

In another terminal:

```bash
curl -i http://localhost:8081/v1/whoami
```

Expected: `HTTP/1.1 401 Unauthorized` with a JSON body like `{"error":"invalid or missing API key"}`.

- [ ] **Step 7: Verify authorized access works**

```bash
curl -i http://localhost:8081/v1/whoami -H "Authorization: Bearer <the key from Step 4>"
```

Expected: `HTTP/1.1 200 OK`, an `X-Request-Id` response header, and a JSON body with your client's `client_id` and `name`. Stop the server (Ctrl+C) after confirming.

- [ ] **Step 8: Verify the usage event was recorded**

```bash
psql "$DATABASE_URL" -c "SELECT client_id, endpoint, request_id, created_at FROM usage_events ORDER BY created_at DESC LIMIT 1;"
```

Expected: one row for `/v1/whoami` with the client ID from Step 4.

- [ ] **Step 9: Commit**

```bash
git add cmd/api
git commit -m "feat: wire API key auth into the main server"
```

---

## Session 2 done when

- `go test ./...` passes locally (with `DATABASE_URL` exported)
- `go run ./cmd/issuekey -name ... -email ...` creates a client and prints a usable API key
- `curl /v1/whoami` without a key returns `401`
- `curl /v1/whoami` with a valid key returns `200` with the client's info and an `X-Request-Id` header
- A row appears in `usage_events` for that request
