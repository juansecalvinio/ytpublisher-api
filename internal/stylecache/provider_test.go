package stylecache

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type fakeVideoLister struct {
	videos []storage.ChannelVideo
	calls  int
}

func (f *fakeVideoLister) ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error) {
	f.calls++
	return f.videos, nil
}

type fakeStyleStore struct {
	cached    storage.StyleCache
	hasCached bool
	upserted  bool
}

func (f *fakeStyleStore) GetChannelStyle(ctx context.Context, channelID string) (storage.StyleCache, error) {
	if !f.hasCached {
		return storage.StyleCache{}, storage.ErrStyleNotFound
	}
	return f.cached, nil
}

func (f *fakeStyleStore) UpsertChannelStyle(ctx context.Context, channelID string, summaryJSON []byte, videoCountAnalyzed int, computedAt, expiresAt time.Time) error {
	f.upserted = true
	f.cached = storage.StyleCache{ChannelID: channelID, SummaryJSON: summaryJSON, VideoCountAnalyzed: videoCountAnalyzed, ComputedAt: computedAt, ExpiresAt: expiresAt}
	f.hasCached = true
	return nil
}

func TestGetStyle_ComputesAndCachesWhenNothingCached(t *testing.T) {
	videos := &fakeVideoLister{videos: []storage.ChannelVideo{
		{Title: "How to code in Go?", Tags: []string{"go", "programming"}},
	}}
	styles := &fakeStyleStore{}
	provider := NewProvider(videos, styles, time.Hour)

	summary, err := provider.GetStyle(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("GetStyle() returned unexpected error: %v", err)
	}
	if summary.VideoCountAnalyzed != 1 {
		t.Errorf("VideoCountAnalyzed = %d, want 1", summary.VideoCountAnalyzed)
	}
	if videos.calls != 1 {
		t.Errorf("videos.calls = %d, want 1", videos.calls)
	}
	if !styles.upserted {
		t.Error("expected style to be cached via UpsertChannelStyle")
	}
}

func TestGetStyle_ReturnsCachedValueWithoutRecomputing(t *testing.T) {
	videos := &fakeVideoLister{}
	cachedSummary := styleanalysis.Summary{VideoCountAnalyzed: 42, Confidence: "high"}
	summaryJSON, err := json.Marshal(cachedSummary)
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	styles := &fakeStyleStore{
		hasCached: true,
		cached: storage.StyleCache{
			SummaryJSON: summaryJSON,
			ExpiresAt:   time.Now().Add(time.Hour),
		},
	}
	provider := NewProvider(videos, styles, time.Hour)

	summary, err := provider.GetStyle(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("GetStyle() returned unexpected error: %v", err)
	}
	if summary.VideoCountAnalyzed != 42 {
		t.Errorf("VideoCountAnalyzed = %d, want 42 (from cache)", summary.VideoCountAnalyzed)
	}
	if videos.calls != 0 {
		t.Errorf("videos.calls = %d, want 0 (should not recompute)", videos.calls)
	}
}

func TestGetStyle_RecomputesWhenCacheExpired(t *testing.T) {
	videos := &fakeVideoLister{videos: []storage.ChannelVideo{{Title: "Fresh video"}}}
	staleJSON, err := json.Marshal(styleanalysis.Summary{VideoCountAnalyzed: 1})
	if err != nil {
		t.Fatalf("json.Marshal() returned unexpected error: %v", err)
	}
	styles := &fakeStyleStore{
		hasCached: true,
		cached: storage.StyleCache{
			SummaryJSON: staleJSON,
			ExpiresAt:   time.Now().Add(-time.Hour),
		},
	}
	provider := NewProvider(videos, styles, time.Hour)

	_, err = provider.GetStyle(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("GetStyle() returned unexpected error: %v", err)
	}
	if videos.calls != 1 {
		t.Errorf("videos.calls = %d, want 1 (should recompute since cache expired)", videos.calls)
	}
}
