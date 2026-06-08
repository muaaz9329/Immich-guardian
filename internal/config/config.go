package config

import (
	"log"
	"os"

	"github.com/muaaz9329/Immich-guardian/internal/backup"
	"github.com/muaaz9329/Immich-guardian/internal/monitor"
	"github.com/muaaz9329/Immich-guardian/internal/video"
)

func LoadConfig() (monitor.Config, backup.Config, video.Config, string, string) {
	photosPath := requireEnv("PHOTOS_PATH")
	ssdMount := requireEnv("SSD_MOUNT")
	backupPath := getEnv("BACKUP_PATH", photosPath+"/db-backup")
	postgresContainer := getEnv("POSTGRES_CONTAINER", "immich_postgres")
	immichContainer := getEnv("IMMICH_CONTAINER", "immich_server")
	dbUser := getEnv("DB_USER", "postgres")
	dbUserPass := getEnv("DB_PASS", "postgres")

	monCfg := monitor.Config{
		SSDMount:          ssdMount,
		PhotosPath:        photosPath,
		PostgresContainer: postgresContainer,
		ImmichContainer:   immichContainer,
	}

	bakCfg := backup.Config{
		BackupPath:        backupPath,
		PostgresContainer: postgresContainer,
		DBUser:            dbUser,
		DBUserPass:        dbUserPass,
		KeepWeeks:         4,
	}

	vidCfg := video.Config{
		LibraryPath: photosPath + "/library",
		CRF:         getEnv("VIDEO_CRF", "28"),
		Preset:      getEnv("VIDEO_PRESET", "medium"),
	}

	pushoverAppKey := requireEnv("PUSHOVER_APP_KEY")
	pushoverUserKey := requireEnv("PUSHOVER_USER_KEY")

	return monCfg, bakCfg, vidCfg, pushoverAppKey, pushoverUserKey
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
