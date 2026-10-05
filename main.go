package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"easy_proxies/internal/app"
	"easy_proxies/internal/config"
	"easy_proxies/internal/monitor"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Covers the launcher's graceful shutdown window for a previous instance.
const instanceLockWait = 40 * time.Second

func main() {
	var configPath string
	var shutdownOnStdinClose bool
	flag.StringVar(&configPath, "config", "config.yaml", "path to config file")
	flag.BoolVar(&shutdownOnStdinClose, "shutdown-on-stdin-close", false, "exit when the owning launcher's stdin pipe closes")
	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if shutdownOnStdinClose {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			cancel()
			// The owner must not leave listeners behind even if a dependency
			// fails to honor cancellation during startup or shutdown.
			time.Sleep(35 * time.Second)
			os.Exit(1)
		}()
	}

	releaseLock, err := config.AcquireInstanceLock(configPath, 0)
	if errors.Is(err, config.ErrInstanceRunning) {
		log.Printf("⏳ Another instance is using %s; waiting up to %s for it to exit...", configPath, instanceLockWait)
		releaseLock, err = config.AcquireInstanceLock(configPath, instanceLockWait)
	}
	if err != nil {
		log.Fatalf("instance lock: %v", err)
	}
	defer releaseLock()

	var cfg *config.Config
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		cfg, err = config.Load(configPath)
		if err == nil {
			break
		}
		if attempt < 3 && strings.Contains(err.Error(), "config.nodes cannot be empty") {
			log.Printf("⚠️  Attempt %d/3: %v (retrying in %ds...)", attempt, err, attempt*10)
			time.Sleep(time.Duration(attempt*10) * time.Second)
			continue
		}
		log.Fatalf("load config: %v", err)
	}

	// Setup logging based on config
	setupLogging(cfg)

	if err := app.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "proxy pool exited with error: %v\n", err)
		os.Exit(1)
	}
}

func setupLogging(cfg *config.Config) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	// Always include the in-memory ring buffer for dashboard console
	writers := []io.Writer{os.Stdout, monitor.LogWriter()}

	if cfg.Log.Output == "file" {
		// Ensure log directory exists
		logDir := filepath.Dir(cfg.Log.File)
		if err := os.MkdirAll(logDir, 0o755); err != nil {
			log.Printf("\u26a0\ufe0f Failed to create log dir %s: %v, falling back to stdout", logDir, err)
		} else {
			lj := &lumberjack.Logger{
				Filename:   cfg.Log.File,
				MaxSize:    cfg.Log.MaxSize, // MB
				MaxBackups: cfg.Log.MaxBackups,
				MaxAge:     cfg.Log.MaxAge, // days
				Compress:   cfg.Log.Compress,
			}
			writers = append(writers, lj)
			log.Printf("\u2705 Log rotation enabled: file=%s, maxSize=%dMB, maxBackups=%d, maxAge=%dd",
				cfg.Log.File, cfg.Log.MaxSize, cfg.Log.MaxBackups, cfg.Log.MaxAge)
		}
	}

	log.SetOutput(io.MultiWriter(writers...))
}
