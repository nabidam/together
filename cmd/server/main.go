package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"together/internal/api"
	"together/internal/auth"
	"together/internal/db"
	"together/internal/live"
	"together/internal/media"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dataDir := env("TOGETHER_DATA", "./data")
	if err := os.MkdirAll(filepath.Join(dataDir, "media"), 0o755); err != nil {
		log.Fatal(err)
	}
	d, err := db.Open(filepath.Join(dataDir, "together.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()

	if err := auth.Seed(d, os.Getenv("ADMIN_USER"), os.Getenv("ADMIN_PASS")); err != nil {
		log.Fatal(err)
	}

	hub := live.NewHub(d)
	if err := hub.Restore(); err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	// ponytail: SameSite=Lax + HttpOnly suffices behind TLS proxy on private instance
	auth.Routes(mux, d)
	api.Routes(mux, d)
	media.UploadRoutes(mux, d, dataDir)
	media.ServeRoutes(mux, d)
	mux.HandleFunc("GET /ws/{id}", auth.Require(d, false, hub.Handle))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go media.Worker(ctx, d, dataDir)

	addr := env("TOGETHER_ADDR", ":8080")
	log.Println("listening on", addr)
	// ponytail: no graceful HTTP drain; clients reconnect and resync by design
	log.Fatal(http.ListenAndServe(addr, mux))
}
