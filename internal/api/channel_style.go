package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/juansecalvinio/ytpublisher-api/internal/styleanalysis"
)

type StyleProvider interface {
	GetStyle(ctx context.Context, channelID string) (styleanalysis.Summary, error)
}

func handleChannelStyle(provider StyleProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := chi.URLParam(r, "channelID")
		if channelID == "" {
			writeJSONError(w, http.StatusBadRequest, "channel id is required")
			return
		}

		summary, err := provider.GetStyle(r.Context(), channelID)
		if err != nil {
			log.Printf("channel style: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to compute channel style")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
	}
}
