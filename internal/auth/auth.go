package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"golang.org/x/crypto/argon2"
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

var ErrBadLogin = errors.New("invalid username or password")

func Hash(pw string) (hash, salt []byte) {
	salt = make([]byte, 16)
	rand.Read(salt)
	return argon2.IDKey([]byte(pw), salt, 1, 64*1024, 4, 32), salt
}

func Verify(pw string, hash, salt []byte) bool {
	h := argon2.IDKey([]byte(pw), salt, 1, 64*1024, 4, 32)
	return subtle.ConstantTimeCompare(h, hash) == 1
}

// Seed creates an admin user if the users table is empty.
func Seed(d *sql.DB, user, pass string) error {
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 || user == "" {
		return nil
	}
	h, s := Hash(pass)
	_, err := d.Exec(`INSERT INTO users (username, pass_hash, salt, role) VALUES (?,?,?, 'admin')`, user, h, s)
	return err
}

func Login(d *sql.DB, user, pass string) (User, error) {
	var u User
	var h, s []byte
	err := d.QueryRow(`SELECT id, username, role, pass_hash, salt FROM users WHERE username=?`, user).
		Scan(&u.ID, &u.Username, &u.Role, &h, &s)
	if err != nil || !Verify(pass, h, s) {
		return User{}, ErrBadLogin
	}
	return u, nil
}

func CreateSession(d *sql.DB, userID int64) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	tok := hex.EncodeToString(b)
	_, err := d.Exec(`INSERT INTO sessions (token, user_id, expires_at) VALUES (?,?,?)`,
		tok, userID, time.Now().Add(30*24*time.Hour).Unix())
	return tok, err
}

func UserByToken(d *sql.DB, tok string) (User, error) {
	var u User
	err := d.QueryRow(`SELECT u.id, u.username, u.role FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token=? AND s.expires_at > unixepoch()`, tok).Scan(&u.ID, &u.Username, &u.Role)
	return u, err
}

func DeleteSession(d *sql.DB, tok string) { d.Exec(`DELETE FROM sessions WHERE token=?`, tok) }

// Register consumes a single-use invite code. Role is always member.
func Register(d *sql.DB, code, user, pass string) (User, error) {
	res, err := d.Exec(`UPDATE invite_codes SET used_by=-1 WHERE code=? AND used_by IS NULL`, code)
	if err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, errors.New("invalid or used invite code")
	}
	h, s := Hash(pass)
	r, err := d.Exec(`INSERT INTO users (username, pass_hash, salt) VALUES (?,?,?)`, user, h, s)
	if err != nil {
		return User{}, errors.New("username taken")
	}
	id, _ := r.LastInsertId()
	d.Exec(`UPDATE invite_codes SET used_by=? WHERE code=?`, id, code)
	return User{ID: id, Username: user, Role: "member"}, nil
}
