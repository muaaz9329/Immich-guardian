package video

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/muaaz9329/Immich-guardian/internal/notify"
)

type Config struct {
	LibraryPath string // immich library folder e.g. /Volumes/L1/photos/library
	CRF         string // compression quality, 28 is default
	Preset      string // ffmpeg preset, medium is default
}

// processedDB is a simple JSON file tracking which videos have been compressed
// stored in LibraryPath/.guardian_processed
type processedDB struct {
	Processed map[string]string `json:"processed"` // filepath -> md5 of original
}

type Video struct {
	cfg      Config
	notifier *notify.Notifier
	dbPath   string
}

func New(cfg Config, notifier *notify.Notifier) *Video {
	return &Video{
		cfg:      cfg,
		notifier: notifier,
		dbPath:   filepath.Join(cfg.LibraryPath, ".guardian_processed.json"),
	}
}

func (v *Video) Start(ctx context.Context) {
	// Run every Sunday at 4am (after backup at 3am)
	for {

		// give it a startup run to catch any videos missed while guardian was down
		v.run(ctx)

		// schedule future runs
		next := nextOccurrence(time.Sunday, 4, 0)
		log.Printf("[video] next compression run scheduled at %s", next.Format(time.RFC1123))

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			v.run(ctx)
		}
	}
}

func (v *Video) run(ctx context.Context) {
	log.Println("[video] starting compression run...")

	db, err := v.loadDB()
	if err != nil {
		log.Printf("[video] failed to load processed db: %v", err)
		db = &processedDB{Processed: make(map[string]string)}
	}

	videos, err := v.findVideos()
	if err != nil {
		log.Printf("[video] failed to find videos: %v", err)
		return
	}

	if len(videos) == 0 {
		log.Println("[video] no new videos to compress")
		return
	}

	log.Printf("[video] found %d videos to process", len(videos))

	var (
		compressed int
		skipped    int
		failed     int
		savedBytes int64
	)

	for _, path := range videos {
		select {
		case <-ctx.Done():
			log.Println("[video] compression interrupted by shutdown")
			return
		default:
		}

		// Check if already processed
		hash, err := fileHash(path)
		if err != nil {
			log.Printf("[video] hash failed for %s: %v", path, err)
			failed++
			continue
		}

		if db.Processed[path] == hash {
			skipped++
			continue
		}

		// Skip files modified in last 10 minutes (might still be uploading)
		info, err := os.Stat(path)
		if err != nil || time.Since(info.ModTime()) < 10*time.Minute {
			log.Printf("[video] skipping recently modified file: %s", path)
			skipped++
			continue
		}

		originalSize := info.Size()
		saved, err := v.compressInPlace(path)
		if err != nil {
			log.Printf("[video] compression failed for %s: %v", path, err)
			failed++
			continue
		}

		// If compression didn't save at least 10%, not worth it — keep original
		if saved < originalSize/10 {
			log.Printf("[video] skipping replace — compression saved less than 10%% for %s", path)
			db.Processed[path] = hash
			skipped++
			continue
		}

		savedBytes += saved
		compressed++

		// Update processed db
		newHash, _ := fileHash(path)
		db.Processed[path] = newHash

		log.Printf("[video] compressed %s, saved %.1f MB", filepath.Base(path), float64(saved)/1024/1024)
	}

	v.saveDB(db)

	savedMB := float64(savedBytes) / 1024 / 1024
	msg := fmt.Sprintf(
		"Video compression complete.\n✅ Compressed: %d\n⏭ Skipped: %d\n❌ Failed: %d\n💾 Space saved: %.1f MB",
		compressed, skipped, failed, savedMB,
	)
	log.Printf("[video] %s", msg)

	if compressed > 0 || failed > 0 {
		v.notifier.SendLow("🎬 Video Compression Done", msg)
	}
}

// compressInPlace compresses a video file in place using ffmpeg.
// MP4 muxer requires seekable output so we cannot pipe to stdout.
// Instead ffmpeg writes directly to a .guardian_tmp file, then we
// atomically rename over the original.
// Write count: 1 write (tmp) + 1 rename — original untouched on failure.
func (v *Video) compressInPlace(path string) (savedBytes int64, err error) {
	originalInfo, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stat original: %w", err)
	}
	originalSize := originalInfo.Size()

	// Temp file in same directory for atomic rename
	tmpPath := path + ".guardian_tmp.mp4"

	// Key flags:
	// -nostdin             : don't read stdin
	// -loglevel error      : suppress ffmpeg noise
	// -c:v libx265         : H.265 — better quality per MB than H.264
	// -crf                 : quality factor (28 = good quality, smaller file)
	// -preset              : speed/compression tradeoff
	// -c:a copy            : copy audio stream as-is, no quality loss
	// -tag:v hvc1          : Apple compatibility tag for HEVC
	// -movflags +faststart : put metadata at front for fast streaming
	// -y                   : overwrite tmpPath if a previous run left one
	cmd := exec.Command(
		"nice", "-n", "19",
		"ffmpeg",
		"-nostdin",
		"-loglevel", "error",
		"-threads", "4",
		"-i", path,
		"-c:v", "libx265",
		"-crf", v.cfg.CRF,
		"-preset", v.cfg.Preset,
		"-c:a", "copy",
		"-tag:v", "hvc1",
		"-movflags", "+faststart",
		"-f", "mp4",
		"-y",
		tmpPath,
	)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("ffmpeg: %w\nstderr: %s", err, stderr.String())
	}

	compressedInfo, err := os.Stat(tmpPath)
	if err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("stat compressed: %w", err)
	}

	compressedSize := compressedInfo.Size()

	// If compressed is actually larger (can happen with already-compressed videos), bail
	if compressedSize >= originalSize {
		os.Remove(tmpPath)
		return 0, nil
	}

	// Atomic replace: rename is a single syscall, no window where file is missing
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("rename: %w", err)
	}

	return originalSize - compressedSize, nil
}

// findVideos returns all .mov and .mp4 files in the library
func (v *Video) findVideos() ([]string, error) {
	var videos []string

	err := filepath.Walk(v.cfg.LibraryPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable files
		}
		if info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".mov" || ext == ".mp4" || ext == ".m4v" {
			videos = append(videos, path)
		}
		return nil
	})

	return videos, err
}

// fileHash returns MD5 of first 1MB of file — fast enough for change detection
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.CopyN(h, f, 1024*1024); err != nil && err != io.EOF {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func (v *Video) loadDB() (*processedDB, error) {
	data, err := os.ReadFile(v.dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &processedDB{Processed: make(map[string]string)}, nil
		}
		return nil, err
	}

	var db processedDB
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, err
	}
	if db.Processed == nil {
		db.Processed = make(map[string]string)
	}
	return &db, nil
}

func (v *Video) saveDB(db *processedDB) {
	data, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		log.Printf("[video] failed to marshal db: %v", err)
		return
	}
	if err := os.WriteFile(v.dbPath, data, 0644); err != nil {
		log.Printf("[video] failed to save db: %v", err)
	}
}

func nextOccurrence(weekday time.Weekday, hour, min int) time.Time {
	now := time.Now()
	t := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())

	daysUntil := int(weekday - now.Weekday())
	if daysUntil < 0 {
		daysUntil += 7
	}
	if daysUntil == 0 && t.Before(now) {
		daysUntil = 7
	}

	return t.AddDate(0, 0, daysUntil)
}
