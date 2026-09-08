package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
	"github.com/juansecalvinio/ytpublisher-api/internal/pricing"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

type GenerationOrchestrator interface {
	Generate(ctx context.Context, input generation.Input) (generation.Output, error)
}

type UsageReporter interface {
	ReportUsage(ctx context.Context, stripeCustomerID string) error
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

func handleGenerate(orchestrator GenerationOrchestrator, reporter UsageReporter, recorder UsageRecorder) http.HandlerFunc {
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

		// Record whatever usage actually happened, even on failure — a
		// request that burned tokens before erroring still has a real cost.
		if requestID, ok := RequestIDFromContext(r.Context()); ok {
			cost := pricing.ClaudeCost(output.Usage.LLMInputTokens, output.Usage.LLMOutputTokens) +
				pricing.EmbeddingCost(output.Usage.EmbeddingTokens)
			if updateErr := recorder.UpdateUsageEvent(r.Context(), requestID, storage.UsageUpdate{
				LLMInputTokens:   output.Usage.LLMInputTokens,
				LLMOutputTokens:  output.Usage.LLMOutputTokens,
				EmbeddingCalls:   output.Usage.EmbeddingCalls,
				EstimatedCostUSD: cost,
			}); updateErr != nil {
				log.Printf("generate: failed to update usage event %s: %v", requestID, updateErr)
			}
		}

		if err != nil {
			log.Printf("generate: %v", err)
			writeJSONError(w, http.StatusInternalServerError, "failed to generate content")
			return
		}

		if client, ok := ClientFromContext(r.Context()); ok && client.StripeCustomerID != "" {
			if err := reporter.ReportUsage(r.Context(), client.StripeCustomerID); err != nil {
				// The customer already has their content; a billing report
				// failure shouldn't turn into a failed request for them.
				log.Printf("generate: failed to report usage for %s: %v", client.StripeCustomerID, err)
			}
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
