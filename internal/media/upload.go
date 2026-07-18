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

const (
	DefaultMaxUploadBytes int64 = 20 << 30
	maxCreateBody               = 4 << 10
	maxChunkBody                = 8 << 20
	maxSubtitleBody             = 10 << 20
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

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func UploadRoutes(mux *http.ServeMux, d *sql.DB, dataDir string, maxUploadBytes int64) {
	os.MkdirAll(filepath.Join(dataDir, "uploads"), 0o755)
	adm := func(h http.HandlerFunc) http.HandlerFunc { return auth.Require(d, true, h) }

	mux.HandleFunc("POST /api/admin/media", adm(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(io.LimitReader(r.Body, maxCreateBody+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if len(body) > maxCreateBody {
			writeError(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		var in struct {
			Title, OrigName string
			SizeBytes       int64
		}
		if json.Unmarshal(body, &in) != nil || in.Title == "" || in.SizeBytes <= 0 {
			writeError(w, http.StatusBadRequest, "title and positive sizeBytes required")
			return
		}
		if in.SizeBytes > maxUploadBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "upload too large")
			return
		}
		// kind is provisional until the worker probes the uploaded bytes; the
		// schema requires a value before those bytes exist. Client-supplied kind
		// fields are deliberately ignored and can never select the pipeline path.
		res, err := d.Exec(`INSERT INTO media (kind, title, orig_name, size_bytes) VALUES ('video',?,?,?)`, in.Title, in.OrigName, in.SizeBytes)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server error")
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
		var total sql.NullInt64
		if d.QueryRow(`SELECT status, size_bytes FROM media WHERE id=?`, id).Scan(&status, &total) != nil || status != "uploading" {
			writeError(w, http.StatusConflict, "not uploading")
			return
		}
		offset, err := strconv.ParseInt(r.URL.Query().Get("offset"), 10, 64)
		if err != nil || offset < 0 {
			writeError(w, http.StatusConflict, "offset mismatch")
			return
		}
		declared, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
		if err != nil || declared <= 0 || declared > maxUploadBytes {
			writeError(w, http.StatusBadRequest, "valid Upload-Length required")
			return
		}
		if !total.Valid || total.Int64 <= 0 {
			if _, err := d.Exec(`UPDATE media SET size_bytes=? WHERE id=? AND status='uploading' AND (size_bytes IS NULL OR size_bytes <= 0)`, declared, id); err != nil {
				writeError(w, http.StatusInternalServerError, "server error")
				return
			}
			total = sql.NullInt64{Int64: declared, Valid: true}
		} else if total.Int64 != declared {
			writeError(w, http.StatusConflict, "total mismatch")
			return
		}
		f, err := os.OpenFile(UploadPath(dataDir, id), os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server error")
			return
		}
		defer f.Close()
		// ponytail: Stat-then-Write is not atomic; single admin uploads sequentially, add per-id lock if uploads ever go concurrent
		if fi, _ := f.Stat(); fi.Size() != offset {
			writeError(w, http.StatusConflict, fmt.Sprintf("offset mismatch, have %d", fi.Size()))
			return
		}
		f.Seek(offset, io.SeekStart)
		allowed := min(maxChunkBody, total.Int64-offset)
		if allowed < 0 {
			writeError(w, http.StatusConflict, "upload exceeds declared size")
			return
		}
		n, err := io.Copy(f, io.LimitReader(r.Body, allowed+1))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "write failed")
			return
		}
		if n > allowed {
			if err := f.Truncate(offset); err != nil {
				writeError(w, http.StatusInternalServerError, "write failed")
				return
			}
			if allowed == maxChunkBody {
				writeError(w, http.StatusRequestEntityTooLarge, "chunk too large")
			} else {
				writeError(w, http.StatusConflict, "upload exceeds declared size")
			}
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
		body, err := io.ReadAll(io.LimitReader(r.Body, maxSubtitleBody+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid subtitle")
			return
		}
		if len(body) > maxSubtitleBody {
			writeError(w, http.StatusRequestEntityTooLarge, "body too large")
			return
		}
		f, err := os.Create(path)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		defer f.Close()
		if _, err := f.Write(body); err != nil {
			http.Error(w, "write failed", 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]int{"id": len(subs)})
	}))

	mux.HandleFunc("POST /api/admin/media/{id}/finish", adm(func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var total sql.NullInt64
		if d.QueryRow(`SELECT size_bytes FROM media WHERE id=? AND status='uploading'`, id).Scan(&total) != nil || !total.Valid || total.Int64 <= 0 {
			writeError(w, http.StatusConflict, "upload incomplete")
			return
		}
		fi, err := os.Stat(UploadPath(dataDir, id))
		if err != nil || fi.Size() != total.Int64 {
			writeError(w, http.StatusConflict, "upload incomplete")
			return
		}
		res, err := d.Exec(`UPDATE media SET status='processing' WHERE id=? AND status='uploading'`, id)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "not found or already finished", 409)
			return
		}
		if _, err := d.Exec(`INSERT INTO jobs (media_id) VALUES (?)`, id); err != nil {
			d.Exec(`UPDATE media SET status='uploading' WHERE id=?`, id) // undo the flip; media would be stuck 'processing' with no job
			http.Error(w, "server error", 500)
			return
		}
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
