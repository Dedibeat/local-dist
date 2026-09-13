package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mtes/local-dist/internal/provider"
)

func main() {
	listen := flag.String("listen", envOr("LOCAL_DIST_LISTEN", ":8080"), "HTTP listen address")
	dataDir := flag.String("data", envOr("LOCAL_DIST_DATA", "./data"), "directory containing catalog.json, rooms.json, and packages/")
	logPath := flag.String("log", envOr("LOCAL_DIST_LOG", ""), "append logs to this file instead of stderr")
	flag.Parse()
	if *logPath != "" {
		logFile, err := os.OpenFile(*logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o640)
		if err != nil {
			log.Fatalf("open log file: %v", err)
		}
		defer logFile.Close()
		log.SetOutput(logFile)
	}

	store, err := provider.Load(*dataDir)
	if err != nil {
		log.Fatalf("load provider data: %v", err)
	}

	server := &http.Server{
		Addr:              *listen,
		Handler:           provider.NewHandler(store),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("local-dist listening on %s with data from %s", *listen, *dataDir)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
