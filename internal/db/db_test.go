package db

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// v1Schema is a frozen snapshot of the shipped V1 DDL (rooms/messages/
// activities tables, media.kind still 'movie'/'music'), used only to seed a
// pre-cutover database file for the cutover tests below. It intentionally
// duplicates db.go's old schema rather than importing it, since the V2
// schema in this package no longer defines these tables.
const v1Schema = `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE,
  pass_hash BLOB NOT NULL,
  salt BLOB NOT NULL,
  role TEXT NOT NULL DEFAULT 'member',
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE IF NOT EXISTS sessions (
  token TEXT PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id),
  expires_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS invite_codes (
  code TEXT PRIMARY KEY,
  created_by INTEGER NOT NULL,
  used_by INTEGER
);
CREATE TABLE IF NOT EXISTS media (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'uploading',
  file_path TEXT,
  orig_name TEXT,
  size_bytes INTEGER,
  duration REAL,
  error TEXT,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE IF NOT EXISTS subtitles (
  id INTEGER PRIMARY KEY,
  media_id INTEGER NOT NULL REFERENCES media(id),
  label TEXT NOT NULL,
  file_path TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS rooms (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  owner_id INTEGER NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE IF NOT EXISTS messages (
  id INTEGER PRIMARY KEY,
  room_id INTEGER NOT NULL,
  user_id INTEGER NOT NULL,
  body TEXT NOT NULL,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE TABLE IF NOT EXISTS activities (
  id INTEGER PRIMARY KEY,
  room_id INTEGER NOT NULL,
  type TEXT NOT NULL,
  state_json TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'active',
  started_at INTEGER NOT NULL DEFAULT (unixepoch()),
  ended_at INTEGER
);
CREATE TABLE IF NOT EXISTS jobs (
  id INTEGER PRIMARY KEY,
  media_id INTEGER NOT NULL REFERENCES media(id),
  status TEXT NOT NULL DEFAULT 'pending',
  error TEXT,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
CREATE INDEX IF NOT EXISTS idx_messages_room ON messages(room_id, id);
`

// openV1Seed opens path with modernc.org/sqlite directly (bypassing Open, so
// the V2 schema/cutover doesn't run yet), applies the V1 schema, and seeds
// one row per table -- including a 'movie' and a 'music' media row -- so the
// cutover tests can assert both structural changes (tables dropped, kind
// normalized) and that unrelated rows/tables survive untouched.
func openV1Seed(t *testing.T, path string) {
	t.Helper()
	raw, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Exec(v1Schema); err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`INSERT INTO users (id, username, pass_hash, salt, role) VALUES (1,'admin',x'01',x'02','admin')`,
		`INSERT INTO sessions (token, user_id, expires_at) VALUES ('sesstok1',1,9999999999)`,
		`INSERT INTO invite_codes (code, created_by, used_by) VALUES ('invcode1',1,NULL)`,
		`INSERT INTO media (id, kind, title, status, file_path, size_bytes) VALUES (1,'movie','A Movie','ready','/data/1.mp4',100)`,
		`INSERT INTO media (id, kind, title, status, file_path, size_bytes) VALUES (2,'music','A Song','ready','/data/2.m4a',50)`,
		`INSERT INTO subtitles (id, media_id, label, file_path) VALUES (1,1,'English','/data/1.vtt')`,
		`INSERT INTO jobs (id, media_id, status) VALUES (1,1,'done')`,
		`INSERT INTO rooms (id, name, owner_id) VALUES (1,'Movie Night',1)`,
		`INSERT INTO messages (id, room_id, user_id, body) VALUES (1,1,1,'hi')`,
		`INSERT INTO activities (id, room_id, type, state_json) VALUES (1,1,'watch','{}')`,
	}
	for _, s := range stmts {
		if _, err := raw.Exec(s); err != nil {
			t.Fatalf("seed %q: %v", s, err)
		}
	}
}

func tableExists(t *testing.T, d *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestOpen_FreshDatabaseHasV2TablesOnly(t *testing.T) {
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, table := range []string{"users", "sessions", "invite_codes", "media", "subtitles", "jobs"} {
		if !tableExists(t, d, table) {
			t.Errorf("table %s missing", table)
		}
	}
	for _, table := range []string{"rooms", "messages", "activities"} {
		if tableExists(t, d, table) {
			t.Errorf("V1 table %s should not exist post-cutover", table)
		}
	}
}

func TestOpen_CutoverDropsV1TablesAndNormalizesKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	openV1Seed(t, path)

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	for _, table := range []string{"rooms", "messages", "activities"} {
		if tableExists(t, d, table) {
			t.Errorf("V1 table %s survived cutover", table)
		}
	}

	rows, err := d.Query(`SELECT id, kind FROM media ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	want := map[int64]string{1: "video", 2: "audio"}
	got := map[int64]string{}
	for rows.Next() {
		var id int64
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			t.Fatal(err)
		}
		got[id] = kind
		if kind != "video" && kind != "audio" {
			t.Errorf("media id=%d has non-normalized kind %q", id, kind)
		}
	}
	if got[1] != want[1] || got[2] != want[2] {
		t.Errorf("kind normalization: got %+v want %+v", got, want)
	}
}

func TestOpen_CutoverPreservesSurvivingTableRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	openV1Seed(t, path)

	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var username string
	if err := d.QueryRow(`SELECT username FROM users WHERE id=1`).Scan(&username); err != nil || username != "admin" {
		t.Errorf("users row lost: username=%q err=%v", username, err)
	}
	var sessUser int64
	if err := d.QueryRow(`SELECT user_id FROM sessions WHERE token='sesstok1'`).Scan(&sessUser); err != nil || sessUser != 1 {
		t.Errorf("sessions row lost: user_id=%d err=%v", sessUser, err)
	}
	var invUsedBy sql.NullInt64
	if err := d.QueryRow(`SELECT used_by FROM invite_codes WHERE code='invcode1'`).Scan(&invUsedBy); err != nil {
		t.Errorf("invite_codes row lost: %v", err)
	}
	var mediaTitle string
	if err := d.QueryRow(`SELECT title FROM media WHERE id=1`).Scan(&mediaTitle); err != nil || mediaTitle != "A Movie" {
		t.Errorf("media row content lost: title=%q err=%v", mediaTitle, err)
	}
	var subLabel string
	if err := d.QueryRow(`SELECT label FROM subtitles WHERE id=1`).Scan(&subLabel); err != nil || subLabel != "English" {
		t.Errorf("subtitles row lost: label=%q err=%v", subLabel, err)
	}
	var jobStatus string
	if err := d.QueryRow(`SELECT status FROM jobs WHERE id=1`).Scan(&jobStatus); err != nil || jobStatus != "done" {
		t.Errorf("jobs row lost: status=%q err=%v", jobStatus, err)
	}
}

func TestOpen_CutoverIdempotentAcrossTwoOpens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.db")
	openV1Seed(t, path)

	d1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	d1.Close()

	d2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open (idempotence): %v", err)
	}
	defer d2.Close()

	for _, table := range []string{"rooms", "messages", "activities"} {
		if tableExists(t, d2, table) {
			t.Errorf("V1 table %s reappeared after second Open", table)
		}
	}
	rows, err := d2.Query(`SELECT kind FROM media`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			t.Fatal(err)
		}
		if kind != "video" && kind != "audio" {
			t.Errorf("kind %q not normalized after second Open", kind)
		}
	}
}
