package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

type ChannelSyncer interface {
	SyncChannel(ctx context.Context, channelID string) (channelsync.Result, error)
}

func handleChannelSync(syncer ChannelSyncer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		channelID := chi.URLParam(r, "channelID")
		if channelID == "" {
			writeJSONError(w, http.StatusBadRequest, "channel id is required")
			return
		}

		result, err := syncer.SyncChannel(r.Context(), channelID)
		if err != nil {
			switch {
			case errors.Is(err, youtube.ErrChannelNotFound):
				writeJSONError(w, http.StatusNotFound, "channel not found")
			case errors.Is(err, channelsync.ErrQuotaExceeded):
				w.Header().Set("Retry-After", strconv.Itoa(secondsUntilNextUTCMidnight()))
				writeJSONError(w, http.StatusTooManyRequests, "daily YouTube quota reached")
			default:
				log.Printf("channel sync: %v", err)
				writeJSONError(w, http.StatusInternalServerError, "failed to sync channel")
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"channel_id":    channelID,
			"videos_synced": result.VideosSynced,
			"quota_used":    result.QuotaUsed,
		})
	}
}

func secondsUntilNextUTCMidnight() int {
	now := time.Now().UTC()
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return int(nextMidnight.Sub(now).Seconds())
}
