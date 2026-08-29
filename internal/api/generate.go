package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
)

type GenerationOrchestrator interface {
	Generate(ctx context.Context, input generation.Input) (generation.Output, error)
}

type generateRequest struct {
	ChannelID string   `json:"channel_id"`
	Topic     string   `json:"topic"`
	Notes     string   `json:"notes"`
	Language  string   `json:"language"`
	Links     []string `json:"links"`
	Mentions  []string `json:"mentions"`
	Tone      string   `json:"tone"`
}

func handleGenerate(orchestrator GenerationOrchestrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req generateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.ChannelID == "" || req.Topic == "" {
			writeJSONError(w, http.StatusBadRequest, "channel_id and topic are required")
			return
		}

		output, err := orchestrator.Generate(r.Context(), generation.Input{
			ChannelID: req.ChannelID,
			Topic:     req.Topic,
			Notes:     req.Notes,
			Language:  req.Language,
			Links:     req.Links,
			Mentions:  req.Mentions,
			Tone:      req.Tone,
		})
		if err != nil {
			log.Printf("generate: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to generate content")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"title":          output.Title,
			"description":    output.Description,
			"tags":           output.Tags,
			"related_videos": output.RelatedVideos,
			"warnings":       output.Warnings,
		})
	}
}
