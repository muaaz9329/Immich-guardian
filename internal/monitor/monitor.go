package monitor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/muaaz9329/Immich-guardian/internal/notify"
)

type Config struct {
	SSDMount          string
	PhotosPath        string
	PostgresContainer string
	ImmichContainer   string
}

type Monitor struct {
	cfg      Config
	notifier *notify.Notifier

	// track last known states to avoid spamming notifications
	lastSSDState      bool
	lastImmichState   bool
	lastPostgresState bool
}

func New(cfg Config, notifier *notify.Notifier) *Monitor {
	return &Monitor{
		cfg:               cfg,
		notifier:          notifier,
		lastSSDState:      true,
		lastImmichState:   true,
		lastPostgresState: true,
	}
}

func (m *Monitor) Start(ctx context.Context) {
	// Check every 2 minutes
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	// Run immediately on start
	m.runChecks()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.runChecks()
		}
	}
}

func (m *Monitor) runChecks() {
	m.checkSSD()
	m.checkContainer(m.cfg.ImmichContainer, &m.lastImmichState)
	m.checkContainer(m.cfg.PostgresContainer, &m.lastPostgresState)
	m.checkDiskSpace()
}

// checkSSD detects if the SSD has been unmounted/unplugged
func (m *Monitor) checkSSD() {
	_, err := os.Stat(m.cfg.SSDMount)
	ssdOK := err == nil

	if !ssdOK && m.lastSSDState {
		// SSD just went away
		log.Printf("[monitor] EMERGENCY: SSD unmounted at %s", m.cfg.SSDMount)
		m.notifier.SendEmergency(
			"🚨 SSD Unplugged!",
			fmt.Sprintf("Your photo SSD at %s is no longer accessible. Photos may be at risk. Check the drive immediately.", m.cfg.SSDMount),
		)
	} else if ssdOK && !m.lastSSDState {
		// SSD came back
		log.Printf("[monitor] SSD remounted at %s", m.cfg.SSDMount)
		m.notifier.SendInfo(
			"✅ SSD Reconnected",
			fmt.Sprintf("SSD at %s is accessible again.", m.cfg.SSDMount),
		)
	}

	m.lastSSDState = ssdOK
}

// checkContainer verifies a Docker container is running
func (m *Monitor) checkContainer(name string, lastState *bool) {
	running := isContainerRunning(name)

	if !running && *lastState {
		log.Printf("[monitor] EMERGENCY: container %s is down", name)
		m.notifier.SendEmergency(
			fmt.Sprintf("🚨 %s Crashed!", name),
			fmt.Sprintf("Docker container '%s' is no longer running. Immich may be unavailable. SSH in and check: docker ps", name),
		)
	} else if running && !*lastState {
		log.Printf("[monitor] container %s recovered", name)
		m.notifier.SendInfo(
			fmt.Sprintf("✅ %s Recovered", name),
			fmt.Sprintf("Container '%s' is running again.", name),
		)
	}

	*lastState = running
}

// checkDiskSpace warns if SSD is getting full
func (m *Monitor) checkDiskSpace() {
	if !m.lastSSDState {
		return // SSD not mounted, skip
	}

	out, err := exec.Command("df", "-h", m.cfg.PhotosPath).Output()
	if err != nil {
		log.Printf("[monitor] disk space check failed: %v", err)
		return
	}

	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return
	}

	// Parse usage percentage from df output
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return
	}

	usagePct := strings.TrimSuffix(fields[4], "%")
	var pct int
	fmt.Sscanf(usagePct, "%d", &pct)

	if pct >= 90 {
		m.notifier.SendEmergency(
			"🚨 SSD Almost Full!",
			fmt.Sprintf("Your photo SSD is %d%% full (%s used of %s). Free up space soon to avoid data loss.", pct, fields[2], fields[1]),
		)
	} else if pct >= 75 {
		m.notifier.SendInfo(
			"⚠️ SSD Getting Full",
			fmt.Sprintf("Your photo SSD is %d%% full. Consider cleaning up or expanding storage.", pct),
		)
	}
}

func isContainerRunning(name string) bool {
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", name).Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}
