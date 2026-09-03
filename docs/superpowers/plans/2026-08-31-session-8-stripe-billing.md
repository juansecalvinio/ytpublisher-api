# Session 8: Stripe Billing (Self-Service Signup + Metered Usage) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a new customer self-serve signup via Stripe Checkout (metered subscription), have a webhook automatically provision their `api_clients` row and email them their API key, and report one unit of usage to Stripe's Billing Meter for every successful `/v1/generate` call.

**Architecture:** A new `internal/billing` package wraps `stripe-go` for three things: creating a Checkout Session (mode=subscription, our metered Price), verifying+parsing incoming webhook events, and reporting a Billing Meter Event per billable request. A new `internal/email` package (raw `net/http`, same style as `internal/embeddings`) sends the API key via Resend. `internal/storage.Client` gains a `StripeCustomerID` field so the webhook can look up an already-provisioned customer (idempotency) and so `/v1/generate` knows who to report usage for.

**Tech Stack:** Go 1.25.3, `github.com/stripe/stripe-go/v86` (verify exact API surface via `go doc` before writing code — see Task 3 note), raw `net/http` for Resend, existing `chi` router / `pgx` stack.

**Spec:** `docs/superpowers/specs/2026-08-24-ytpublisher-api-v1-design.md` (original roadmap item: "Billing: cliente Stripe, reporte de uso metered desde `usage_events`, estimación de costo por request" — the cost-estimation half of that item is explicitly deferred to Session 9; this plan covers self-service signup + metered usage reporting only).

## Global Constraints

- Go 1.25.3, module `github.com/juansecalvinio/ytpublisher-api`.
- TDD: failing test → verify fails → implement → verify passes → commit, for every step below.
- Real integration testing against real external services wherever the design calls for it (Stripe test mode, Resend, real Postgres via `DATABASE_URL`) — no mocks for these. Pure-logic pieces (webhook signature verification, JSON parsing) get deterministic unit tests instead, since there's nothing external to integrate against.
- `stripe-go/v86`'s exact calling convention (e.g. whether it's `sc.V1CheckoutSessions.Create(...)` on a `stripe.NewClient(key)` object, or older package-level functions) is **not confirmed** — verify with `go doc github.com/stripe/stripe-go/v86` and `go doc` on the specific type before finalizing Task 3/5's code, the same way Session 7 verified `anthropic-sdk-go` identifiers. Treat the code in this plan as a documented best-effort starting point, not gospel.
- New required env vars (add to `.env` — already gitignored — not just `.env.example`): `STRIPE_SECRET_KEY`, `STRIPE_WEBHOOK_SECRET`, `STRIPE_METERED_PRICE_ID`, `RESEND_API_KEY`, `PUBLIC_BASE_URL`. `RESEND_FROM_EMAIL` is optional (defaults to `onboarding@resend.dev`).
- Meter event name is a fixed string, `ytpublisher_generate_request` — must exactly match the "Event name" configured on the Stripe Meter created during account setup.

---

### Task 1: Config — Stripe & Resend settings

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config.StripeSecretKey`, `Config.StripeWebhookSecret`, `Config.StripeMeteredPriceID`, `Config.ResendAPIKey`, `Config.ResendFromEmail`, `Config.PublicBaseURL` — all `string`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/config/config_test.go`, and add the five new env vars (blank) to `setRequiredEnv`:

```go
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("YOUTUBE_API_KEY", "test-key")
	t.Setenv("YOUTUBE_DAILY_QUOTA_CAP", "")
	t.Setenv("STYLE_CACHE_TTL_HOURS", "")
	t.Setenv("VOYAGE_API_KEY", "test-voyage-key")
	t.Setenv("VOYAGE_MODEL", "")
	t.Setenv("ANTHROPIC_API_KEY", "test-anthropic-key")
	t.Setenv("ANTHROPIC_MODEL", "")
	t.Setenv("STRIPE_SECRET_KEY", "sk_test_dummy")
	t.Setenv("STRIPE_WEBHOOK_SECRET", "whsec_dummy")
	t.Setenv("STRIPE_METERED_PRICE_ID", "price_dummy")
	t.Setenv("RESEND_API_KEY", "re_dummy")
	t.Setenv("RESEND_FROM_EMAIL", "")
	t.Setenv("PUBLIC_BASE_URL", "http://localhost:8081")
}

func TestLoad_ErrorsWhenStripeSecretKeyMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STRIPE_SECRET_KEY", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingStripeSecretKey) {
		t.Errorf("err = %v, want ErrMissingStripeSecretKey", err)
	}
}

func TestLoad_ErrorsWhenStripeWebhookSecretMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STRIPE_WEBHOOK_SECRET", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingStripeWebhookSecret) {
		t.Errorf("err = %v, want ErrMissingStripeWebhookSecret", err)
	}
}

func TestLoad_ErrorsWhenStripeMeteredPriceIDMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STRIPE_METERED_PRICE_ID", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingStripeMeteredPriceID) {
		t.Errorf("err = %v, want ErrMissingStripeMeteredPriceID", err)
	}
}

func TestLoad_ErrorsWhenResendAPIKeyMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RESEND_API_KEY", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingResendAPIKey) {
		t.Errorf("err = %v, want ErrMissingResendAPIKey", err)
	}
}

func TestLoad_UsesDefaultResendFromEmailWhenUnset(t *testing.T) {
	setRequiredEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.ResendFromEmail != "onboarding@resend.dev" {
		t.Errorf("ResendFromEmail = %q, want %q", cfg.ResendFromEmail, "onboarding@resend.dev")
	}
}

func TestLoad_ReadsCustomResendFromEmail(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("RESEND_FROM_EMAIL", "billing@ytpublisher.example")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned unexpected error: %v", err)
	}
	if cfg.ResendFromEmail != "billing@ytpublisher.example" {
		t.Errorf("ResendFromEmail = %q, want %q", cfg.ResendFromEmail, "billing@ytpublisher.example")
	}
}

func TestLoad_ErrorsWhenPublicBaseURLMissing(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("PUBLIC_BASE_URL", "")

	_, err := Load()
	if !errors.Is(err, ErrMissingPublicBaseURL) {
		t.Errorf("err = %v, want ErrMissingPublicBaseURL", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/... -run TestLoad_ErrorsWhenStripe -v`
