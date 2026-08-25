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
