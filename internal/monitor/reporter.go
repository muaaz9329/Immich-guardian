package monitor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// StartReporter sends weekly and monthly summary reports
func (m *Monitor) StartReporter(ctx context.Context) {
	weeklyTicker := time.NewTicker(7 * 24 * time.Hour)
	// Approximate monthly with 30 days
	monthlyTicker := time.NewTicker(30 * 24 * time.Hour)
	defer weeklyTicker.Stop()
	defer monthlyTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-weeklyTicker.C:
			m.sendWeeklyReport()
		case <-monthlyTicker.C:
			m.sendMonthlyReport()
		}
	}
}

func (m *Monitor) sendWeeklyReport() {
	log.Println("[monitor] sending weekly report...")

	ssdUsage := getDiskUsage(m.cfg.PhotosPath)
	photoCount := countFiles(m.cfg.PhotosPath+"/library", []string{".jpg", ".jpeg", ".heic", ".png"})
	videoCount := countFiles(m.cfg.PhotosPath+"/library", []string{".mov", ".mp4", ".m4v"})
	immichUp := isContainerRunning(m.cfg.ImmichContainer)
	postgresUp := isContainerRunning(m.cfg.PostgresContainer)

	status := "✅ All systems running"
	if !immichUp || !postgresUp {
		status = "⚠️ Some services are down!"
	}

	report := fmt.Sprintf(
		"%s\n\n📁 Photos: %d files\n🎬 Videos: %d files\n💾 SSD Usage: %s\n🐘 Postgres: %s\n🖼 Immich: %s",
		status,
		photoCount,
		videoCount,
		ssdUsage,
		containerStatus(postgresUp),
		containerStatus(immichUp),
	)

	m.notifier.SendLow("📊 Weekly Immich Report", report)
}

func (m *Monitor) sendMonthlyReport() {
	log.Println("[monitor] sending monthly report...")

	ssdUsage := getDiskUsage(m.cfg.PhotosPath)
	totalFiles := countFiles(m.cfg.PhotosPath+"/library", []string{".jpg", ".jpeg", ".heic", ".png", ".mov", ".mp4"})
	backupCount := countFiles(filepath.Dir(m.cfg.PhotosPath)+"/db-backup", []string{".sql"})

	report := fmt.Sprintf(
		"Monthly summary for %s\n\n📸 Total media files: %d\n💾 Total SSD usage: %s\n🗄 DB backups on disk: %d\n\nAll your memories are safe! 🎉",
		time.Now().Format("January 2006"),
		totalFiles,
		ssdUsage,
		backupCount,
	)

	m.notifier.SendLow("📅 Monthly Immich Report", report)
}

func getDiskUsage(path string) string {
	if _, err := os.Stat(path); err != nil {
		return "SSD not mounted"
	}

	out, err := exec.Command("df", "-h", path).Output()
	if err != nil {
		return "unknown"
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return "unknown"
	}

	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return "unknown"
	}

	return fmt.Sprintf("%s used of %s (%s)", fields[2], fields[1], fields[4])
}

func countFiles(dir string, extensions []string) int {
	if _, err := os.Stat(dir); err != nil {
		return 0
	}

	extSet := make(map[string]bool)
	for _, e := range extensions {
		extSet[strings.ToLower(e)] = true
	}

	count := 0
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if extSet[strings.ToLower(filepath.Ext(path))] {
			count++
		}
		return nil
	})
	return count
}

func containerStatus(running bool) string {
	if running {
		return "✅ Running"
	}
	return "❌ Down"
}
