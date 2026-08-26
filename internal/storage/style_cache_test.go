package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestUpsertChannelStyle_ThenGetChannelStyle(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-style-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelStyle(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelStyle() returned error: %v", err)
		}
	})

	computedAt := time.Now().Truncate(time.Second)
	expiresAt := computedAt.Add(48 * time.Hour)
	summaryJSON := []byte(`{"video_count_analyzed":10,"confidence":"high"}`)

	if err := store.UpsertChannelStyle(ctx, channelID, summaryJSON, 10, computedAt, expiresAt); err != nil {
		t.Fatalf("UpsertChannelStyle() returned unexpected error: %v", err)
	}

	found, err := store.GetChannelStyle(ctx, channelID)
	if err != nil {
		t.Fatalf("GetChannelStyle() returned unexpected error: %v", err)
	}
	if found.VideoCountAnalyzed != 10 {
		t.Errorf("VideoCountAnalyzed = %d, want 10", found.VideoCountAnalyzed)
	}

	// jsonb reorders object keys internally (by length, then lexicographically),
	// so comparing raw bytes against the original input is the wrong check —
	// compare the parsed content instead.
	var foundValue map[string]any
	if err := json.Unmarshal(found.SummaryJSON, &foundValue); err != nil {
		t.Fatalf("json.Unmarshal(found.SummaryJSON) returned unexpected error: %v", err)
	}
	if foundValue["confidence"] != "high" {
		t.Errorf("SummaryJSON[\"confidence\"] = %v, want %q", foundValue["confidence"], "high")
	}
}

func TestUpsertChannelStyle_UpdatesOnConflict(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()
	channelID := "UC-style-test-" + randomHash(t)[:8]

	t.Cleanup(func() {
		if err := store.DeleteChannelStyle(context.Background(), channelID); err != nil {
			t.Errorf("cleanup DeleteChannelStyle() returned error: %v", err)
		}
	})

	now := time.Now().Truncate(time.Second)
	if err := store.UpsertChannelStyle(ctx, channelID, []byte(`{"video_count_analyzed":1}`), 1, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("UpsertChannelStyle() (first) returned unexpected error: %v", err)
	}
	if err := store.UpsertChannelStyle(ctx, channelID, []byte(`{"video_count_analyzed":2}`), 2, now, now.Add(time.Hour)); err != nil {
		t.Fatalf("UpsertChannelStyle() (second) returned unexpected error: %v", err)
	}

	found, err := store.GetChannelStyle(ctx, channelID)
	if err != nil {
		t.Fatalf("GetChannelStyle() returned unexpected error: %v", err)
	}
	if found.VideoCountAnalyzed != 2 {
		t.Errorf("VideoCountAnalyzed = %d, want 2 (update, not duplicate)", found.VideoCountAnalyzed)
	}
}

func TestGetChannelStyle_ReturnsErrNotFoundForUnknownChannel(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetChannelStyle(context.Background(), "UC-does-not-exist-"+randomHash(t)[:8])
	if !errors.Is(err, ErrStyleNotFound) {
		t.Errorf("err = %v, want ErrStyleNotFound", err)
	}
}
