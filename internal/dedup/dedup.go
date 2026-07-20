// Package dedup implements Redis-backed alert deduplication: each distinct
// (master, node, reason) failure is fingerprinted, suppressed for a
// suppression window, and only re-notified at escalating occurrence counts
// so operators see density without being paged on every poll cycle.
package dedup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// escalationThresholds mirrors the blueprint's suppressed_alert_thresholds:
// re-notify (with the running count) only when the incident recurs this many
// times within the active window, instead of on every single poll.
var escalationThresholds = map[int64]bool{5: true, 10: true, 50: true, 100: true}

// Result describes what the caller should do with a detected failure.
type Result struct {
	ShouldNotify bool
	FirstSeen    bool
	Count        int64
}

// Deduplicator tracks incident fingerprints in Redis.
type Deduplicator struct {
	rdb    *redis.Client
	window time.Duration
}

// New builds a Deduplicator backed by the given Redis client. window is the
// suppression TTL applied to both the incident key and its occurrence counter.
func New(rdb *redis.Client, window time.Duration) *Deduplicator {
	return &Deduplicator{rdb: rdb, window: window}
}

// Fingerprint reproduces the blueprint's SHA256(masterURL + nodeName + failureType).
func Fingerprint(masterURL, nodeName, failureType string) string {
	raw := fmt.Sprintf("%s:%s:%s", masterURL, nodeName, failureType)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Process records one occurrence of a failure and reports whether it should
// trigger a notification: always on first sight, then only at the
// escalation thresholds while the incident stays active within window.
func (d *Deduplicator) Process(ctx context.Context, masterURL, nodeName, failureType string) (Result, error) {
	fp := Fingerprint(masterURL, nodeName, failureType)
	alertKey := "alert:dedup:" + fp
	countKey := "count:" + alertKey

	isNew, err := d.rdb.SetNX(ctx, alertKey, time.Now().UTC().Format(time.RFC3339), d.window).Result()
	if err != nil {
		return Result{}, fmt.Errorf("dedup SETNX %q: %w", alertKey, err)
	}
	if isNew {
		// Reset the occurrence counter for this incident window.
		if err := d.rdb.Set(ctx, countKey, 1, d.window).Err(); err != nil {
			return Result{}, fmt.Errorf("dedup init counter %q: %w", countKey, err)
		}
		return Result{ShouldNotify: true, FirstSeen: true, Count: 1}, nil
	}

	count, err := d.rdb.Incr(ctx, countKey).Result()
	if err != nil {
		return Result{}, fmt.Errorf("dedup INCR %q: %w", countKey, err)
	}
	// Keep the counter's TTL aligned with the still-active incident window.
	if err := d.rdb.Expire(ctx, countKey, d.window).Err(); err != nil {
		return Result{}, fmt.Errorf("dedup refresh TTL %q: %w", countKey, err)
	}

	return Result{ShouldNotify: escalationThresholds[count], FirstSeen: false, Count: count}, nil
}
