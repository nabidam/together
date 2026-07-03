package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"together/internal/api"
	"together/internal/auth"
	"together/internal/db"
	"together/internal/live"
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
	mux.HandleFunc("GET /ws/{id}", auth.Require(d, false, hub.Handle))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	addr := env("TOGETHER_ADDR", ":8080")
	log.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
