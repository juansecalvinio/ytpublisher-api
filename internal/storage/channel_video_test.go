package storage

import (
	"context"
	"testing"
	"time"
)

func TestUpsertChannelVideos_ThenListChannelVideos(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	videos := []ChannelVideo{
		{
			ChannelID:   channelID,
			VideoID:     "v1",
			Title:       "First",
			Description: "desc1",
			Tags:        []string{"go", "test"},
			PublishedAt: time.Now().Truncate(time.Second),
		},
	}

	if err := store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		t.Fatalf("UpsertChannelVideos() returned unexpected error: %v", err)
	}

	found, err := store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() returned unexpected error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len(found) = %d, want 1", len(found))
	}
	if found[0].Title != "First" {
		t.Errorf("Title = %q, want %q", found[0].Title, "First")
	}
	if len(found[0].Tags) != 2 || found[0].Tags[0] != "go" {
		t.Errorf("Tags = %v, want [go test]", found[0].Tags)
	}
}

func TestUpsertChannelVideos_UpdatesOnConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelVideos(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelVideos() returned error: %v", err)
		}
	})

	first := []ChannelVideo{{ChannelID: channelID, VideoID: "v1", Title: "Old Title", PublishedAt: time.Now().Truncate(time.Second)}}
	if err := store.UpsertChannelVideos(ctx, channelID, first); err != nil {
		t.Fatalf("UpsertChannelVideos() (first) returned unexpected error: %v", err)
	}

	second := []ChannelVideo{{ChannelID: channelID, VideoID: "v1", Title: "New Title", PublishedAt: time.Now().Truncate(time.Second)}}
	if err := store.UpsertChannelVideos(ctx, channelID, second); err != nil {
		t.Fatalf("UpsertChannelVideos() (second) returned unexpected error: %v", err)
	}

	found, err := store.ListChannelVideos(ctx, channelID)
	if err != nil {
		t.Fatalf("ListChannelVideos() returned unexpected error: %v", err)
	}
	if len(found) != 1 {
		t.Fatalf("len(found) = %d, want 1 (update, not duplicate)", len(found))
	}
	if found[0].Title != "New Title" {
		t.Errorf("Title = %q, want %q", found[0].Title, "New Title")
	}
}
