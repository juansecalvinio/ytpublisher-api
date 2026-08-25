package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/juansecalvinio/ytpublisher-api/internal/apikey"
	"github.com/juansecalvinio/ytpublisher-api/internal/config"
	"github.com/juansecalvinio/ytpublisher-api/internal/storage"
)

func main() {
	name := flag.String("name", "", "client name (required)")
	email := flag.String("email", "", "client email (required)")
	flag.Parse()

	if *name == "" || *email == "" {
		log.Fatal("both -name and -email are required")
	}

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

	plainKey, err := apikey.Generate()
	if err != nil {
		log.Fatalf("apikey: %v", err)
	}

	store := storage.NewStore(pool)
	client, err := store.CreateClient(ctx, *name, *email, apikey.Hash(plainKey))
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	fmt.Print(formatIssuedKeyMessage(client, plainKey))
}

func formatIssuedKeyMessage(client storage.Client, plainKey string) string {
	return fmt.Sprintf(
		"API key created for %s <%s>\nClient ID: %s\nAPI Key (save this now, it will not be shown again): %s\n",
		client.Name, client.Email, client.ID, plainKey,
	)
}