Expected: FAIL — `ErrMissingStripeSecretKey` etc. undefined (compile error).

- [ ] **Step 3: Implement**

In `internal/config/config.go`, add fields to `Config`:

```go
type Config struct {
	Port                 string
	DatabaseURL          string
	YouTubeAPIKey        string
	YouTubeDailyQuotaCap int
	StyleCacheTTLHours   int
	VoyageAPIKey         string
	VoyageModel          string
	AnthropicAPIKey      string
	AnthropicModel       string
	StripeSecretKey      string
	StripeWebhookSecret  string
	StripeMeteredPriceID string
	ResendAPIKey         string
	ResendFromEmail      string
	PublicBaseURL        string
}
```

Add errors:

```go
var (
	ErrMissingDatabaseURL          = errors.New("config: DATABASE_URL is required")
	ErrMissingYouTubeAPIKey        = errors.New("config: YOUTUBE_API_KEY is required")
	ErrInvalidQuotaCap             = errors.New("config: YOUTUBE_DAILY_QUOTA_CAP must be a positive integer")
	ErrInvalidStyleCacheTTL        = errors.New("config: STYLE_CACHE_TTL_HOURS must be a positive integer")
	ErrMissingVoyageAPIKey         = errors.New("config: VOYAGE_API_KEY is required")
	ErrMissingAnthropicAPIKey      = errors.New("config: ANTHROPIC_API_KEY is required")
	ErrMissingStripeSecretKey      = errors.New("config: STRIPE_SECRET_KEY is required")
	ErrMissingStripeWebhookSecret  = errors.New("config: STRIPE_WEBHOOK_SECRET is required")
	ErrMissingStripeMeteredPriceID = errors.New("config: STRIPE_METERED_PRICE_ID is required")
	ErrMissingResendAPIKey         = errors.New("config: RESEND_API_KEY is required")
	ErrMissingPublicBaseURL        = errors.New("config: PUBLIC_BASE_URL is required")
)
```

Add default:

```go
const (
	defaultYouTubeDailyQuotaCap = 9000
	defaultStyleCacheTTLHours   = 48
	defaultVoyageModel          = "voyage-3.5-lite"
	defaultAnthropicModel       = "claude-sonnet-5"
	defaultResendFromEmail      = "onboarding@resend.dev"
)
```

In `Load()`, after the existing `AnthropicAPIKey` block and before `return cfg, nil`:

```go
	cfg.StripeSecretKey = os.Getenv("STRIPE_SECRET_KEY")
	if cfg.StripeSecretKey == "" {
		return Config{}, ErrMissingStripeSecretKey
	}

	cfg.StripeWebhookSecret = os.Getenv("STRIPE_WEBHOOK_SECRET")
	if cfg.StripeWebhookSecret == "" {
		return Config{}, ErrMissingStripeWebhookSecret
	}

	cfg.StripeMeteredPriceID = os.Getenv("STRIPE_METERED_PRICE_ID")
	if cfg.StripeMeteredPriceID == "" {
		return Config{}, ErrMissingStripeMeteredPriceID
	}

	cfg.ResendAPIKey = os.Getenv("RESEND_API_KEY")
	if cfg.ResendAPIKey == "" {
		return Config{}, ErrMissingResendAPIKey
	}

	cfg.ResendFromEmail = os.Getenv("RESEND_FROM_EMAIL")
	if cfg.ResendFromEmail == "" {
		cfg.ResendFromEmail = defaultResendFromEmail
	}

	cfg.PublicBaseURL = os.Getenv("PUBLIC_BASE_URL")
	if cfg.PublicBaseURL == "" {
		return Config{}, ErrMissingPublicBaseURL
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS (all tests, old and new)

- [ ] **Step 5: Update `.env` and `.env.example`, then commit**

Add `PUBLIC_BASE_URL=http://localhost:8081` to both `.env` and `.env.example` (the other four vars are already present in both from account setup).

```bash
git add internal/config/config.go internal/config/config_test.go .env.example
git commit -m "config: add Stripe and Resend settings"
```

