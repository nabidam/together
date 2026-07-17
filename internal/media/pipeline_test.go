package media

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"together/internal/auth"
	"together/internal/db"
)

func TestPlanDecisions(t *testing.T) {
	cases := []struct {
		info Info
		want string // "asis" | "remux" | "audio" | "audio_only" | "full"
	}{
		{Info{"mov,mp4,m4a,3gp,3g2,mj2", "h264", "aac", 10}, "asis"},
		{Info{"matroska,webm", "h264", "aac", 10}, "remux"},
		{Info{"matroska,webm", "h264", "ac3", 10}, "audio"},
		{Info{"matroska,webm", "hevc", "aac", 10}, "full"},
		{Info{"mp3", "", "mp3", 10}, "asis"},
		{Info{"mov,mp4,m4a,3gp,3g2,mj2", "", "aac", 10}, "asis"},
		{Info{"ogg", "", "opus", 10}, "audio_only"},
	}
	for _, c := range cases {
		got := classify(Plan(c.info))
		if got != c.want {
			t.Errorf("%+v: want %s got %s", c.info, c.want, got)
		}
	}
	if args := strings.Join(Plan(Info{"ogg", "", "opus", 10}), " "); strings.Contains(args, "libx264") {
		t.Fatalf("audio-only plan must never use libx264: %s", args)
	}
}

func classify(args []string) string {
	if args == nil {
		return "asis"
	}
	s := ""
	for _, a := range args {
		s += a + " "
	}
	switch {
	case contains(s, "-vn") && contains(s, "-c:a aac"):
		return "audio_only"
	case contains(s, "libx264"):
		return "full"
	case contains(s, "-c:v copy") && contains(s, "-c:a aac"):
		return "audio"
	case contains(s, "-c copy"):
		return "remux"
	}
	return "?"
}

func TestProcess_AudioFixtures(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}

	t.Run("mp3 moved unchanged", func(t *testing.T) {
		dir := t.TempDir()
		d, err := db.Open(filepath.Join(dir, "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		res, err := d.Exec(`INSERT INTO media (kind, title, status, orig_name) VALUES ('video','MP3','processing','song.mp3')`)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		os.MkdirAll(filepath.Join(dir, "uploads"), 0o755)
		os.MkdirAll(filepath.Join(dir, "media"), 0o755)
		src := UploadPath(dir, id)
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-c:a", "libmp3lame", "-f", "mp3", src)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v\n%s", err, out)
		}
		before, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}

		if err := process(d, dir, id); err != nil {
			t.Fatal(err)
		}
		var kind, status, output string
		if err := d.QueryRow(`SELECT kind, status, file_path FROM media WHERE id=?`, id).Scan(&kind, &status, &output); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if kind != "audio" || status != "ready" || filepath.Ext(output) != ".mp3" {
			t.Fatalf("kind=%s status=%s output=%s", kind, status, output)
		}
		if !bytes.Equal(before, after) {
			t.Fatal("mp3 bytes changed; expected a rename without transcoding")
		}
	})

	t.Run("mp3 with attached artwork remains audio", func(t *testing.T) {
		dir := t.TempDir()
		d, err := db.Open(filepath.Join(dir, "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		res, err := d.Exec(`INSERT INTO media (kind, title, status, orig_name) VALUES ('video','Artwork MP3','processing','artwork.mp3')`)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		os.MkdirAll(filepath.Join(dir, "uploads"), 0o755)
		os.MkdirAll(filepath.Join(dir, "media"), 0o755)
		src := UploadPath(dir, id)
		cmd := exec.Command("ffmpeg", "-y",
			"-f", "lavfi", "-i", "sine=frequency=550:duration=1",
			"-f", "lavfi", "-i", "color=c=red:s=32x32",
			"-map", "0:a", "-map", "1:v", "-c:a", "libmp3lame", "-c:v", "mjpeg",
			"-disposition:v:0", "attached_pic", "-shortest", "-f", "mp3", src)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v\n%s", err, out)
		}

		info, err := Probe(src)
		if err != nil {
			t.Fatal(err)
		}
		if info.VCodec != "" || info.ACodec != "mp3" {
			t.Fatalf("attached artwork must not classify as video: %+v", info)
		}
		if err := process(d, dir, id); err != nil {
			t.Fatal(err)
		}
		var kind, status, output string
		if err := d.QueryRow(`SELECT kind, status, file_path FROM media WHERE id=?`, id).Scan(&kind, &status, &output); err != nil {
			t.Fatal(err)
		}
		if kind != "audio" || status != "ready" || filepath.Ext(output) != ".mp3" {
			t.Fatalf("kind=%s status=%s output=%s", kind, status, output)
		}
	})

	t.Run("opus transcoded to aac m4a", func(t *testing.T) {
		dir := t.TempDir()
		d, err := db.Open(filepath.Join(dir, "t.db"))
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		res, err := d.Exec(`INSERT INTO media (kind, title, status, orig_name) VALUES ('video','Opus','processing','song.ogg')`)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		os.MkdirAll(filepath.Join(dir, "uploads"), 0o755)
		os.MkdirAll(filepath.Join(dir, "media"), 0o755)
		src := UploadPath(dir, id)
		cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=660:duration=1", "-c:a", "libopus", "-f", "ogg", src)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v\n%s", err, out)
		}

		if err := process(d, dir, id); err != nil {
			t.Fatal(err)
		}
		var kind, status, output string
		if err := d.QueryRow(`SELECT kind, status, file_path FROM media WHERE id=?`, id).Scan(&kind, &status, &output); err != nil {
			t.Fatal(err)
		}
		info, err := Probe(output)
		if err != nil {
			t.Fatal(err)
		}
		if kind != "audio" || status != "ready" || filepath.Ext(output) != ".m4a" || info.VCodec != "" || info.ACodec != "aac" {
			t.Fatalf("kind=%s status=%s output=%s probe=%+v", kind, status, output, info)
		}
		if got := listedMediaKind(t, d, dir); got != "audio" {
			t.Fatalf("GET /api/media kind=%q, want audio", got)
		}
	})
}

