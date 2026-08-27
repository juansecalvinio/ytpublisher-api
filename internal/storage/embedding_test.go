package storage

import (
	"context"
	"testing"
	"time"
)

func TestUpdateChannelVideoEmbedding_ThenFindSimilarVideos(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-embed-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	videos := []ChannelVideo{
		{ChannelID: channelID, VideoID: "close", Title: "Close", PublishedAt: time.Now()},
		{ChannelID: channelID, VideoID: "far", Title: "Far", PublishedAt: time.Now()},
	}
	if err := store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		t.Fatalf("UpsertChannelVideos() returned unexpected error: %v", err)
	}

	closeEmbedding := make([]float32, 1024)
	farEmbedding := make([]float32, 1024)
	queryEmbedding := make([]float32, 1024)
	for i := range closeEmbedding {
		closeEmbedding[i] = 1.0
		farEmbedding[i] = -1.0
		queryEmbedding[i] = 1.0
	}

	if err := store.UpdateChannelVideoEmbedding(ctx, channelID, "close", closeEmbedding); err != nil {
		t.Fatalf("UpdateChannelVideoEmbedding() (close) returned unexpected error: %v", err)
	}
	if err := store.UpdateChannelVideoEmbedding(ctx, channelID, "far", farEmbedding); err != nil {
		t.Fatalf("UpdateChannelVideoEmbedding() (far) returned unexpected error: %v", err)
	}

	results, err := store.FindSimilarVideos(ctx, channelID, queryEmbedding, 1)
	if err != nil {
		t.Fatalf("FindSimilarVideos() returned unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if results[0].VideoID != "close" {
		t.Errorf("results[0].VideoID = %q, want %q (most similar to the query direction)", results[0].VideoID, "close")
	}
}

func TestListChannelVideos_IncludesEmbeddingWhenPresent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-embed-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	videos := []ChannelVideo{{ChannelID: channelID, VideoID: "v1", Title: "V1", PublishedAt: time.Now()}}
	if err := store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		t.Fatalf("UpsertChannelVideos() returned unexpected error: %v", err)
	}

	found, err := store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() returned unexpected error: %v", err)
	}
	if len(found[0].Embedding) != 0 {
		t.Errorf("Embedding = %v, want empty (not yet computed)", found[0].Embedding)
	}

	embedding := make([]float32, 1024)
	embedding[0] = 0.5
	if err := store.UpdateChannelVideoEmbedding(ctx, channelID, "v1", embedding); err != nil {
		t.Fatalf("UpdateChannelVideoEmbedding() returned unexpected error: %v", err)
	}

	found, err = store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() (after) returned unexpected error: %v", err)
	}
	if len(found[0].Embedding) != 1024 {
		t.Fatalf("len(Embedding) = %d, want 1024", len(found[0].Embedding))
	}
	if found[0].Embedding[0] != 0.5 {
		t.Errorf("Embedding[0] = %v, want 0.5", found[0].Embedding[0])
	}
}
