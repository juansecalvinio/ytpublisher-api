package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type RelatedVideosProvider interface {
	FindRelated(ctx context.Context, channelID, topic string, limit int) ([]storage.ChannelVideo, error)
}

const defaultRelatedVideosLimit = 5

func handleRelatedVideos(provider RelatedVideosProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := chi.URLParam(r, "channelID")
		topic := r.URL.Query().Get("topic")
		if channelID == "" || topic == "" {
			writeJSONError(w, http.StatusBadRequest, "channel id and topic are required")
			return
		}

		videos, err := provider.FindRelated(r.Context(), channelID, topic, defaultRelatedVideosLimit)
		if err != nil {
			log.Printf("related videos: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to find related videos")
			return
		}

		related := make([]map[string]string, len(videos))
		for i, v := range videos {
			related[i] = map[string]string{"video_id": v.VideoID, "title": v.Title}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"channel_id":     channelID,
			"topic":          topic,
			"related_videos": related,
		})
	}
}