func listedMediaKind(t *testing.T, d *sql.DB, dataDir string) string {
	t.Helper()
	if err := auth.Seed(d, "admin", "password"); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	auth.Routes(mux, d)
	accountGate := func(next http.HandlerFunc) http.HandlerFunc { return auth.Require(d, false, next) }
	ServeRoutes(mux, d, dataDir, accountGate)

	login := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"password"}`))
	login.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	mux.ServeHTTP(loginResponse, login)
	cookies := loginResponse.Result().Cookies()
	if loginResponse.Code != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("login status=%d cookies=%d", loginResponse.Code, len(cookies))
	}

	request := httptest.NewRequest(http.MethodGet, "/api/media", nil)
	request.AddCookie(cookies[0])
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET /api/media status=%d body=%s", response.Code, response.Body.String())
	}
	var items []struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(response.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("GET /api/media returned %d items, want 1", len(items))
	}
	return items[0].Kind
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func TestProbeAndWorkerEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()

	// synth a 2s h264/aac mkv → must remux to mp4
	res, _ := d.Exec(`INSERT INTO media (kind, title, status) VALUES ('video','Fixture','processing')`)
	id, _ := res.LastInsertId()
	os.MkdirAll(filepath.Join(dir, "uploads"), 0o755)
	os.MkdirAll(filepath.Join(dir, "media"), 0o755)
	out := UploadPath(dir, id)
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=2:size=128x72:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-f", "matroska", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v\n%s", err, b)
	}
	d.Exec(`INSERT INTO jobs (media_id) VALUES (?)`, id)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Worker(ctx, d, dir)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		d.QueryRow(`SELECT status FROM media WHERE id=?`, id).Scan(&status)
		if status == "ready" {
			var fp string
			var dur float64
			var kind string
			d.QueryRow(`SELECT kind, file_path, duration FROM media WHERE id=?`, id).Scan(&kind, &fp, &dur)
			if kind != "video" || filepath.Ext(fp) != ".mp4" || dur < 1.5 {
				t.Fatalf("kind=%s fp=%s dur=%v", kind, fp, dur)
			}
			return
		}
		if status == "failed" {
			var e string
			d.QueryRow(`SELECT error FROM media WHERE id=?`, id).Scan(&e)
			t.Fatal("job failed:", e)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("worker never finished")
}

// TestProcessResumesAfterMoveCrash covers the narrow crash window where the
// source blob was already moved/removed but the media row never flipped to
// 'ready': a rerun must finish from dst instead of failing on the missing src.
func TestProcessResumesAfterMoveCrash(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()

	res, _ := d.Exec(`INSERT INTO media (kind, title, status) VALUES ('video','Fixture','processing')`)
	id, _ := res.LastInsertId()
	os.MkdirAll(filepath.Join(dir, "media"), 0o755)
	// dst already exists, src does not — as left by a crash right after the move
	dst := filepath.Join(dir, "media", "1.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=2:size=128x72:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", dst)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v\n%s", err, b)
	}

	if err := process(d, dir, id); err != nil {
		t.Fatal("resume failed:", err)
	}
	var status string
	var dur float64
	d.QueryRow(`SELECT status, duration FROM media WHERE id=?`, id).Scan(&status, &dur)
	if status != "ready" || dur < 1.5 {
		t.Fatalf("status=%s dur=%v", status, dur)
	}
}

// TestWorkerReclaimsOrphanedRunningJob covers the crash/restart case: a job
// left status='running' by a killed process (Worker only polls 'pending')
// must be picked back up on the next Worker start, not stranded forever.
func TestWorkerReclaimsOrphanedRunningJob(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	d, _ := db.Open(filepath.Join(dir, "t.db"))
	defer d.Close()

	res, _ := d.Exec(`INSERT INTO media (kind, title, status) VALUES ('video','Fixture','processing')`)
	id, _ := res.LastInsertId()
	os.MkdirAll(filepath.Join(dir, "uploads"), 0o755)
	os.MkdirAll(filepath.Join(dir, "media"), 0o755)
	out := UploadPath(dir, id)
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=2:size=128x72:rate=10",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-f", "matroska", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v\n%s", err, b)
	}
	// simulate a job orphaned by a crashed worker: status='running', not 'pending'
	d.Exec(`INSERT INTO jobs (media_id, status) VALUES (?, 'running')`, id)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Worker(ctx, d, dir)

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		var status string
		d.QueryRow(`SELECT status FROM media WHERE id=?`, id).Scan(&status)
		if status == "ready" {
			return
		}
		if status == "failed" {
			var e string
			d.QueryRow(`SELECT error FROM media WHERE id=?`, id).Scan(&e)
			t.Fatal("job failed:", e)
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("orphaned running job was never reclaimed")
}
