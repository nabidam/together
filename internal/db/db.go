package db

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

const schema = `
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
CREATE TABLE IF NOT EXISTS jobs (
  id INTEGER PRIMARY KEY,
  media_id INTEGER NOT NULL REFERENCES media(id),
  status TEXT NOT NULL DEFAULT 'pending',
  error TEXT,
  created_at INTEGER NOT NULL DEFAULT (unixepoch())
);
`

// cutover is the V2 boot cutover (ARCHITECTURE §3.1): V1's persistent
// rooms/messages/activities are gone in V2 (rooms live in hub memory now,
// SPEC §9.11/§9.2), and V1's movie|music kind vocabulary normalizes to
// V2's video|audio (SPEC §9.9). It runs after the idempotent schema, on
// every Open(); every statement here is itself idempotent (DROP ... IF
// EXISTS, and the UPDATEs match zero rows once already normalized), so
// re-running it on each boot is safe without a migration framework.
const cutover = `
DROP TABLE IF EXISTS rooms;
DROP TABLE IF EXISTS messages;
DROP TABLE IF EXISTS activities;
UPDATE media SET kind='video' WHERE kind='movie';
UPDATE media SET kind='audio' WHERE kind='music';
`

// Open opens (creating if needed) the SQLite database, applies the schema,
// and runs the V2 boot cutover.
func Open(path string) (*sql.DB, error) {
	d, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	if _, err := d.Exec(schema); err != nil {
		d.Close()
		return nil, err
	}
	if _, err := d.Exec(cutover); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}
