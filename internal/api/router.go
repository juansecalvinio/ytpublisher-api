package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Dependencies struct {
	Finder        ClientFinder
	Recorder      UsageRecorder
	Syncer        ChannelSyncer
	StyleProvider StyleProvider
	RelatedVideos RelatedVideosProvider
}

func NewRouter(deps Dependencies) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/healthz", handleHealthz)

	r.Group(func(r chi.Router) {
		r.Use(RequireAPIKey(deps.Finder, deps.Recorder))
		r.Get("/v1/whoami", handleWhoami)
		r.Post("/v1/internal/channels/{channelID}/sync", handleChannelSync(deps.Syncer))
		r.Get("/v1/internal/channels/{channelID}/style", handleChannelStyle(deps.StyleProvider))
		r.Get("/v1/internal/channels/{channelID}/related-videos", handleRelatedVideos(deps.RelatedVideos))
	})

	return r
}

func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func handleWhoami(w http.ResponseWriter, r *http.Request) {
	client, ok := ClientFromContext(r.Context())
	if !ok {
		http.Error(w, "internal error: client not found in context", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"client_id": client.ID,
		"name":      client.Name,
	})
}
