package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

type fakeChannelSyncer struct {
	result channelsync.Result
	err    error
}

func (f *fakeChannelSyncer) SyncChannel(ctx context.Context, channelID string) (channelsync.Result, error) {
	return f.result, f.err
}

type fakeStyleProvider struct {
	summary styleanalysis.Summary
	err     error
}

func (f *fakeStyleProvider) GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error) {
	return f.summary, f.err
}

type fakeRelatedVideosProvider struct {
	videos []storage.ChannelVideo
	err    error
}

func (f *fakeRelatedVideosProvider) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, error) {
	return f.videos, f.err
}

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

func signPayload(secret string, payload []byte, timestamp int64) string {
	signedPayload := fmt.Sprintf("%d.%s", timestamp, payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedPayload))
	return fmt.Sprintf("t=%d,v1=%s", timestamp, hex.EncodeToString(mac.Sum(nil)))
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
	payload := []byte(`{"object":"event","type":"checkout.session.completed","data":{"object":{"customer":"cus_new","customer_details":{"email":"new@customer.com","name":"New Customer"}}}}`)

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
	payload := []byte(`{"object":"event","type":"checkout.session.completed","data":{"object":{"customer":"cus_existing","customer_details":{"email":"existing@customer.com","name":"Existing"}}}}`)

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
	payload := []byte(`{"object":"event","type":"checkout.session.completed","data":{"object":{}}}`)
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

func TestHealthz_ReturnsOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{}).ServeHTTP(rec, req)

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

	NewRouter(Dependencies{Finder: finder, Recorder: recorder}).ServeHTTP(rec, req)

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

	NewRouter(Dependencies{Finder: &fakeClientFinder{}, Recorder: &fakeUsageRecorder{}}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestChannelSync_ReturnsSyncResultWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	syncer := &fakeChannelSyncer{result: channelsync.Result{VideosSynced: 5, QuotaUsed: 3}}

	req := httptest.NewRequest(http.MethodPost, "/v1/internal/channels/UC123/sync", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, Syncer: syncer}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["channel_id"] != "UC123" {
		t.Errorf("channel_id = %v, want %q", body["channel_id"], "UC123")
	}
}

func TestChannelSync_ReturnsNotFoundForUnknownChannel(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	syncer := &fakeChannelSyncer{err: youtube.ErrChannelNotFound}

	req := httptest.NewRequest(http.MethodPost, "/v1/internal/channels/UC-does-not-exist/sync", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, Syncer: syncer}).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestChannelSync_ReturnsTooManyRequestsWhenQuotaExceeded(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	syncer := &fakeChannelSyncer{err: channelsync.ErrQuotaExceeded}

	req := httptest.NewRequest(http.MethodPost, "/v1/internal/channels/UC123/sync", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, Syncer: syncer}).ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header not set")
	}
}

func TestChannelStyle_ReturnsSummaryWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	provider := &fakeStyleProvider{summary: styleanalysis.Summary{VideoCountAnalyzed: 10, Confidence: "high"}}

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/style", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, StyleProvider: provider}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body styleanalysis.Summary
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.VideoCountAnalyzed != 10 {
		t.Errorf("VideoCountAnalyzed = %d, want 10", body.VideoCountAnalyzed)
	}
}

func TestChannelStyle_ReturnsUnauthorizedWithoutKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/style", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: &fakeClientFinder{}, Recorder: &fakeUsageRecorder{}}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestRelatedVideos_ReturnsResultsWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}
	provider := &fakeRelatedVideosProvider{videos: []storage.ChannelVideo{{VideoID: "v1", Title: "Video 1"}}}

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/related-videos?topic=go+programming", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, RelatedVideos: provider}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["topic"] != "go programming" {
		t.Errorf("topic = %v, want %q", body["topic"], "go programming")
	}
	related, ok := body["related_videos"].([]any)
	if !ok || len(related) != 1 {
		t.Errorf("related_videos = %v, want a 1-element list", body["related_videos"])
	}
}

func TestRelatedVideos_ReturnsBadRequestWithoutTopic(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{
		apikey.Hash(validKey): client,
	}}
	recorder := &fakeUsageRecorder{}

	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/related-videos", nil)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestRelatedVideos_ReturnsUnauthorizedWithoutKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/internal/channels/UC123/related-videos?topic=x", nil)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: &fakeClientFinder{}, Recorder: &fakeUsageRecorder{}}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

type fakeGenerationOrchestrator struct {
	output generation.Output
	err    error
}

func (f *fakeGenerationOrchestrator) Generate(ctx context.Context, input generation.Input) (generation.Output, error) {
	return f.output, f.err
}

func TestGenerate_ReturnsGeneratedContentWithValidKey(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	recorder := &fakeUsageRecorder{}
	orchestrator := &fakeGenerationOrchestrator{output: generation.Output{
		Title:       "Generated Title",
		Description: "Generated description",
		Tags:        []string{"go"},
	}}

	body := strings.NewReader(`{"channel_id":"UC123","topic":"Go basics"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/generate", body)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder, Generator: orchestrator}).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var respBody map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&respBody); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if respBody["title"] != "Generated Title" {
		t.Errorf("title = %v, want %q", respBody["title"], "Generated Title")
	}
}

func TestGenerate_ReturnsBadRequestForMissingFields(t *testing.T) {
	validKey := "ytpub_validkey"
	client := storage.Client{ID: "client-1", Name: "Acme", Email: "a@acme.com", IsActive: true}
	finder := &fakeClientFinder{clientsByHash: map[string]storage.Client{apikey.Hash(validKey): client}}
	recorder := &fakeUsageRecorder{}

	body := strings.NewReader(`{"channel_id":"UC123"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/generate", body)
	req.Header.Set("Authorization", "Bearer "+validKey)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: finder, Recorder: recorder}).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGenerate_ReturnsUnauthorizedWithoutKey(t *testing.T) {
	body := strings.NewReader(`{"channel_id":"UC123","topic":"Go basics"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/generate", body)
	rec := httptest.NewRecorder()

	NewRouter(Dependencies{Finder: &fakeClientFinder{}, Recorder: &fakeUsageRecorder{}}).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
