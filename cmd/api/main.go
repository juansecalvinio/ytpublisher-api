package main

import (
	"context"
	"log"
	"net/http"

	"github.com/juansecalvinio/ytpublisher-api/internal/api"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func main() {
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
	router := api.NewRouter(store, store)

	log.Printf("listening on :%s", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}
