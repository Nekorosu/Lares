package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"lares/internal/audit"
	"lares/internal/cleanup"
	"lares/internal/config"
	"lares/internal/db"
	"lares/internal/download"
	"lares/internal/ratelimit"
	"lares/internal/securitylog"
	"lares/internal/settings"
	"lares/internal/speedlimit"
	"lares/internal/storage"
	"lares/internal/traffic"
	"lares/internal/upload"
	"lares/internal/zip"
)

func main() {
	configPath := flag.String("config", "/etc/lares/config.yaml", "Path to config file")
	flag.Parse()

	// 1. Load configuration
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// 2. Initialize Database
	database, err := db.InitDB(cfg.Paths.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// 3. Initialize Services
	secLog, err := securitylog.NewLogger(cfg.Paths.SecurityLog)
	if err != nil {
		log.Fatalf("Failed to initialize security logger: %v", err)
	}
	defer secLog.Close()

	auditLogger := audit.NewLogger(database)
	st := storage.NewStorage(cfg)
	trafficTracker := traffic.NewTracker(database)
	rl := ratelimit.NewLimiter(database)
	speedManager := speedlimit.NewSpeedManager(cfg.SpeedLimits.ExternalUploadLimitMbps, cfg.SpeedLimits.ExternalDownloadLimitMbps, cfg.SpeedLimits.BurstMB)
	speedTracker := speedlimit.NewSpeedTracker()

	uploadManager := upload.NewManager(database, cfg, st, trafficTracker)
	downloadManager := download.NewManager(database, st, trafficTracker, speedManager, speedTracker)
	zipService := zip.NewZipService(database, st, trafficTracker, speedManager, speedTracker)
	settingsManager := settings.NewManager(database, cfg)

	// 4. Start Background Worker
	cleanupWorker := cleanup.NewWorker(database, cfg, st, auditLogger, trafficTracker)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go cleanupWorker.Start(ctx)

	// 5. Mux & Static Web Handling
	mux := http.NewServeMux()

	// Serve Static Files
	fs := http.FileServer(http.Dir("web/static"))
	mux.Handle("/static/", http.StripPrefix("/static/", fs))

	// Healthcheck
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","service":"lares"}`))
	})

	// Unused variable suppressions for now
	_ = uploadManager
	_ = downloadManager
	_ = zipService
	_ = settingsManager
	_ = rl

	server := &http.Server{
		Addr:         cfg.Listen,
		Handler:      mux,
		ReadTimeout:  30 * time.Minute,
		WriteTimeout: 30 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	// 6. Graceful Shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Printf("Lares server starting on http://%s", cfg.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server HTTP error: %v", err)
		}
	}()

	<-stop
	log.Println("Shutting down Lares server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced shutdown: %v", err)
	}
	log.Println("Lares server stopped.")
}
