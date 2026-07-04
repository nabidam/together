package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"together/internal/auth"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

// ponytail: room privacy/passwords deferred; add rooms.password_hash when >2 users actually want it
func Routes(mux *http.ServeMux, d *sql.DB) {
	mux.HandleFunc("GET /api/rooms", auth.Require(d, false, func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.Query(`SELECT id, name, owner_id, created_at FROM rooms ORDER BY created_at DESC`)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		defer rows.Close()
		type room struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			OwnerID   int64  `json:"ownerId"`
			CreatedAt int64  `json:"createdAt"`
		}
		out := []room{}
		for rows.Next() {
			var x room
			rows.Scan(&x.ID, &x.Name, &x.OwnerID, &x.CreatedAt)
			out = append(out, x)
		}
		writeJSON(w, out)
	}))

	mux.HandleFunc("POST /api/rooms", auth.Require(d, false, func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Name string }
		json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in)
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || len(in.Name) > 60 {
			http.Error(w, "name required (max 60 chars)", 400)
			return
		}
		res, err := d.Exec(`INSERT INTO rooms (name, owner_id) VALUES (?,?)`, in.Name, auth.From(r).ID)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		id, _ := res.LastInsertId()
		writeJSON(w, map[string]any{"id": id, "name": in.Name})
	}))

	mux.HandleFunc("DELETE /api/rooms/{id}", auth.Require(d, false, func(w http.ResponseWriter, r *http.Request) {
		u := auth.From(r)
		res, err := d.Exec(`DELETE FROM rooms WHERE id=? AND (owner_id=? OR 'admin'=?)`, pathID(r), u.ID, u.Role)
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			http.Error(w, "not found or not yours", 404)
			return
		}
		w.WriteHeader(204)
	}))

	mux.HandleFunc("GET /api/rooms/{id}/messages", auth.Require(d, false, func(w http.ResponseWriter, r *http.Request) {
		rows, err := d.Query(`SELECT m.id, m.user_id, u.username, m.body, m.created_at
			FROM messages m JOIN users u ON u.id=m.user_id
			WHERE m.room_id=? ORDER BY m.id DESC LIMIT 50`, pathID(r))
		if err != nil {
			http.Error(w, "server error", 500)
			return
		}
		defer rows.Close()
		type msg struct {
			ID        int64  `json:"id"`
			UserID    int64  `json:"userId"`
			Username  string `json:"username"`
			Body      string `json:"body"`
			CreatedAt int64  `json:"createdAt"`
		}
		out := []msg{}
		for rows.Next() {
			var m msg
			rows.Scan(&m.ID, &m.UserID, &m.Username, &m.Body, &m.CreatedAt)
			out = append(out, m)
		}
		for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 { // ascending for UI
			out[i], out[j] = out[j], out[i]
		}
		writeJSON(w, out)
	}))
}
