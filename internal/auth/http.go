package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type ctxKey struct{}

func From(r *http.Request) User { u, _ := r.Context().Value(ctxKey{}).(User); return u }

func Require(d *sql.DB, adminOnly bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("session")
		if err != nil {
			http.Error(w, "unauthorized", 401)
			return
		}
		u, err := UserByToken(d, c.Value)
		if err != nil {
			http.Error(w, "unauthorized", 401)
			return
		}
		if adminOnly && u.Role != "admin" {
			http.Error(w, "forbidden", 403)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// ponytail: Secure when TLS-terminated by Caddy; plain http only in dev/tests
func secureCookie(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func setSession(w http.ResponseWriter, r *http.Request, d *sql.DB, u User) error {
	tok, err := CreateSession(d, u.ID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: tok, Path: "/",
		HttpOnly: true, Secure: secureCookie(r), SameSite: http.SameSiteLaxMode, MaxAge: 30 * 24 * 3600})
	return nil
}

func Routes(mux *http.ServeMux, d *sql.DB) {
	type creds struct{ Username, Password, Code string }

	mux.HandleFunc("POST /api/login", func(w http.ResponseWriter, r *http.Request) {
		var c creds
		json.NewDecoder(r.Body).Decode(&c)
		u, err := Login(d, c.Username, c.Password)
		if err != nil {
			http.Error(w, err.Error(), 401)
			return
		}
		if err := setSession(w, r, d, u); err != nil {
			http.Error(w, "server error", 500)
			return
		}
		writeJSON(w, u)
	})

	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, r *http.Request) {
		var c creds
		json.NewDecoder(r.Body).Decode(&c)
		if len(c.Username) < 2 || len(c.Password) < 8 {
			http.Error(w, "username min 2 chars, password min 8", 400)
			return
		}
		u, err := Register(d, c.Code, c.Username, c.Password)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := setSession(w, r, d, u); err != nil {
			http.Error(w, "server error", 500)
			return
		}
		writeJSON(w, u)
	})

	mux.HandleFunc("POST /api/logout", func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err == nil {
			DeleteSession(d, c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, Secure: secureCookie(r)})
	})

	mux.HandleFunc("GET /api/me", Require(d, false, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, From(r))
	}))

	mux.HandleFunc("POST /api/admin/invites", Require(d, true, func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 4)
		rand.Read(b)
		code := hex.EncodeToString(b)
		d.Exec(`INSERT INTO invite_codes (code, created_by) VALUES (?,?)`, code, From(r).ID)
		writeJSON(w, map[string]string{"code": code})
	}))

	mux.HandleFunc("GET /api/admin/invites", Require(d, true, func(w http.ResponseWriter, r *http.Request) {
		rows, _ := d.Query(`SELECT code, used_by IS NOT NULL FROM invite_codes ORDER BY rowid DESC`)
		defer rows.Close()
		type inv struct {
			Code string `json:"code"`
			Used bool   `json:"used"`
		}
		out := []inv{}
		for rows.Next() {
			var i inv
			rows.Scan(&i.Code, &i.Used)
			out = append(out, i)
		}
		writeJSON(w, out)
	}))
}