(`.env` is gitignored — edit it but don't stage it.)

---

### Task 2: storage — thread `stripe_customer_id` through `Client`

The `api_clients.stripe_customer_id` column already exists (Session 2 migration) but nothing reads or writes it yet.

**Files:**
- Modify: `internal/storage/client.go`
- Modify: `internal/storage/client_test.go`
- Modify: `cmd/issuekey/main.go`

**Interfaces:**
- Produces: `Client.StripeCustomerID string`; `CreateClient(ctx, name, email, apiKeyHash, stripeCustomerID string) (Client, error)`; `FindClientByStripeCustomerID(ctx, stripeCustomerID string) (Client, error)`.
- Consumes (later tasks): `FindClientByStripeCustomerID` is the webhook's idempotency check; `Client.StripeCustomerID` is what `/v1/generate` passes to `billing.ReportUsage`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/storage/client_test.go`:

```go
func TestCreateClient_StoresStripeCustomerID(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hash := randomHash(t)

	created, err := store.CreateClient(ctx, "Stripe Client", "stripe@example.com", hash, "cus_test123")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), created.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	if created.StripeCustomerID != "cus_test123" {
		t.Errorf("created.StripeCustomerID = %q, want %q", created.StripeCustomerID, "cus_test123")
	}

	found, err := store.FindClientByAPIKeyHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindClientByAPIKeyHash() returned unexpected error: %v", err)
	}
	if found.StripeCustomerID != "cus_test123" {
		t.Errorf("found.StripeCustomerID = %q, want %q", found.StripeCustomerID, "cus_test123")
	}
}

func TestFindClientByStripeCustomerID_ReturnsClient(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	hash := randomHash(t)

	created, err := store.CreateClient(ctx, "Stripe Client 2", "stripe2@example.com", hash, "cus_test456")
	if err != nil {
		t.Fatalf("CreateClient() returned unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.DeleteClient(context.Background(), created.ID); err != nil {
			t.Errorf("cleanup DeleteClient() returned error: %v", err)
		}
	})

	found, err := store.FindClientByStripeCustomerID(ctx, "cus_test456")
	if err != nil {
		t.Fatalf("FindClientByStripeCustomerID() returned unexpected error: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, created.ID)
	}
}

func TestFindClientByStripeCustomerID_ReturnsErrNotFoundForUnknownID(t *testing.T) {
	store := newTestStore(t)

	_, err := store.FindClientByStripeCustomerID(context.Background(), "cus_does_not_exist")
	if !errors.Is(err, ErrClientNotFound) {
		t.Errorf("err = %v, want ErrClientNotFound", err)
	}
}
```

Update the existing `TestCreateClient_ThenFindByAPIKeyHash` call site (it will fail to compile otherwise):

```go
	created, err := store.CreateClient(ctx, "Test Client", "test@example.com", hash, "")
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/storage/... -run TestCreateClient -v`
Expected: FAIL — compile error, `CreateClient` takes 4 args not 5; `FindClientByStripeCustomerID` undefined.

- [ ] **Step 3: Implement**

Replace `internal/storage/client.go` with:

```go
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
```

(`NULLIF($4, '')` keeps manually-issued clients' `stripe_customer_id` truly `NULL` rather than an empty string, matching the column's existing nullable design.)

Update `cmd/issuekey/main.go`'s call site:

```go
	client, err := store.CreateClient(ctx, *name, *email, apikey.Hash(plainKey), "")
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/storage/... -v` and `go build ./...`
Expected: PASS, build succeeds (confirms `cmd/issuekey` call site compiles).

- [ ] **Step 5: Commit**

```bash
git add internal/storage/client.go internal/storage/client_test.go cmd/issuekey/main.go
git commit -m "storage: thread stripe_customer_id through Client"
```

---

### Task 3: `internal/billing` — Checkout Session creation

**Files:**
- Create: `internal/billing/client.go`
- Create: `internal/billing/client_test.go`

**Interfaces:**
- Produces: `billing.NewClient(secretKey string) *Client`; `(c *Client) CreateCheckoutSession(ctx context.Context, priceID, successURL, cancelURL string) (string, error)` — returns the hosted Checkout URL.

**Before writing code:** run `go doc github.com/stripe/stripe-go/v86` and `go doc github.com/stripe/stripe-go/v86.Client` (or whatever the top-level client type turns out to be) to confirm: how to construct a client from a secret key, and the exact method/field name for creating a Checkout Session. The code below is a documented best guess (`stripe.NewClient(key)` → `.V1CheckoutSessions.Create(ctx, params)`) — adjust identifiers to match what `go doc` actually shows, the same way `ExtraFields`/`DisableParallelToolUse` were confirmed for the Anthropic SDK in Session 7.

- [ ] **Step 1: Add the dependency**

```bash
go get github.com/stripe/stripe-go/v86
```

- [ ] **Step 2: Write the failing test**

```go
package billing

import (
	"context"
	"os"
	"strings"
	"testing"
)

func testCredentials(t *testing.T) (secretKey, priceID string) {
	t.Helper()
	secretKey = os.Getenv("STRIPE_SECRET_KEY")
	priceID = os.Getenv("STRIPE_METERED_PRICE_ID")
	if secretKey == "" || priceID == "" {
		t.Skip("STRIPE_SECRET_KEY / STRIPE_METERED_PRICE_ID not set; skipping integration test against Stripe")
	}
	return secretKey, priceID
}

func TestCreateCheckoutSession_ReturnsHostedURL(t *testing.T) {
	secretKey, priceID := testCredentials(t)
	client := NewClient(secretKey)

	url, err := client.CreateCheckoutSession(context.Background(), priceID, "http://localhost:8081/v1/billing/success", "http://localhost:8081/v1/billing/cancel")
	if err != nil {
		t.Fatalf("CreateCheckoutSession() returned unexpected error: %v", err)
	}
	if !strings.HasPrefix(url, "https://checkout.stripe.com/") {
		t.Errorf("url = %q, want prefix %q", url, "https://checkout.stripe.com/")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/billing/... -v`
Expected: FAIL — package/`NewClient` undefined.

- [ ] **Step 4: Implement**

```go
package billing

import (
	"context"

	"github.com/stripe/stripe-go/v86"
)

type Client struct {
	sc *stripe.Client
}

func NewClient(secretKey string) *Client {
	return &Client{sc: stripe.NewClient(secretKey)}
}

func (c *Client) CreateCheckoutSession(ctx context.Context, priceID, successURL, cancelURL string) (string, error) {
	params := &stripe.CheckoutSessionCreateParams{
		Mode: stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID)},
		},
		SuccessURL: stripe.String(successURL),
		CancelURL:  stripe.String(cancelURL),
	}
	session, err := c.sc.V1CheckoutSessions.Create(ctx, params)
	if err != nil {
		return "", err
	}
	return session.URL, nil
}
```

Note the metered `LineItem` deliberately has no `Quantity` — usage is reported after the fact via meter events, never chosen upfront.

- [ ] **Step 5: Run test to verify it passes**

Run: `STRIPE_SECRET_KEY=<your sk_test_...> STRIPE_METERED_PRICE_ID=<your price_...> go test ./internal/billing/... -v`
Expected: PASS. If it fails on an SDK identifier, run the `go doc` commands from the note above and adjust.

- [ ] **Step 6: Commit**

```bash
git add internal/billing/client.go internal/billing/client_test.go go.mod go.sum
git commit -m "billing: create Stripe Checkout Sessions for metered subscriptions"
```

---

### Task 4: `internal/billing` — webhook signature verification + event parsing

This is pure logic (HMAC verification, JSON parsing) — no external service to call, so these are deterministic unit tests, not integration tests.

**Files:**
- Create: `internal/billing/webhook.go`
- Create: `internal/billing/webhook_test.go`

**Interfaces:**
- Produces: `ParseWebhookEvent(payload []byte, signatureHeader, secret string) (Event, error)` where `Event` has at least `Type string` and `Data []byte` (the raw object JSON); `ParseCheckoutCompleted(data []byte) (CheckoutCompleted, error)` where `CheckoutCompleted` has `CustomerID, Email, Name string`.

- [ ] **Step 1: Write the failing tests**

```go
package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"
)

func signPayload(secret string, payload []byte, timestamp int64) string {
	signedPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
}

func TestParseWebhookEvent_AcceptsValidSignature(t *testing.T) {
	secret := "whsec_test_secret"
	payload := []byte(`{"type":"checkout.session.completed","data":{"object":{"customer":"cus_1"}}}`)
	header := signPayload(secret, payload, time.Now().Unix())

	event, err := ParseWebhookEvent(payload, header, secret)
	if err != nil {
		t.Fatalf("ParseWebhookEvent() returned unexpected error: %v", err)
	}
	if event.Type != "checkout.session.completed" {
		t.Errorf("event.Type = %q, want %q", event.Type, "checkout.session.completed")
	}
}

func TestParseWebhookEvent_RejectsWrongSecret(t *testing.T) {
	payload := []byte(`{"type":"checkout.session.completed","data":{"object":{}}}`)
	header := signPayload("whsec_correct", payload, time.Now().Unix())

	_, err := ParseWebhookEvent(payload, header, "whsec_wrong")
	if err == nil {
		t.Error("ParseWebhookEvent() returned nil error, want an error for a mismatched signature")
	}
}

func TestParseCheckoutCompleted_ExtractsCustomerAndDetails(t *testing.T) {
	data := []byte(`{"customer":"cus_abc123","customer_details":{"email":"new@customer.com","name":"New Customer"}}`)

	completed, err := ParseCheckoutCompleted(data)
	if err != nil {
		t.Fatalf("ParseCheckoutCompleted() returned unexpected error: %v", err)
	}
	if completed.CustomerID != "cus_abc123" {
		t.Errorf("CustomerID = %q, want %q", completed.CustomerID, "cus_abc123")
	}
	if completed.Email != "new@customer.com" {
		t.Errorf("Email = %q, want %q", completed.Email, "new@customer.com")
	}
	if completed.Name != "New Customer" {
		t.Errorf("Name = %q, want %q", completed.Name, "New Customer")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/billing/... -run TestParseWebhookEvent -v`
Expected: FAIL — `ParseWebhookEvent`/`Event`/`ParseCheckoutCompleted` undefined.

- [ ] **Step 3: Implement**

```go
package billing

import (
	"encoding/json"

	"github.com/stripe/stripe-go/v86"
)

type Event struct {
	Type string
	Data []byte
}

// ParseWebhookEvent verifies the Stripe-Signature header against secret and,
// only if valid, returns the event's type and raw object JSON. Verify the
// exact stripe-go v86 identifier for signature verification via `go doc`
// before finalizing this — stripe.ConstructEvent is a documented guess.
func ParseWebhookEvent(payload []byte, signatureHeader, secret string) (Event, error) {
	stripeEvent, err := stripe.ConstructEvent(payload, signatureHeader, secret)
	if err != nil {
		return Event{}, err
	}
	return Event{Type: string(stripeEvent.Type), Data: stripeEvent.Data.Raw}, nil
}

// CheckoutCompleted holds the fields we need from a checkout.session.completed
// event. It's parsed with our own struct rather than a stripe-go type so this
// stays correct regardless of how that SDK models the Checkout Session object.
type CheckoutCompleted struct {
	CustomerID string
	Email      string
	Name       string
}

func ParseCheckoutCompleted(data []byte) (CheckoutCompleted, error) {
	var raw struct {
		Customer        string `json:"customer"`
		CustomerDetails struct {
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"customer_details"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return CheckoutCompleted{}, err
	}
	return CheckoutCompleted{
		CustomerID: raw.Customer,
		Email:      raw.CustomerDetails.Email,
		Name:       raw.CustomerDetails.Name,
	}, nil
}
```

If `go doc` shows the verification function actually lives in a `webhook` subpackage (`github.com/stripe/stripe-go/v86/webhook`) as `webhook.ConstructEvent`, use that import and call instead — the test suite doesn't care which, only that `ParseWebhookEvent`'s behavior is correct.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/billing/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/billing/webhook.go internal/billing/webhook_test.go
git commit -m "billing: verify Stripe webhook signatures and parse checkout.session.completed"
```

---

### Task 5: `internal/billing` — report usage to the Billing Meter

**Files:**
- Modify: `internal/billing/client.go`
- Modify: `internal/billing/client_test.go`

**Interfaces:**
- Produces: `(c *Client) ReportUsage(ctx context.Context, stripeCustomerID string) error`.

- [ ] **Step 1: Write the failing test**

Creating a real meter event needs a real Stripe customer ID to attach it to. Create a throwaway test customer via the same `stripe.Client`:

```go
func TestReportUsage_Succeeds(t *testing.T) {
	secretKey, _ := testCredentials(t)
	client := NewClient(secretKey)
	ctx := context.Background()

	customer, err := client.sc.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
		Email: stripe.String("session8-test@example.com"),
	})
	if err != nil {
		t.Fatalf("failed to create test customer: %v", err)
	}
	t.Cleanup(func() {
		if _, err := client.sc.V1Customers.Delete(context.Background(), customer.ID, nil); err != nil {
			t.Errorf("cleanup: failed to delete test customer: %v", err)
		}
	})

	if err := client.ReportUsage(ctx, customer.ID); err != nil {
		t.Errorf("ReportUsage() returned unexpected error: %v", err)
	}
}
```

Add `"github.com/stripe/stripe-go/v86"` to this test file's imports if not already present from Task 3.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/billing/... -run TestReportUsage -v`
Expected: FAIL — `ReportUsage` undefined.

