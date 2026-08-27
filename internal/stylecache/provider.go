package stylecache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type VideoLister interface {
	ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error)
}

type StyleStore interface {
	GetChannelStyle(ctx context.Context, channelID string) (storage.StyleCache, error)
	UpsertChannelStyle(ctx context.Context, channelID string, summaryJSON []byte, videoCountAnalyzed int, computedAt, expiresAt time.Time) error
}

type Provider struct {
	videos VideoLister
	styles StyleStore
	ttl    time.Duration
}

func NewProvider(videos VideoLister, styles StyleStore, ttl time.Duration) *Provider {
	return &Provider{videos: videos, styles: styles, ttl: ttl}
}

func (p *Provider) GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error) {
	cached, err := p.styles.GetChannelStyle(ctx, channelID)
	if err == nil && time.Now().Before(cached.ExpiresAt) {
		var summary styleanalysis.Summary
		if err := json.Unmarshal(cached.SummaryJSON, &summary); err != nil {
			return styleanalysis.Summary{}, err
		}
		return summary, nil
	}
	if err != nil && !errors.Is(err, storage.ErrStyleNotFound) {
		return styleanalysis.Summary{}, err
	}

	videos, err := p.videos.ListChannelVideos(ctx, channelID)
	if err != nil {
		return styleanalysis.Summary{}, err
	}

	summary := styleanalysis.Analyze(videos)

	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return styleanalysis.Summary{}, err
	}

	computedAt := time.Now()
	if err := p.styles.UpsertChannelStyle(ctx, channelID, summaryJSON, summary.VideoCountAnalyzed, computedAt, computedAt.Add(p.ttl)); err != nil {
		return styleanalysis.Summary{}, err
	}

	return summary, nil
}
