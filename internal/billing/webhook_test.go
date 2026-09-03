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
	payload := []byte(`{"object":"event","type":"checkout.session.completed","data":{"object":{"customer":"cus_1"}}}`)
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
