package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"together/internal/auth"
	"together/internal/db"
	"together/internal/live"
	"together/internal/media"
)

//go:embed all:webdist
var webFS embed.FS

func spa(mux *http.ServeMux) {
	sub, err := fs.Sub(webFS, "webdist")
	if err != nil {
		log.Fatal(err)
	}
	fileServer := http.FileServerFS(sub)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, strings.TrimPrefix(r.URL.Path, "/")); err != nil && r.URL.Path != "/" {
			r.URL.Path = "/" // SPA fallback (hash routing: only "/" really needed)
		}
		fileServer.ServeHTTP(w, r)
	})
}

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
	auth.GC(d)

	idle, err := time.ParseDuration(env("TOGETHER_ROOM_IDLE", "30m"))
	if err != nil {
		log.Fatal("bad TOGETHER_ROOM_IDLE: ", err)
	}
	maxUploadBytes, err := strconv.ParseInt(env("TOGETHER_MAX_UPLOAD_BYTES", strconv.FormatInt(media.DefaultMaxUploadBytes, 10)), 10, 64)
	if err != nil || maxUploadBytes <= 0 {
		log.Fatal("bad TOGETHER_MAX_UPLOAD_BYTES")
	}
	hub := live.NewHub(d, idle)

	mux := http.NewServeMux()
	// ponytail: SameSite=Lax + HttpOnly suffices behind TLS proxy on private instance
	auth.Routes(mux, d)
	hub.Routes(mux)
	media.UploadRoutes(mux, d, dataDir, maxUploadBytes)
	media.ServeRoutes(mux, d, dataDir, hub.RequireRoomMedia)
	mux.HandleFunc("GET /ws/{id}", hub.RequireRoom(hub.Handle))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	spa(mux)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go media.Worker(ctx, d, dataDir)
	// ponytail: exit on signal, no HTTP drain; clients reconnect and resync by design
	go func() { <-ctx.Done(); log.Println("shutting down"); os.Exit(0) }()

	addr := env("TOGETHER_ADDR", ":8080")
	log.Println("listening on", addr)
	// ponytail: no graceful HTTP drain; clients reconnect and resync by design
	// ReadHeaderTimeout only — no write/idle timeouts, they would kill WS and long media streams
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
