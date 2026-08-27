package relatedvideos

import (
	"context"
	"testing"

	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type fakeVideoStore struct {
	videos            []storage.ChannelVideo
	updatedEmbeddings map[string][]float32
	similarResult     []storage.ChannelVideo
}

func (f *fakeVideoStore) ListChannelVideos(ctx context.Context, channelID string) ([]storage.ChannelVideo, error) {
	return f.videos, nil
}

func (f *fakeVideoStore) UpdateChannelVideoEmbedding(ctx context.Context, channelID, videoID string, embedding []float32) error {
	if f.updatedEmbeddings == nil {
		f.updatedEmbeddings = map[string][]float32{}
	}
	f.updatedEmbeddings[videoID] = embedding
	return nil
}

func (f *fakeVideoStore) FindSimilarVideos(ctx context.Context, channelID string, queryEmbedding []float32, limit int) ([]storage.ChannelVideo, error) {
	return f.similarResult, nil
}

type fakeEmbedder struct {
	documentCalls [][]string
	queryCalls    []string
}

func (f *fakeEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	f.documentCalls = append(f.documentCalls, texts)
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = []float32{float32(i)}
	}
	return result, nil
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	f.queryCalls = append(f.queryCalls, text)
	return []float32{1}, nil
}

func TestFindRelated_EmbedsOnlyMissingVideosBeforeSearching(t *testing.T) {
	videoStore := &fakeVideoStore{
		videos: []storage.ChannelVideo{
			{VideoID: "v1", Title: "Video 1"},
			{VideoID: "v2", Title: "Video 2", Embedding: []float32{0.5}},
		},
		similarResult: []storage.ChannelVideo{{VideoID: "v1", Title: "Video 1"}},
	}
	embedder := &fakeEmbedder{}
	provider := NewProvider(videoStore, embedder)

	results, err := provider.FindRelated(context.Background(), "UC123", "some topic", 5)
	if err != nil {
		t.Fatalf("FindRelated() returned unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("len(results) = %d, want 1", len(results))
	}
	if len(embedder.documentCalls) != 1 || len(embedder.documentCalls[0]) != 1 {
		t.Errorf("documentCalls = %v, want exactly 1 call embedding exactly 1 text (only v1, which lacked an embedding)", embedder.documentCalls)
	}
	if _, ok := videoStore.updatedEmbeddings["v1"]; !ok {
		t.Error("expected v1's embedding to be updated")
	}
	if _, ok := videoStore.updatedEmbeddings["v2"]; ok {
		t.Error("v2 already had an embedding, should not have been re-embedded")
	}
	if len(embedder.queryCalls) != 1 || embedder.queryCalls[0] != "some topic" {
		t.Errorf("queryCalls = %v, want [\"some topic\"]", embedder.queryCalls)
	}
}

func TestFindRelated_SkipsEmbeddingWhenAllVideosAlreadyEmbedded(t *testing.T) {
	videoStore := &fakeVideoStore{
		videos: []storage.ChannelVideo{
			{VideoID: "v1", Title: "Video 1", Embedding: []float32{0.1}},
		},
	}
	embedder := &fakeEmbedder{}
	provider := NewProvider(videoStore, embedder)

	_, err := provider.FindRelated(context.Background(), "UC123", "some topic", 5)
	if err != nil {
		t.Fatalf("FindRelated() returned unexpected error: %v", err)
	}
	if len(embedder.documentCalls) != 0 {
		t.Errorf("documentCalls = %v, want none (all videos already embedded)", embedder.documentCalls)
	}
}
