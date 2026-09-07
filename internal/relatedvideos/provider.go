package relatedvideos

import (
	"context"
	"fmt"

	"github.com/juansecalvinio/ytpublisher-api/internal/embeddings"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type VideoStore interface {
	ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error)
	UpdateChannelVideoEmbedding(ctx context.Context, channelID, videoID string, embedding []float32) error
	FindSimilarVideos(ctx context.Context, channelID string, queryEmbedding []float32, limit int) ([]storage.ChannelVideo, error)
}

type Embedder interface {
	EmbedDocuments(ctx context.Context, texts []string) ([][]float32, embeddings.Usage, error)
	EmbedQuery(ctx context.Context, text string) ([]float32, embeddings.Usage, error)
}

type Usage struct {
	EmbeddingCalls  int
	EmbeddingTokens int64
}

type Provider struct {
	videos   VideoStore
	embedder Embedder
}

func NewProvider(videos VideoStore, embedder Embedder) *Provider {
	return &Provider{videos: videos, embedder: embedder}
}

func (p *Provider) FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, Usage, error) {
	var usage Usage

	videos, err := p.videos.ListChannelVideos(ctx, channelID)
	if err != nil {
		return nil, usage, fmt.Errorf("relatedvideos: listing videos: %w", err)
	}

	var missing []storage.ChannelVideo
	for _, v := range videos {
		if len(v.Embedding) == 0 {
			missing = append(missing, v)
		}
	}

	if len(missing) > 0 {
		texts := make([]string, len(missing))
		for i, v := range missing {
			texts[i] = v.Title + "\n\n" + v.Description
		}
		newEmbeddings, embedUsage, err := p.embedder.EmbedDocuments(ctx, texts)
		usage.EmbeddingCalls++
		usage.EmbeddingTokens += embedUsage.TotalTokens
		if err != nil {
			return nil, usage, fmt.Errorf("relatedvideos: embedding videos: %w", err)
		}
		for i, v := range missing {
			if err := p.videos.UpdateChannelVideoEmbedding(ctx, channelID, v.VideoID, newEmbeddings[i]); err != nil {
				return nil, usage, fmt.Errorf("relatedvideos: storing embedding: %w", err)
			}
		}
	}

	queryEmbedding, queryUsage, err := p.embedder.EmbedQuery(ctx, topic)
	usage.EmbeddingCalls++
	usage.EmbeddingTokens += queryUsage.TotalTokens
	if err != nil {
		return nil, usage, fmt.Errorf("relatedvideos: embedding topic: %w", err)
	}

	results, err := p.videos.FindSimilarVideos(ctx, channelID, queryEmbedding, limit)
	if err != nil {
		return nil, usage, fmt.Errorf("relatedvideos: searching similar videos: %w", err)
	}
	return results, usage, nil
}