- [ ] **Step 3: Implement**

Add to `internal/billing/client.go`:

```go
const meterEventName = "ytpublisher_generate_request"

func (c *Client) ReportUsage(ctx context.Context, stripeCustomerID string) error {
	params := &stripe.BillingMeterEventCreateParams{
		EventName: stripe.String(meterEventName),
		Payload: map[string]string{
			"stripe_customer_id": stripeCustomerID,
			"value":              "1",
		},
	}
	_, err := c.sc.V1BillingMeterEvents.Create(ctx, params)
	return err
}
```

Verify via `go doc` whether `Payload` is `map[string]string` or `map[string]interface{}` on `BillingMeterEventCreateParams`, and whether a `Timestamp` field is required or optional (defaults to "now" server-side if omitted) — adjust if `go doc` disagrees.

- [ ] **Step 4: Run test to verify it passes**

Run: `STRIPE_SECRET_KEY=<your sk_test_...> STRIPE_METERED_PRICE_ID=<your price_...> go test ./internal/billing/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/billing/client.go internal/billing/client_test.go
git commit -m "billing: report usage to the Stripe Billing Meter"
```

---

### Task 6: `internal/email` — send the API key via Resend

**Files:**
- Create: `internal/email/client.go`
- Create: `internal/email/client_test.go`

