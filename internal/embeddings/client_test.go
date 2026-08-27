package embeddings

import (
	"context"
	"os"
	"testing"
)

func TestEmbedDocuments_ReturnsVectorsWithExpectedDimension(t *testing.T) {
	apiKey := os.Getenv("VOYAGE_API_KEY")
	if apiKey == "" {
		t.Skip("VOYAGE_API_KEY not set; skipping integration test against the real Voyage API")
	}

	client := NewClient(apiKey, "voyage-3.5-lite")

	vectors, err := client.EmbedDocuments(context.Background(), []string{"Go programming tutorial", "How to bake bread"})
	if err != nil {
		t.Fatalf("EmbedDocuments() returned unexpected error: %v", err)
	}
	if len(vectors) != 2 {
		t.Fatalf("len(vectors) = %d, want 2", len(vectors))
	}
	for i, v := range vectors {
		if len(v) != 1024 {
			t.Errorf("len(vectors[%d]) = %d, want 1024", i, len(v))
		}
	}
}

func TestEmbedQuery_ReturnsSingleVector(t *testing.T) {
	apiKey := os.Getenv("VOYAGE_API_KEY")
	if apiKey == "" {
		t.Skip("VOYAGE_API_KEY not set; skipping integration test against the real Voyage API")
	}

	client := NewClient(apiKey, "voyage-3.5-lite")

	vector, err := client.EmbedQuery(context.Background(), "Go programming")
	if err != nil {
		t.Fatalf("EmbedQuery() returned unexpected error: %v", err)
	}
	if len(vector) != 1024 {
		t.Errorf("len(vector) = %d, want 1024", len(vector))
	}
}
