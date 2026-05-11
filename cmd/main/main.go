package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/muaaz9329/Immich-guardian/internal/backup"
	"github.com/muaaz9329/Immich-guardian/internal/config"
	"github.com/muaaz9329/Immich-guardian/internal/monitor"
	"github.com/muaaz9329/Immich-guardian/internal/notify"
	"github.com/muaaz9329/Immich-guardian/internal/video"
)

func main() {
	monCfg, bakCfg, vidCfg, pushoverAppKey, pushoverUserKey := config.LoadConfig()

	notifier := notify.New(pushoverAppKey, pushoverUserKey)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mon := monitor.New(monCfg, notifier)
	bak := backup.New(bakCfg, notifier)
	vid := video.New(vidCfg, notifier)

	go mon.Start(ctx)
	go mon.StartReporter(ctx)
	go bak.Start(ctx)
	go vid.Start(ctx)

	log.Println("immich-guardian started")
	notifier.SendEmergency("immich-guardian started", "All services running: monitor, backup, video compressor.")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	notifier.SendEmergency("immich-guardian shutting down", "Received shutdown signal, stopping all services...")

	log.Println("shutting down...")
	cancel()
}
