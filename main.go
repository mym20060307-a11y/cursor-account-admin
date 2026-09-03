package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"cursor-account-admin/internal/handler"
	"cursor-account-admin/internal/localsync"
	"cursor-account-admin/internal/store"
)

//go:embed web/index.html
var indexHTML string

func main() {
	port := flag.Int("port", 9999, "HTTP server port")
	dataFile := flag.String("data", "accounts.json", "Path to accounts data file")
	syncInterval := flag.Duration("sync-interval", 5*time.Second, "Local Cursor account sync interval (0 to disable)")
	flag.Parse()

	// Initialize the store
	accountStore, err := store.New(*dataFile)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}
	log.Printf("Data file: %s", *dataFile)

	// Background: sync local Cursor login into this platform
	localsync.StartLocalSync(accountStore, *syncInterval)

	// Initialize the HTTP handler
	h := handler.New(accountStore, indexHTML)

	// Set up routes
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Start the server
	addr := fmt.Sprintf("0.0.0.0:%d", *port)
	log.Printf("Starting server at http://0.0.0.0:%d", *port)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
