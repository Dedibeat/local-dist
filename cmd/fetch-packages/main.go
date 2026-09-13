package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mtes/local-dist/internal/provider"
)

func main() {
	dataDir := flag.String("data", "./data", "directory containing catalog.json and packages/")
	flag.Parse()

	store, err := provider.Load(*dataDir)
	if err != nil {
		log.Fatalf("load provider data: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client := &http.Client{Timeout: 30 * time.Minute}
	if err := provider.FetchPackages(ctx, store, client, os.Stdout); err != nil {
		log.Fatal(err)
	}
}
