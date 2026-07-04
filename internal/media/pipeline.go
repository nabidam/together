package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Info struct {
	Container string
	VCodec    string
	ACodec    string
	Duration  float64
}

func Probe(path string) (Info, error) {
	out, err := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return Info{}, fmt.Errorf("ffprobe: %w", err)
	}
	var p struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return Info{}, err
	}
	info := Info{Container: p.Format.FormatName}
	info.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			if info.VCodec == "" {
				info.VCodec = s.CodecName
			}
		case "audio":
			if info.ACodec == "" {
				info.ACodec = s.CodecName
			}
		}
	}
	return info, nil
}

// Plan returns ffmpeg output args, or nil to copy the file unchanged.
func Plan(kind string, i Info) []string {
	if kind == "music" {
		// ponytail: exotic music formats (wma…) will fail in browser; transcode-once rule extends here if it ever happens
		return nil
	}
	goodAudio := i.ACodec == "aac" || i.ACodec == "mp3"
	switch {
	case i.VCodec == "h264" && goodAudio && strings.Contains(i.Container, "mp4"):
		return nil
	case i.VCodec == "h264" && goodAudio:
		return []string{"-c", "copy", "-movflags", "+faststart"}
	case i.VCodec == "h264":
		return []string{"-c:v", "copy", "-c:a", "aac", "-movflags", "+faststart"}
	default:
		return []string{"-c:v", "libx264", "-preset", "medium", "-crf", "22", "-c:a", "aac", "-movflags", "+faststart"}
	}
}

// Worker processes pending jobs one at a time. Run as a goroutine; respects ctx.
func Worker(ctx context.Context, d *sql.DB, dataDir string) {
	// ponytail: reclaim jobs orphaned by a crash; process() is idempotent (-y, re-probe)
	d.Exec(`UPDATE jobs SET status='pending' WHERE status='running'`)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			var jobID, mediaID int64
			err := d.QueryRow(`SELECT id, media_id FROM jobs WHERE status='pending' ORDER BY id LIMIT 1`).Scan(&jobID, &mediaID)
			if err != nil {
				continue
			}
			d.Exec(`UPDATE jobs SET status='running' WHERE id=?`, jobID)
			if err := process(d, dataDir, mediaID); err != nil {
				log.Printf("job %d media %d failed: %v", jobID, mediaID, err)
				d.Exec(`UPDATE jobs SET status='failed', error=? WHERE id=?`, err.Error(), jobID)
				d.Exec(`UPDATE media SET status='failed', error=? WHERE id=?`, err.Error(), mediaID)
			} else {
				d.Exec(`UPDATE jobs SET status='done' WHERE id=?`, jobID)
			}
		}
	}
}

func run(name string, args ...string) error {
	cmd := exec.Command("nice", append([]string{"-n", "19", name}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		tail := string(out)
		if len(tail) > 800 {
			tail = tail[len(tail)-800:]
		}
		return fmt.Errorf("%s failed: %w\n%s", name, err, tail)
	}
	return nil
}

func process(d *sql.DB, dataDir string, mediaID int64) error {
	var kind string
	var origName sql.NullString
	if err := d.QueryRow(`SELECT kind, orig_name FROM media WHERE id=?`, mediaID).Scan(&kind, &origName); err != nil {
		return err
	}
	src := UploadPath(dataDir, mediaID)
	info, err := Probe(src)
	if err != nil {
		return err
	}

	ext := ".mp4"
	if kind == "music" {
		// ponytail: upload endpoint guarantees music orig_name has an extension; .mp4 fallback only reachable for movies
		if e := filepath.Ext(origName.String); e != "" {
			ext = e
		}
	}
	dst := filepath.Join(dataDir, "media", strconv.FormatInt(mediaID, 10)+ext)

	if args := Plan(kind, info); args == nil {
		if err := os.Rename(src, dst); err != nil {
			return err
		}
	} else {
		full := append([]string{"-y", "-i", src}, append(args, dst)...)
		if err := run("ffmpeg", full...); err != nil {
			return err
		}
		os.Remove(src)
	}

	// uploaded subtitle sidecars: <upload>.sub.N.<label>.srt → vtt
	subs, _ := SubPaths(dataDir, mediaID)
	for n, sp := range subs {
		label := "Subtitles"
		if parts := strings.Split(filepath.Base(sp), "."); len(parts) >= 4 {
			label = parts[len(parts)-2]
		}
		vtt := filepath.Join(dataDir, "media", fmt.Sprintf("%d.sub.%d.vtt", mediaID, n))
		if err := run("ffmpeg", "-y", "-i", sp, vtt); err != nil {
			log.Printf("subtitle %s skipped: %v", sp, err) // bad sub must not fail the movie
			continue
		}
		d.Exec(`INSERT INTO subtitles (media_id, label, file_path) VALUES (?,?,?)`, mediaID, label, vtt)
		os.Remove(sp)
	}
	// ponytail: embedded mkv subtitle extraction deferred — sidecar .srt covers the stated V1 flow; add -map 0:s:N loop when an mkv with embedded subs actually shows up

	fi, err := os.Stat(dst)
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE media SET status='ready', file_path=?, size_bytes=?, duration=? WHERE id=?`,
		dst, fi.Size(), info.Duration, mediaID)
	return err
}
