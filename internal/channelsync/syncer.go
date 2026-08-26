package channelsync

import (
	"context"
	"errors"
	"fmt"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

var ErrQuotaExceeded = errors.New("channelsync: daily YouTube quota cap reached")

type YouTubeFetcher interface {
	FetchLatestVideos(ctx context.Context, channelID string, maxResults int) (youtube.FetchResult, error)
}

type QuotaTracker interface {
	UnitsUsedToday(ctx context.Context) (int, error)
	IncrementUnitsUsed(ctx context.Context, units int) error
}

type VideoStore interface {
	UpsertChannelVideos(ctx context.Context, channelID string, videos []storage.ChannelVideo) error
}

const estimatedUnitsPerSync = 3

type Syncer struct {
	fetcher    YouTubeFetcher
	quota      QuotaTracker
	store      VideoStore
	maxResults int
	dailyCap   int
}

func NewSyncer(fetcher YouTubeFetcher, quota QuotaTracker, store VideoStore, maxResults, dailyCap int) *Syncer {
	return &Syncer{fetcher: fetcher, quota: quota, store: store, maxResults: maxResults, dailyCap: dailyCap}
}

type Result struct {
	VideosSynced int
	QuotaUsed    int
}

func (s *Syncer) SyncChannel(ctx context.Context, channelID string) (Result, error) {
	used, err := s.quota.UnitsUsedToday(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("channelsync: checking quota: %w", err)
	}
	if used+estimatedUnitsPerSync > s.dailyCap {
		return Result{}, ErrQuotaExceeded
	}

	fetchResult, fetchErr := s.fetcher.FetchLatestVideos(ctx, channelID, s.maxResults)

	if fetchResult.QuotaUsed > 0 {
		if err := s.quota.IncrementUnitsUsed(ctx, fetchResult.QuotaUsed); err != nil {
			return Result{}, fmt.Errorf("channelsync: recording quota usage: %w", err)
		}
	}

	if fetchErr != nil {
		return Result{QuotaUsed: fetchResult.QuotaUsed}, fetchErr
	}

	videos := make([]storage.ChannelVideo, 0, len(fetchResult.Videos))
	for _, v := range fetchResult.Videos {
		videos = append(videos, storage.ChannelVideo{
			ChannelID:   channelID,
			VideoID:     v.ID,
			Title:       v.Title,
			Description: v.Description,
			Tags:        v.Tags,
			PublishedAt: v.PublishedAt,
		})
	}

	if err := s.store.UpsertChannelVideos(ctx, channelID, videos); err != nil {
		return Result{QuotaUsed: fetchResult.QuotaUsed}, fmt.Errorf("channelsync: storing videos: %w", err)
	}

	return Result{VideosSynced: len(videos), QuotaUsed: fetchResult.QuotaUsed}, nil
}
