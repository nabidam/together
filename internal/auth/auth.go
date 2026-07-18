package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
}

var ErrBadLogin = errors.New("invalid username or password")

// ErrSeedCredentials deliberately contains no supplied credential values.
var ErrSeedCredentials = errors.New("strong administrator credentials required")

// argon2id uses 64 MB per invocation; unbounded concurrent logins could OOM the
// 2 GB box (systemd MemoryMax=1200M). Cap concurrent hashes at 2.
var hashSem = make(chan struct{}, 2)

// dummyHash makes unknown-user login attempts perform the same one Argon2id
// verification as a wrong password for an existing account.
var dummyHash, dummySalt = Hash("invalid password")

var verifyPassword = Verify

func idKey(pw string, salt []byte) []byte {
	hashSem <- struct{}{}
	defer func() { <-hashSem }()
	return argon2.IDKey([]byte(pw), salt, 1, 64*1024, 4, 32)
}

func Hash(pw string) (hash, salt []byte) {
	salt = make([]byte, 16)
	rand.Read(salt)
	return idKey(pw, salt), salt
}

func Verify(pw string, hash, salt []byte) bool {
	return subtle.ConstantTimeCompare(idKey(pw, salt), hash) == 1
}

// GC drops expired sessions. Called once at boot; the table stays tiny at ≤10 users.
func GC(d *sql.DB) { d.Exec(`DELETE FROM sessions WHERE expires_at <= unixepoch()`) }

// Seed creates an admin user if the users table is empty. First boot requires
// a username and a password of at least 12 Unicode code points.
func Seed(d *sql.DB, user, pass string) error {
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM users`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	if user == "" || utf8.RuneCountInString(pass) < 12 {
		return ErrSeedCredentials
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
	if err != nil {
		verifyPassword(pass, dummyHash, dummySalt)
		return User{}, ErrBadLogin
	}
	if !verifyPassword(pass, h, s) {
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
// The claim and insert happen in one transaction so a failed registration
// (e.g. username taken) rolls back and leaves the invite code unused.
func Register(d *sql.DB, code, user, pass string) (User, error) {
	tx, err := d.Begin()
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()

	res, err := tx.Exec(`UPDATE invite_codes SET used_by=-1 WHERE code=? AND used_by IS NULL`, code)
	if err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, errors.New("invalid or used invite code")
	}
	h, s := Hash(pass)
	r, err := tx.Exec(`INSERT INTO users (username, pass_hash, salt) VALUES (?,?,?)`, user, h, s)
	if err != nil {
		return User{}, errors.New("username taken")
	}
	id, _ := r.LastInsertId()
	if _, err := tx.Exec(`UPDATE invite_codes SET used_by=? WHERE code=?`, id, code); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return User{ID: id, Username: user, Role: "member"}, nil
}
