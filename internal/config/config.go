// Package config loads the observer's runtime configuration: environment
// variables for wiring (Redis, ports, timeouts) and JSON files for the list
// of Jenkins masters to poll and the label-to-webhook routing table.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"jenkins-monitoring-tool/internal/jenkins"
)

// AppConfig holds all environment-driven settings for the observer daemon.
type AppConfig struct {
	MastersFile   string
	RoutingFile   string
	InstancesFile string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	DedupWindow   time.Duration
	PollInterval  time.Duration
	HTTPTimeout   time.Duration
	MetricsAddr   string
	LogLevel      string
}

// RoutingConfig maps agent labels to Teams webhook URLs, with a mandatory
// default used when no label matches.
type RoutingConfig struct {
	Default string            `json:"default"`
	Routes  map[string]string `json:"routes"`
}

// LoadFromEnv reads AppConfig from environment variables, applying sane
// defaults for local/dev use so the daemon is runnable without a .env file.
func LoadFromEnv() AppConfig {
	return AppConfig{
		MastersFile:   getEnv("MASTERS_FILE", "configs/masters.json"),
		RoutingFile:   getEnv("ROUTING_FILE", "configs/routing.json"),
		InstancesFile: getEnv("INSTANCES_FILE", "configs/instances.json"),
		RedisAddr:     getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       getEnvInt("REDIS_DB", 0),
		DedupWindow:   getEnvDuration("DEDUP_WINDOW", 300*time.Second),
		PollInterval:  getEnvDuration("POLL_INTERVAL", 30*time.Second),
		HTTPTimeout:   getEnvDuration("HTTP_TIMEOUT", 10*time.Second),
		MetricsAddr:   getEnv("METRICS_ADDR", ":9090"),
		LogLevel:      getEnv("LOG_LEVEL", "info"),
	}
}

// LoadMasters reads and validates the list of Jenkins masters to poll.
func LoadMasters(path string) ([]jenkins.MasterConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read masters file %q: %w", path, err)
	}
	var masters []jenkins.MasterConfig
	if err := json.Unmarshal(data, &masters); err != nil {
		return nil, fmt.Errorf("parse masters file %q: %w", path, err)
	}
	if len(masters) == 0 {
		return nil, fmt.Errorf("masters file %q defines no masters", path)
	}
	for i, m := range masters {
		if m.Name == "" || m.URL == "" {
			return nil, fmt.Errorf("masters file %q: entry %d missing name or url", path, i)
		}
	}
	return masters, nil
}

// LoadRouting reads and validates the label-to-webhook routing table.
func LoadRouting(path string) (*RoutingConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read routing file %q: %w", path, err)
	}
	var rc RoutingConfig
	if err := json.Unmarshal(data, &rc); err != nil {
		return nil, fmt.Errorf("parse routing file %q: %w", path, err)
	}
	if rc.Default == "" {
		return nil, fmt.Errorf("routing file %q must set a default webhook", path)
	}
	return &rc, nil
}

// LoadInstances reads the optional node-name -> EC2 instance ID map used to
// enqueue remediation jobs. A missing file disables remediation enqueueing
// (empty map, not an error) so deployments that haven't configured Phase 2
// remediation keep working unchanged.
func LoadInstances(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read instances file %q: %w", path, err)
	}
	var instances map[string]string
	if err := json.Unmarshal(data, &instances); err != nil {
		return nil, fmt.Errorf("parse instances file %q: %w", path, err)
	}
	return instances, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}
