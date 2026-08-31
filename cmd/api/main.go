package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/joho/godotenv"
	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/channelsync"
	"github.com/juansecalvinio/ytpublisher-api/internal/claude"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/embeddings"
	"github.com/juansecalvinio/ytpublisher-api/internal/generation"
	"github.com/juansecalvinio/ytpublisher-api/internal/relatedvideos"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
	"github.com/juansecalvinio/ytpublisher-api/internal/stylecache"
	"github.com/juansecalvinio/ytpublisher-api/internal/youtube"
)

const maxVideosPerChannel = 25

func main() {
	// .env is only present in local dev; in production the real
	// environment variables are set directly (systemd), so a missing
	// file here is expected and not an error.
	_ = godotenv.Load()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx := context.Background()
	pool, err := storage.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer pool.Close()
	log.Println("connected to database")

	store := storage.NewStore(pool)

	youtubeClient, err := youtube.NewClient(ctx, cfg.YouTubeAPIKey)
	if err != nil {
		log.Fatalf("youtube: %v", err)
	}
	syncer := channelsync.NewSyncer(youtubeClient, store, store, maxVideosPerChannel, cfg.YouTubeDailyQuotaCap)

	styleTTL := time.Duration(cfg.StyleCacheTTLHours) * time.Hour
	styleProvider := stylecache.NewProvider(store, store, styleTTL)

	embeddingsClient := embeddings.NewClient(cfg.VoyageAPIKey, cfg.VoyageModel)
	relatedVideosProvider := relatedvideos.NewProvider(store, embeddingsClient)

	claudeClient := claude.NewClient(cfg.AnthropicAPIKey, cfg.AnthropicModel)
	generator := generation.NewOrchestrator(styleProvider, relatedVideosProvider, claudeClient)

	router := api.NewRouter(api.Dependencies{
		Finder:        store,
		Recorder:      store,
		Syncer:        syncer,
		StyleProvider: styleProvider,
		RelatedVideos: relatedVideosProvider,
		Generator:     generator,
	})

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
