// Package remediation lets the Go observer hand off self-healing work to
// the standalone Python remediation worker (see repo-root remediation/) via
// a Redis queue. The two processes share only a small Redis key schema —
// no direct RPC. The Go side only ever produces jobs and reads circuit
// breaker state; the Python worker owns writing circuit breaker state.
package remediation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// jobsKey is the Redis list the Python worker BLPOPs from.
const jobsKey = "remediation:jobs"

// Job is the payload enqueued for the Python worker to consume. Labels are
// included so the worker can resolve the same label-routed Teams webhook
// the original offline alert used, for remediation/escalation notices.
type Job struct {
	Master      string    `json:"master"`
	MasterURL   string    `json:"master_url"`
	Node        string    `json:"node"`
	InstanceID  string    `json:"instance_id"`
	Reason      string    `json:"reason"`
	Fingerprint string    `json:"fingerprint"`
	Labels      []string  `json:"labels"`
	DetectedAt  time.Time `json:"detected_at"`
}

// Enqueue pushes a remediation job for the Python worker to pick up.
func Enqueue(ctx context.Context, rdb *redis.Client, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("marshal remediation job: %w", err)
	}
	if err := rdb.LPush(ctx, jobsKey, data).Err(); err != nil {
		return fmt.Errorf("enqueue remediation job: %w", err)
	}
	return nil
}

// trippedKey returns the Redis key the Python worker sets when a node's
// circuit breaker has tripped (it re-failed after a remediation attempt).
func trippedKey(master, node string) string {
	return fmt.Sprintf("circuit:tripped:%s:%s", master, node)
}

// IsTripped reports whether a node's circuit breaker is currently tripped.
// This is a read-only check from the Go side — only the Python worker
// writes this key; clearing it requires manual SRE intervention.
func IsTripped(ctx context.Context, rdb *redis.Client, master, node string) (bool, error) {
	n, err := rdb.Exists(ctx, trippedKey(master, node)).Result()
	if err != nil {
		return false, fmt.Errorf("check circuit breaker for %s/%s: %w", master, node, err)
	}
	return n > 0, nil
}
