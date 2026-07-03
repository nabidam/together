package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"together/internal/db"
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

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })

	addr := env("TOGETHER_ADDR", ":8080")
	log.Println("listening on", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
