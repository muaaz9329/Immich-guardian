package backup

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/muaaz9329/Immich-guardian/internal/notify"
)

type Config struct {
	BackupPath        string
	PostgresContainer string
	DBUser            string
	KeepWeeks         int // how many weekly backups to retain
}

type Backup struct {
	cfg      Config
	notifier *notify.Notifier
}

func New(cfg Config, notifier *notify.Notifier) *Backup {
	return &Backup{cfg: cfg, notifier: notifier}
}

func (b *Backup) Start(ctx context.Context) {
	// Run weekly on Sunday at 3am
	for {
		next := nextOccurrence(time.Sunday, 3, 0)
		log.Printf("[backup] next backup scheduled at %s", next.Format(time.RFC1123))

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			b.run()
		}
	}
}

func (b *Backup) run() {
	log.Println("[backup] starting weekly pg_dump...")

	if err := os.MkdirAll(b.cfg.BackupPath, 0755); err != nil {
		log.Printf("[backup] failed to create backup dir: %v", err)
		b.notifier.SendEmergency("🚨 Backup Failed", fmt.Sprintf("Could not create backup directory: %v", err))
		return
	}

	filename := fmt.Sprintf("dump_%s.sql", time.Now().Format("2006-01-02"))
	outPath := filepath.Join(b.cfg.BackupPath, filename)

	outFile, err := os.Create(outPath)
	if err != nil {
		log.Printf("[backup] failed to create dump file: %v", err)
		b.notifier.SendEmergency("🚨 Backup Failed", fmt.Sprintf("Could not create dump file: %v", err))
		return
	}
	defer outFile.Close()

	cmd := exec.Command(
		"docker", "exec", b.cfg.PostgresContainer,
		"pg_dumpall", "-c", "-U", b.cfg.DBUser,
	)
	cmd.Stdout = outFile

	var stderr strings.Builder
	cmd.Stderr = &stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		log.Printf("[backup] pg_dump failed: %v\nstderr: %s", err, stderr.String())
		os.Remove(outPath) // clean up partial file
		b.notifier.SendEmergency(
			"🚨 Database Backup Failed",
			fmt.Sprintf("pg_dump error: %v\n%s", err, stderr.String()),
		)
		return
	}

	info, _ := os.Stat(outPath)
	sizeMB := float64(info.Size()) / 1024 / 1024
	duration := time.Since(start).Round(time.Second)

	log.Printf("[backup] completed in %s, size: %.1fMB", duration, sizeMB)

	// Rotate old backups
	b.rotate()

	b.notifier.SendLow(
		"✅ Weekly Backup Complete",
		fmt.Sprintf("Database backed up successfully.\nFile: %s\nSize: %.1f MB\nDuration: %s", filename, sizeMB, duration),
	)
}

// rotate removes old backups, keeping only the last N weeks
func (b *Backup) rotate() {
	entries, err := os.ReadDir(b.cfg.BackupPath)
	if err != nil {
		log.Printf("[backup] rotate: failed to read dir: %v", err)
		return
	}

	var dumps []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "dump_") && strings.HasSuffix(e.Name(), ".sql") {
			dumps = append(dumps, filepath.Join(b.cfg.BackupPath, e.Name()))
		}
	}

	// Sort oldest first
	sort.Strings(dumps)

	// Delete oldest beyond KeepWeeks
	for len(dumps) > b.cfg.KeepWeeks {
		oldest := dumps[0]
		dumps = dumps[1:]
		if err := os.Remove(oldest); err != nil {
			log.Printf("[backup] rotate: failed to remove %s: %v", oldest, err)
		} else {
			log.Printf("[backup] rotate: removed old backup %s", oldest)
		}
	}
}

// nextOccurrence returns the next time the given weekday + hour:min occurs
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