**Interfaces:**
- Produces: `email.NewClient(apiKey, fromAddress string) *Client`; `(c *Client) SendAPIKeyEmail(ctx context.Context, toEmail, toName, apiKey string) error`.

- [ ] **Step 1: Write the failing test**

```go
package email

import (
	"context"
	"os"
	"testing"
)

func TestSendAPIKeyEmail_Succeeds(t *testing.T) {
	apiKeyEnv := os.Getenv("RESEND_API_KEY")
	recipient := os.Getenv("RESEND_TEST_RECIPIENT")
	if apiKeyEnv == "" || recipient == "" {
		t.Skip("RESEND_API_KEY / RESEND_TEST_RECIPIENT not set; skipping integration test against Resend")
	}

	client := NewClient(apiKeyEnv, "onboarding@resend.dev")

	err := client.SendAPIKeyEmail(context.Background(), recipient, "Test Recipient", "ytpub_faketestkey")
	if err != nil {
		t.Errorf("SendAPIKeyEmail() returned unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/email/... -v`
Expected: FAIL — package/`NewClient` undefined.

- [ ] **Step 3: Implement**

```go
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const sendURL = "https://api.resend.com/emails"

type Client struct {
	apiKey     string
	from       string
	httpClient *http.Client
}

func NewClient(apiKey, from string) *Client {
	return &Client{apiKey: apiKey, from: from, httpClient: &http.Client{}}
}

type sendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (c *Client) SendAPIKeyEmail(ctx context.Context, toEmail, toName, apiKey string) error {
	body := sendRequest{
		From:    c.from,
		To:      []string{toEmail},
		Subject: "Your YTPublisher API key",
		Text: fmt.Sprintf(
			"Hi %s,\n\nThanks for subscribing to YTPublisher API. Here's your API key — save it now, it won't be shown again:\n\n%s\n\nUse it as a Bearer token: Authorization: Bearer %s\n",
			toName, apiKey, apiKey,
		),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sendURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("email: resend returned status %d: %s", resp.StatusCode, respBody)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `RESEND_API_KEY=<your re_...> RESEND_TEST_RECIPIENT=<your own email> go test ./internal/email/... -v`
Expected: PASS — check your inbox for the test email.

- [ ] **Step 5: Commit**

```bash
git add internal/email/client.go internal/email/client_test.go
git commit -m "email: send API key notifications via Resend"
```

---

### Task 7: `internal/api` — billing signup/success/cancel handlers

**Files:**
- Create: `internal/api/billing.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`

**Interfaces:**
- Consumes: nothing new from earlier tasks directly (takes a `CheckoutSessionCreator` interface, satisfied structurally by `*billing.Client`).
- Produces: `CheckoutSessionCreator` interface; `handleBillingSignup`, `handleBillingSuccess`, `handleBillingCancel`.

- [ ] **Step 1: Write the failing test**

Add to `internal/api/router_test.go`:

```go
type fakeCheckoutSessionCreator struct {
	url string
	err error
}

func (f *fakeCheckoutSessionCreator) CreateCheckoutSession(ctx context.Context, priceID, successURL, cancelURL string) (string, error) {
	return f.url, f.err
}

func TestBillingSignup_RedirectsToCheckoutURL(t *testing.T) {
	creator := &fakeCheckoutSessionCreator{url: "https://checkout.stripe.com/test-session"}

	req := httptest.NewRequest(http.MethodGet, "/v1/billing/signup", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		CheckoutCreator:      creator,
		StripeMeteredPriceID: "price_123",
		BillingSuccessURL:    "http://localhost:8081/v1/billing/success",
		BillingCancelURL:     "http://localhost:8081/v1/billing/cancel",
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusSeeOther)
	}
	if got := rec.Header().Get("Location"); got != creator.url {
		t.Errorf("Location = %q, want %q", got, creator.url)
	}
}

func TestBillingSuccess_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/billing/success", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run TestBillingSignup -v`
Expected: FAIL — `Dependencies.CheckoutCreator` etc. undefined.

- [ ] **Step 3: Implement**

Create `internal/api/billing.go`:

