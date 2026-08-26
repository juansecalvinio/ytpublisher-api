package youtube

import (
	"context"
	"errors"
	"os"
	"testing"
)

// The official Google Developers channel — stable, public, used in Google's
// own API examples, so it won't disappear or go private under us.
const testChannelID = "UC_x5XG1OV2P6uZZ5FSM9Ttw"

func TestFetchLatestVideos_ReturnsRealVideos(t *testing.T) {
	apiKey := os.Getenv("YOUTUBE_API_KEY")
	if apiKey == "" {
		t.Skip("YOUTUBE_API_KEY not set; skipping integration test against the real YouTube Data API")
	}

	client, err := NewClient(context.Background(), apiKey)
	if err != nil {
		t.Fatalf("NewClient() returned unexpected error: %v", err)
	}

	result, err := client.FetchLatestVideos(context.Background(), testChannelID, 5)
	if err != nil {
		t.Fatalf("FetchLatestVideos() returned unexpected error: %v", err)
	}
	if result.QuotaUsed != 3 {
		t.Errorf("QuotaUsed = %d, want 3", result.QuotaUsed)
	}
	if len(result.Videos) == 0 {
		t.Fatal("expected at least one video, got none")
	}
	for _, v := range result.Videos {
		if v.ID == "" {
			t.Error("video has empty ID")
		}
		if v.Title == "" {
			t.Error("video has empty Title")
		}
	}
}

func TestFetchLatestVideos_ReturnsErrChannelNotFoundForInvalidID(t *testing.T) {
	apiKey := os.Getenv("YOUTUBE_API_KEY")
	if apiKey == "" {
		t.Skip("YOUTUBE_API_KEY not set; skipping integration test against the real YouTube Data API")
	}

	client, err := NewClient(context.Background(), apiKey)
	if err != nil {
		t.Fatalf("NewClient() returned unexpected error: %v", err)
	}

	_, err = client.FetchLatestVideos(context.Background(), "UC_this_channel_does_not_exist_00000", 5)
	if !errors.Is(err, ErrChannelNotFound) {
		t.Errorf("err = %v, want ErrChannelNotFound", err)
	}
}
