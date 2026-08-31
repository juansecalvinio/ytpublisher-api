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