```go
package api

import (
	"context"
	"log"
	"net/http"
)

type CheckoutSessionCreator interface {
	CreateCheckoutSession(ctx context.Context, priceID, successURL, cancelURL string) (string, error)
}

func handleBillingSignup(creator CheckoutSessionCreator, priceID, successURL, cancelURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		url, err := creator.CreateCheckoutSession(r.Context(), priceID, successURL, cancelURL)
		if err != nil {
			log.Printf("billing: failed to create checkout session: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to start checkout")
			return
		}
		http.Redirect(w, r, url, http.StatusSeeOther)
	}
}

func handleBillingSuccess(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Thanks for subscribing! Check your email for your API key.\n"))
}

func handleBillingCancel(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Checkout canceled. No charge was made.\n"))
}
```

Update `internal/api/router.go`:

```go
type Dependencies struct {
	Finder               ClientFinder
	Recorder             UsageRecorder
	Syncer               ChannelSyncer
	StyleProvider        StyleProvider
	RelatedVideos        RelatedVideosProvider
	Generator            GenerationOrchestrator
	CheckoutCreator      CheckoutSessionCreator
	StripeMeteredPriceID string
	BillingSuccessURL    string
	BillingCancelURL     string
}

func NewRouter(deps Dependencies) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)
	r.Get("/v1/billing/signup", handleBillingSignup(deps.CheckoutCreator, deps.StripeMeteredPriceID, deps.BillingSuccessURL, deps.BillingCancelURL))
	r.Get("/v1/billing/success", handleBillingSuccess)
	r.Get("/v1/billing/cancel", handleBillingCancel)

	r.Group(func(r chi.Router) {
		r.Use(RequireAPIKey(deps.Finder, deps.Recorder))
		r.Get("/v1/whoami", handleWhoami)
		r.Post("/v1/internal/channels/{channelID}/sync", handleChannelSync(deps.Syncer))
		r.Get("/v1/internal/channels/{channelID}/style", handleChannelStyle(deps.StyleProvider))
		r.Get("/v1/internal/channels/{channelID}/related-videos", handleRelatedVideos(deps.RelatedVideos))
		r.Post("/v1/generate", handleGenerate(deps.Generator))
	})

	return r
}
```

