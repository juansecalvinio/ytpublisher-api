package channelsync

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

type fakeFetcher struct {
	result youtube.FetchResult
	err    error
}

func (f *fakeFetcher) FetchLatestVideos(ctx context.Context, channelID string, maxResults int) (youtube.FetchResult, error) {
	return f.result, f.err
}

type fakeQuotaTracker struct {
	used        int
	incremented int
}

func (f *fakeQuotaTracker) UnitsUsedToday(ctx context.Context) (int, error) {
	return f.used, nil
}

func (f *fakeQuotaTracker) IncrementUnitsUsed(ctx context.Context, units int) error {
	f.incremented += units
	return nil
}

type fakeVideoStore struct {
	stored []storage.ChannelVideo
}

func (f *fakeVideoStore) UpsertChannelVideos(ctx context.Context, channelID string, videos []storage.ChannelVideo) error {
	f.stored = append(f.stored, videos...)
	return nil
}

func TestSyncChannel_StoresVideosAndRecordsQuota(t *testing.T) {
	fetcher := &fakeFetcher{result: youtube.FetchResult{
		Videos:    []youtube.Video{{ID: "v1", Title: "Video 1", PublishedAt: time.Now()}},
		QuotaUsed: 3,
	}}
	quota := &fakeQuotaTracker{used: 0}
	store := &fakeVideoStore{}
	syncer := NewSyncer(fetcher, quota, store, 25, 9000)

	result, err := syncer.SyncChannel(context.Background(), "UC123")
	if err != nil {
		t.Fatalf("SyncChannel() returned unexpected error: %v", err)
	}
	if result.VideosSynced != 1 {
		t.Errorf("VideosSynced = %d, want 1", result.VideosSynced)
	}
	if result.QuotaUsed != 3 {
		t.Errorf("QuotaUsed = %d, want 3", result.QuotaUsed)
	}
	if quota.incremented != 3 {
		t.Errorf("quota.incremented = %d, want 3", quota.incremented)
	}
	if len(store.stored) != 1 {
		t.Fatalf("len(store.stored) = %d, want 1", len(store.stored))
	}
}

func TestSyncChannel_RefusesWhenQuotaCapWouldBeExceeded(t *testing.T) {
	fetcher := &fakeFetcher{}
	quota := &fakeQuotaTracker{used: 8999}
	store := &fakeVideoStore{}
	syncer := NewSyncer(fetcher, quota, store, 25, 9000)

	_, err := syncer.SyncChannel(context.Background(), "UC123")
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Errorf("err = %v, want ErrQuotaExceeded", err)
	}
	if quota.incremented != 0 {
		t.Errorf("quota.incremented = %d, want 0 (should not call YouTube at all)", quota.incremented)
	}
}

func TestSyncChannel_RecordsPartialQuotaOnFetchError(t *testing.T) {
	fetcher := &fakeFetcher{
		result: youtube.FetchResult{QuotaUsed: 1},
		err:    youtube.ErrChannelNotFound,
	}
	quota := &fakeQuotaTracker{used: 0}
	store := &fakeVideoStore{}
	syncer := NewSyncer(fetcher, quota, store, 25, 9000)

	_, err := syncer.SyncChannel(context.Background(), "UC123")
	if !errors.Is(err, youtube.ErrChannelNotFound) {
		t.Errorf("err = %v, want youtube.ErrChannelNotFound", err)
	}
	if quota.incremented != 1 {
		t.Errorf("quota.incremented = %d, want 1 (partial cost still recorded)", quota.incremented)
	}
}
