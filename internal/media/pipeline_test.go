package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"together/internal/db"
)

func TestPlanDecisions(t *testing.T) {
	cases := []struct {
		info Info
		want string // "asis" | "remux" | "audio" | "full"
	}{
		{Info{"mov,mp4,m4a,3gp,3g2,mj2", "h264", "aac", 10}, "asis"},
		{Info{"matroska,webm", "h264", "aac", 10}, "remux"},
		{Info{"matroska,webm", "h264", "ac3", 10}, "audio"},
		{Info{"matroska,webm", "hevc", "aac", 10}, "full"},
	}
	for _, c := range cases {
		got := classify(Plan("movie", c.info))
		if got != c.want {
			t.Errorf("%+v: want %s got %s", c.info, c.want, got)
		}
	}
	if Plan("music", Info{"flac", "", "flac", 10}) != nil {
		t.Error("music must copy as-is")
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
	case contains(s, "libx264"):
		return "full"
	case contains(s, "-c:v copy") && contains(s, "-c:a aac"):
		return "audio"
	case contains(s, "-c copy"):
		return "remux"
	}
	return "?"
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
	res, _ := d.Exec(`INSERT INTO media (kind, title, status) VALUES ('movie','Fixture','processing')`)
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
			d.QueryRow(`SELECT file_path, duration FROM media WHERE id=?`, id).Scan(&fp, &dur)
			if filepath.Ext(fp) != ".mp4" || dur < 1.5 {
				t.Fatalf("fp=%s dur=%v", fp, dur)
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

	res, _ := d.Exec(`INSERT INTO media (kind, title, status) VALUES ('movie','Fixture','processing')`)
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
