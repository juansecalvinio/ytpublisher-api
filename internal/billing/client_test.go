package billing

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stripe/stripe-go/v86"
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
