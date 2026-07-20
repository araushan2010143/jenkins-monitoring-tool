// Command observer is the Jenkins agent monitoring daemon: it polls
// configured Jenkins masters, deduplicates failures via Redis, and posts
// Adaptive Card alerts to Microsoft Teams webhooks.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/redis/go-redis/v9"

	"jenkins-monitoring-tool/internal/config"
	"jenkins-monitoring-tool/internal/dedup"
	"jenkins-monitoring-tool/internal/jenkins"
	"jenkins-monitoring-tool/internal/metrics"
	"jenkins-monitoring-tool/internal/notify"
	"jenkins-monitoring-tool/internal/poller"
)

func main() {
	cfg := config.LoadFromEnv()
	log := newLogger(cfg.LogLevel)

	masters, err := config.LoadMasters(cfg.MastersFile)
	if err != nil {
		log.Error("failed to load masters config", "error", err)
		os.Exit(1)
	}
	routing, err := config.LoadRouting(cfg.RoutingFile)
	if err != nil {
		log.Error("failed to load routing config", "error", err)
		os.Exit(1)
	}
	instances, err := config.LoadInstances(cfg.InstancesFile)
	if err != nil {
		log.Error("failed to load instances config", "error", err)
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer rdb.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Error("failed to connect to redis", "addr", cfg.RedisAddr, "error", err)
		os.Exit(1)
	}

	rec := metrics.NewRecorder()

	p := poller.New(
		masters,
		cfg.PollInterval,
		jenkins.NewClient(cfg.HTTPTimeout),
		dedup.New(rdb, cfg.DedupWindow),
		notify.New(cfg.HTTPTimeout),
		notify.NewRouter(routing.Default, routing.Routes),
		rec,
		log,
		rdb,
		instances,
	)

	go serveOps(cfg.MetricsAddr, log)

	log.Info("observer starting",
		"masters", len(masters),
		"poll_interval", cfg.PollInterval,
		"metrics_addr", cfg.MetricsAddr,
		"remediation_instances", len(instances),
	)
	p.Run(ctx)
	log.Info("observer stopped")
}

func serveOps(addr string, log *slog.Logger) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", metrics.HealthzHandler)
	mux.Handle("/metrics", metrics.Handler())

	if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
		log.Error("ops http server failed", "error", err)
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler)
}
