package media

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"together/internal/auth"
)

func mediaPath(d *sql.DB, id int64) (string, bool) {
	var fp sql.NullString
	var status string
	if d.QueryRow(`SELECT file_path, status FROM media WHERE id=?`, id).Scan(&fp, &status) != nil {
		return "", false
	}
	return fp.String, status == "ready" && fp.Valid
}

// ServeRoutes mounts the library list, admin delete, and the three
// room-scoped media-byte routes. roomGate is live.RequireRoom's media
// variant (live.Hub.RequireRoomMedia), supplied by cmd/server/main.go — this
// package must not import internal/live (ARCHITECTURE §7 module direction),
// so the gate arrives as a plain middleware value instead.
func ServeRoutes(mux *http.ServeMux, d *sql.DB, dataDir string, roomGate func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/media", auth.Require(d, false, func(w http.ResponseWriter, r *http.Request) {
		u := auth.From(r)
		q := `SELECT id, kind, title, status, COALESCE(duration,0), COALESCE(size_bytes,0), COALESCE(error,'') FROM media`
		if u.Role != "admin" {
			q += ` WHERE status='ready'`
		}
		if k := r.URL.Query().Get("kind"); k == "video" || k == "audio" {
			if u.Role != "admin" {
				q += ` AND kind='` + k + `'`
			} else {
				q += ` WHERE kind='` + k + `'`
			}
		}
		q += ` ORDER BY created_at DESC`
		rows, err := d.Query(q)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		defer rows.Close()
		type sub struct {
			ID    int64  `json:"id"`
			Label string `json:"label"`
		}
		type item struct {
			ID        int64   `json:"id"`
			Kind      string  `json:"kind"`
			Title     string  `json:"title"`
			Status    string  `json:"status"`
			Duration  float64 `json:"duration"`
			SizeBytes int64   `json:"sizeBytes"`
			Error     string  `json:"error,omitempty"`
			Subtitles []sub   `json:"subtitles"`
		}
		out := []item{}
		for rows.Next() {
			var it item
			rows.Scan(&it.ID, &it.Kind, &it.Title, &it.Status, &it.Duration, &it.SizeBytes, &it.Error)
			it.Subtitles = []sub{}
			if srows, err := d.Query(`SELECT id, label FROM subtitles WHERE media_id=?`, it.ID); err == nil {
				for srows.Next() {
					var s sub
					srows.Scan(&s.ID, &s.Label)
					it.Subtitles = append(it.Subtitles, s)
				}
				srows.Close()
			}
			out = append(out, it)
		}
		writeJSON(w, out)
	}))

	serve := func(download bool) http.HandlerFunc {
		return roomGate(func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
			fp, ok := mediaPath(d, id)
			if !ok {
				http.Error(w, "not found", 404)
				return
			}
			if download {
				w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(fp)+`"`)
			}
			http.ServeFile(w, r, fp) // Range requests handled by stdlib
		})
	}
	mux.HandleFunc("GET /media/{id}/stream", serve(false))
	mux.HandleFunc("GET /media/{id}/download", serve(true))

	mux.HandleFunc("GET /media/{id}/subs/{sid}", roomGate(func(w http.ResponseWriter, r *http.Request) {
		sid, _ := strconv.ParseInt(r.PathValue("sid"), 10, 64)
		mid, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		// Gate on parent media readiness
		if _, ok := mediaPath(d, mid); !ok {
			http.Error(w, "not found", 404)
			return
		}
		var fp string
		if d.QueryRow(`SELECT file_path FROM subtitles WHERE id=? AND media_id=?`, sid, mid).Scan(&fp) != nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "text/vtt")
		http.ServeFile(w, r, fp)
	}))

	mux.HandleFunc("DELETE /api/admin/media/{id}", auth.Require(d, true, func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		var fp sql.NullString
		d.QueryRow(`SELECT file_path FROM media WHERE id=?`, id).Scan(&fp)
		if fp.Valid {
			os.Remove(fp.String)
		}
		os.Remove(UploadPath(dataDir, id)) // partial/unprocessed upload blob
		if sidecars, err := SubPaths(dataDir, id); err == nil {
			for _, sp := range sidecars {
				os.Remove(sp)
			}
		}
		if rows, err := d.Query(`SELECT file_path FROM subtitles WHERE media_id=?`, id); err == nil {
			for rows.Next() {
				var sp string
				rows.Scan(&sp)
				os.Remove(sp)
			}
			rows.Close()
		}
		d.Exec(`DELETE FROM subtitles WHERE media_id=?`, id)
		d.Exec(`DELETE FROM jobs WHERE media_id=?`, id)
		d.Exec(`DELETE FROM media WHERE id=?`, id)
		w.WriteHeader(204)
	}))
}
