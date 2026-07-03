package media

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"together/internal/auth"
)

func UploadPath(dataDir string, id int64) string {
	return filepath.Join(dataDir, "uploads", strconv.FormatInt(id, 10))
}

func SubPaths(dataDir string, id int64) ([]string, error) {
	return filepath.Glob(UploadPath(dataDir, id) + ".sub.*")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func UploadRoutes(mux *http.ServeMux, d *sql.DB, dataDir string) {
	os.MkdirAll(filepath.Join(dataDir, "uploads"), 0o755)
	adm := func(h http.HandlerFunc) http.HandlerFunc { return auth.Require(d, true, h) }

	mux.HandleFunc("POST /api/admin/media", adm(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Kind, Title, OrigName string }
		json.NewDecoder(r.Body).Decode(&in)
		if (in.Kind != "movie" && in.Kind != "music") || in.Title == "" {
			http.Error(w, "kind must be movie|music, title required", 400)
			return
		}
		res, err := d.Exec(`INSERT INTO media (kind, title, orig_name) VALUES (?,?,?)`, in.Kind, in.Title, in.OrigName)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		id, _ := res.LastInsertId()
		writeJSON(w, map[string]int64{"id": id})
	}))

	mux.HandleFunc("GET /api/admin/media/{id}/blob", adm(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		fi, err := os.Stat(UploadPath(dataDir, id))
		size := int64(0)
		if err == nil {
			size = fi.Size()
		}
		writeJSON(w, map[string]int64{"size": size})
	}))

	mux.HandleFunc("PATCH /api/admin/media/{id}/blob", adm(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var status string
		if d.QueryRow(`SELECT status FROM media WHERE id=?`, id).Scan(&status) != nil || status != "uploading" {
			http.Error(w, "not uploading", 409)
			return
		}
		offset, _ := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		f, err := os.OpenFile(UploadPath(dataDir, id), os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		defer f.Close()
		if fi, _ := f.Stat(); fi.Size() != offset {
			http.Error(w, fmt.Sprintf("offset mismatch, have %d", fi.Size()), 409)
			return
		}
		f.Seek(offset, io.SeekStart)
		n, err := io.Copy(f, r.Body)
		if err != nil {
			http.Error(w, "write failed", 500)
			return
		}
		writeJSON(w, map[string]int64{"size": offset + n})
	}))

	mux.HandleFunc("POST /api/admin/media/{id}/subtitle", adm(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		subs, _ := SubPaths(dataDir, id)
		label := r.URL.Query().Get("label")
		if label == "" {
			label = "Subtitles"
		}
		// label is stored in the filename between .sub.N. and extension-safe encoding is skipped
		// ponytail: labels limited to filename-safe chars; sanitize
		path := fmt.Sprintf("%s.sub.%d.%s.srt", UploadPath(dataDir, id), len(subs), sanitize(label))
		f, err := os.Create(path)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		defer f.Close()
		if _, err := io.Copy(f, io.LimitReader(r.Body, 10<<20)); err != nil {
			http.Error(w, "write failed", 500)
			return
		}
		w.WriteHeader(201)
	}))

	mux.HandleFunc("POST /api/admin/media/{id}/finish", adm(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		res, _ := d.Exec(`UPDATE media SET status='processing' WHERE id=? AND status='uploading'`, id)
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "not found or already finished", 409)
			return
		}
		d.Exec(`INSERT INTO jobs (media_id) VALUES (?)`, id)
		w.WriteHeader(202)
	}))
}

func sanitize(s string) string {
	out := []rune{}
	for _, r := range s {
		if r == '/' || r == '\\' || r == '.' || r == 0 {
			continue
		}
		out = append(out, r)
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return string(out)
}