(Billing routes are public — a brand-new customer has no API key yet — so they sit outside the `RequireAPIKey` group, same tier as `/healthz`.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS (all tests, old and new)

- [ ] **Step 5: Commit**

```bash
git add internal/api/billing.go internal/api/router.go internal/api/router_test.go
git commit -m "api: add self-service billing signup via Stripe Checkout"
```

---

### Task 8: `internal/api` — Stripe webhook handler (provisioning + idempotency + email)

**Files:**
- Create: `internal/api/webhook.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`

**Interfaces:**
- Consumes: `billing.ParseWebhookEvent`, `billing.ParseCheckoutCompleted` (Task 4); `storage.FindClientByStripeCustomerID`, `storage.CreateClient` (Task 2); `apikey.Generate`, `apikey.Hash`.
- Produces: `ClientProvisioner` interface, `KeyMailer` interface, `handleStripeWebhook`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/api/router_test.go`:

```go
type fakeClientProvisioner struct {
	byStripeCustomerID map[string]storage.Client
	created            []storage.Client
	createErr          error
}

func (f *fakeClientProvisioner) FindClientByStripeCustomerID(ctx context.Context, stripeCustomerID string) (storage.Client, error) {
	c, ok := f.byStripeCustomerID[stripeCustomerID]
	if !ok {
		return storage.Client{}, storage.ErrClientNotFound
	}
	return c, nil
}

func (f *fakeClientProvisioner) CreateClient(ctx context.Context, name, email, apiKeyHash, stripeCustomerID string) (storage.Client, error) {
	if f.createErr != nil {
		return storage.Client{}, f.createErr
	}
	c := storage.Client{ID: "new-client-id", Name: name, Email: email, IsActive: true, StripeCustomerID: stripeCustomerID}
	f.created = append(f.created, c)
	return c, nil
}

type fakeKeyMailer struct {
	sentTo []string
	err    error
}

func (f *fakeKeyMailer) SendAPIKeyEmail(ctx context.Context, toEmail, toName, apiKey string) error {
	f.sentTo = append(f.sentTo, toEmail)
	return f.err
}

func stripeSignedRequest(t *testing.T, secret string, payload []byte) *http.Request {
	t.Helper()
	header := signPayload(secret, payload, time.Now().Unix())
	req := httptest.NewRequest(http.MethodPost, "/v1/stripe/webhook", strings.NewReader(string(payload)))
	req.Header.Set("Stripe-Signature", header)
	return req
}

func TestStripeWebhook_ProvisionsNewClientOnCheckoutCompleted(t *testing.T) {
	secret := "whsec_test"
	payload := []byte(`{"type":"checkout.session.completed","data":{"object":{"customer":"cus_new","customer_details":{"email":"new@customer.com","name":"New Customer"}}}}`)

	provisioner := &fakeClientProvisioner{byStripeCustomerID: map[string]storage.Client{}}
	mailer := &fakeKeyMailer{}

	req := stripeSignedRequest(t, secret, payload)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		ClientProvisioner:   provisioner,
		KeyMailer:           mailer,
		StripeWebhookSecret: secret,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(provisioner.created) != 1 {
		t.Fatalf("created %d clients, want 1", len(provisioner.created))
	}
	if provisioner.created[0].Email != "new@customer.com" {
		t.Errorf("created client email = %q, want %q", provisioner.created[0].Email, "new@customer.com")
	}
	if len(mailer.sentTo) != 1 || mailer.sentTo[0] != "new@customer.com" {
		t.Errorf("sentTo = %v, want [new@customer.com]", mailer.sentTo)
	}
}

func TestStripeWebhook_IsIdempotentForAlreadyProvisionedCustomer(t *testing.T) {
	secret := "whsec_test"
	payload := []byte(`{"type":"checkout.session.completed","data":{"object":{"customer":"cus_existing","customer_details":{"email":"existing@customer.com","name":"Existing"}}}}`)

	provisioner := &fakeClientProvisioner{byStripeCustomerID: map[string]storage.Client{
		"cus_existing": {ID: "already-there", StripeCustomerID: "cus_existing"},
	}}
	mailer := &fakeKeyMailer{}

	req := stripeSignedRequest(t, secret, payload)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		ClientProvisioner:   provisioner,
		KeyMailer:           mailer,
		StripeWebhookSecret: secret,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(provisioner.created) != 0 {
		t.Errorf("created %d clients, want 0 (already provisioned)", len(provisioner.created))
	}
	if len(mailer.sentTo) != 0 {
		t.Errorf("sentTo = %v, want none (already provisioned)", mailer.sentTo)
	}
}

func TestStripeWebhook_RejectsInvalidSignature(t *testing.T) {
	payload := []byte(`{"type":"checkout.session.completed","data":{"object":{}}}`)
	req := stripeSignedRequest(t, "whsec_wrong", payload)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		ClientProvisioner:   &fakeClientProvisioner{},
		KeyMailer:           &fakeKeyMailer{},
		StripeWebhookSecret: "whsec_correct",
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
```

This reuses `signPayload` from `internal/billing/webhook_test.go` — since it's in a different package, redefine a small identical copy in `router_test.go` (or, cleaner: export it as `billing.SignPayloadForTest` — for a test-only helper crossing package boundaries, just duplicating the ~6 lines locally is simpler and avoids adding test-only exports to production code). Add `"strings"` and `"time"` to `router_test.go`'s imports if not already present (`"strings"` already is).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run TestStripeWebhook -v`
Expected: FAIL — `Dependencies.ClientProvisioner` etc. undefined.

- [ ] **Step 3: Implement**

Create `internal/api/webhook.go`:

```go
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
```

Update `internal/api/router.go` — add fields to `Dependencies` and the route:

```go
type Dependencies struct {
	Finder               ClientFinder
	Recorder             UsageRecorder
	Syncer               ChannelSyncer
	StyleProvider        StyleProvider
	RelatedVideos        RelatedVideosProvider
	Generator            GenerationOrchestrator
	CheckoutCreator      CheckoutSessionCreator
	ClientProvisioner    ClientProvisioner
	KeyMailer            KeyMailer
	StripeMeteredPriceID string
	StripeWebhookSecret  string
	BillingSuccessURL    string
	BillingCancelURL     string
}
```

Add the route (public, alongside the other billing routes):

```go
	r.Post("/v1/stripe/webhook", handleStripeWebhook(deps.ClientProvisioner, deps.KeyMailer, deps.StripeWebhookSecret))
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/webhook.go internal/api/router.go internal/api/router_test.go
git commit -m "api: provision clients and email API keys on Stripe checkout completion"
```

---

### Task 9: `internal/api` — report usage on successful `/v1/generate`

**Files:**
- Modify: `internal/api/generate.go`
- Modify: `internal/api/router.go`
- Modify: `internal/api/router_test.go`

**Interfaces:**
- Consumes: `ClientFromContext` (existing, `internal/api/middleware.go`); `storage.Client.StripeCustomerID` (Task 2).
- Produces: `UsageReporter` interface.

- [ ] **Step 1: Write the failing tests**

Find the existing `fakeGenerationOrchestrator` and `TestGenerate_ReturnsGeneratedContentWithValidKey` in `internal/api/router_test.go` (around line 270-310) and add:

```go
type fakeUsageReporter struct {
	reportedFor []string
	err         error
}

func (f *fakeUsageReporter) ReportUsage(ctx context.Context, stripeCustomerID string) error {
	f.reportedFor = append(f.reportedFor, stripeCustomerID)
	return f.err
}

func TestGenerate_ReportsUsageForClientWithStripeCustomerID(t *testing.T) {
	validKey := "ytpub_generatekey2"
	client := storage.Client{ID: "client-2", Name: "Acme", Email: "a@acme.com", IsActive: true, StripeCustomerID: "cus_billed"}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	reporter := &fakeUsageReporter{}
	orchestrator := &fakeGenerationOrchestrator{output: generation.Output{Title: "T"}}

	req := httptest.NewRequest(http.MethodPost, "/v1/generate", strings.NewReader(`{"channel_id":"UC1","topic":"t"}`))
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		Finder: finder, Recorder: &fakeUsageRecorder{}, Generator: orchestrator, UsageReporter: reporter,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if len(reporter.reportedFor) != 1 || reporter.reportedFor[0] != "cus_billed" {
		t.Errorf("reportedFor = %v, want [cus_billed]", reporter.reportedFor)
	}
}

func TestGenerate_SkipsUsageReportingForClientWithoutStripeCustomerID(t *testing.T) {
	validKey := "ytpub_generatekey3"
	client := storage.Client{ID: "client-3", Name: "Manual", Email: "m@example.com", IsActive: true, StripeCustomerID: ""}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	reporter := &fakeUsageReporter{}
	orchestrator := &fakeGenerationOrchestrator{output: generation.Output{Title: "T"}}

	req := httptest.NewRequest(http.MethodPost, "/v1/generate", strings.NewReader(`{"channel_id":"UC1","topic":"t"}`))
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{
		Finder: finder, Recorder: &fakeUsageRecorder{}, Generator: orchestrator, UsageReporter: reporter,
	}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(reporter.reportedFor) != 0 {
		t.Errorf("reportedFor = %v, want none (manually-issued client has no Stripe customer)", reporter.reportedFor)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/... -run TestGenerate_Reports -v`
Expected: FAIL — `Dependencies.UsageReporter` undefined; `handleGenerate` takes 1 arg not 2.

- [ ] **Step 3: Implement**

Update `internal/api/generate.go`:

```go
package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
)

type GenerationOrchestrator interface {
	Generate(ctx context.Context, input generation.Input) (generation.Output, error)
}

type UsageReporter interface {
	ReportUsage(ctx context.Context, stripeCustomerID string) error
}

type generateRequest struct {
	ChannelID string   `json:"channel_id"`
	Topic     string   `json:"topic"`
	Notes     string   `json:"notes"`
	Language  string   `json:"language"`
	Links     []string `json:"links"`
	Mentions  []string `json:"mentions"`
	Tone      string   `json:"tone"`
}

func handleGenerate(orchestrator GenerationOrchestrator, reporter UsageReporter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.ChannelID == "" || req.Topic == "" {
			writeJSONError(w, http.StatusBadRequest, "channel_id and topic are required")
			return
		}

		output, err := orchestrator.Generate(r.Context(), generation.Input{
			ChannelID: req.ChannelID,
			Topic:     req.Topic,
			Notes:     req.Notes,
			Language:  req.Language,
			Links:     req.Links,
			Mentions:  req.Mentions,
			Tone:      req.Tone,
		})
		if err != nil {
			log.Printf("generate: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to generate content")
			return
		}

		if client, ok := ClientFromContext(r.Context()); ok && client.StripeCustomerID != "" {
			if err := reporter.ReportUsage(r.Context(), client.StripeCustomerID); err != nil {
				// The customer already has their content; a billing report
				// failure shouldn't turn into a failed request for them.
				log.Printf("generate: failed to report usage for %s: %v", client.StripeCustomerID, err)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"title":          output.Title,
			"description":    output.Description,
			"tags":           output.Tags,
			"related_videos": output.RelatedVideos,
			"warnings":       output.Warnings,
		})
	}
}
```

Update `internal/api/router.go`: add `UsageReporter UsageReporter` to `Dependencies`, and change the route to `r.Post("/v1/generate", handleGenerate(deps.Generator, deps.UsageReporter))`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/api/generate.go internal/api/router.go internal/api/router_test.go
git commit -m "api: report metered usage to Stripe after successful generation"
```

---

### Task 10: Wire everything in `cmd/api/main.go` and verify end-to-end with the Stripe CLI

**Files:**
- Modify: `cmd/api/main.go`

- [ ] **Step 1: Update main.go**

```go
package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/joho/godotenv"
	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/billing"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/claude"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/email"
	"github.com/juansecalvinio/ytpublisher-api/internal/embeddings"
	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
	"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/stylecache"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

const maxVideosPerChannel = 25

func main() {
	_ = godotenv.Load()

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

	youtubeClient, err := youtube.NewClient(ctx, cfg.YouTubeAPIKey)
	if err != nil {
		log.Fatalf("youtube: %v", err)
	}
	syncer := channelsync.NewSyncer(youtubeClient, store, store, maxVideosPerChannel, cfg.YouTubeDailyQuotaCap)

	styleTTL := time.Duration(cfg.StyleCacheTTLHours) * time.Hour
	styleProvider := stylecache.NewProvider(store, store, styleTTL)

	embeddingsClient := embeddings.NewClient(cfg.VoyageAPIKey, cfg.VoyageModel)
	relatedVideosProvider := relatedvideos.NewProvider(store, embeddingsClient)

	claudeClient := claude.NewClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
	generator := generation.NewOrchestrator(styleProvider, relatedVideosProvider, claudeClient)

	billingClient := billing.NewClient(cfg.StripeSecretKey)
	emailClient := email.NewClient(cfg.ResendAPIKey, cfg.ResendFromEmail)

	router := api.NewRouter(api.Dependencies{
		Finder:               store,
		Recorder:             store,
		Syncer:               syncer,
		StyleProvider:        styleProvider,
		RelatedVideos:        relatedVideosProvider,
		Generator:            generator,
		CheckoutCreator:      billingClient,
		ClientProvisioner:    store,
		KeyMailer:            emailClient,
		UsageReporter:        billingClient,
		StripeMeteredPriceID: cfg.StripeMeteredPriceID,
		StripeWebhookSecret:  cfg.StripeWebhookSecret,
		BillingSuccessURL:    cfg.PublicBaseURL + "/v1/billing/success",
		BillingCancelURL:     cfg.PublicBaseURL + "/v1/billing/cancel",
	})

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: succeeds (confirms `*storage.Store` satisfies `ClientProvisioner` and `*billing.Client` satisfies both `CheckoutSessionCreator` and `UsageReporter`).

- [ ] **Step 3: Full test suite**

Run: `go test ./...`
Expected: PASS across all packages.

- [ ] **Step 4: Manual end-to-end verification**

This is the real proof the feature works — run it live, together, the same way Session 7's `/v1/generate` fix was verified against the real failing case.

1. In one terminal: `! stripe listen --forward-to localhost:8081/v1/stripe/webhook` — copy the `whsec_...` it prints into `.env` as `STRIPE_WEBHOOK_SECRET`, replacing the placeholder.
2. In another terminal: `go run ./cmd/api`
3. Open `http://localhost:8081/v1/billing/signup` in a browser — it should redirect to a real Stripe Checkout page for the metered subscription.
4. Complete checkout with Stripe's test card `4242 4242 4242 4242`, any future expiry, any CVC.
5. Confirm: browser lands on the success page; the `stripe listen` terminal shows `checkout.session.completed` forwarded with a `200`; an email arrives with a `ytpub_...` key (check spam if using the Resend sandbox sender).
6. Call `/v1/generate` with that new key and confirm it works, then check the Stripe Dashboard's **Meters** page (test mode) — usage should show up (may take a minute to aggregate).
7. Re-send the same webhook event from the Stripe CLI (`stripe events resend <event-id>` or trigger `stripe trigger checkout.session.completed` twice) and confirm no duplicate client/email — the idempotency check holds.

- [ ] **Step 5: Commit**

```bash
git add cmd/api/main.go
git commit -m "cmd/api: wire Stripe billing and Resend email into the server"
```

---

## After all tasks

Use superpowers:finishing-a-development-branch to verify the full suite, then present the merge/PR/keep menu.
