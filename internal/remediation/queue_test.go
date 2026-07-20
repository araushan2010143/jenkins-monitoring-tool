package remediation

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func TestEnqueue_PushesJobJSON(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()

	job := Job{
		Master:      "m1",
		MasterURL:   "https://jenkins.example.com",
		Node:        "ec2-agent-01",
		InstanceID:  "i-123",
		Reason:      "oom",
		Fingerprint: "abc123",
		Labels:      []string{"team-qa"},
		DetectedAt:  time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
	}

	if err := Enqueue(ctx, rdb, job); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	raw, err := rdb.LPop(ctx, jobsKey).Result()
	if err != nil {
		t.Fatalf("LPop: %v", err)
	}

	var got Job
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("unmarshal enqueued job: %v", err)
	}
	if got.Node != job.Node || got.InstanceID != job.InstanceID || len(got.Labels) != 1 {
		t.Errorf("unexpected decoded job: %+v", got)
	}
}

func TestIsTripped_FalseByDefault(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()

	tripped, err := IsTripped(ctx, rdb, "m1", "node-1")
	if err != nil {
		t.Fatalf("IsTripped: %v", err)
	}
	if tripped {
		t.Error("expected IsTripped to be false with no tripped key set")
	}
}

func TestIsTripped_TrueWhenKeySet(t *testing.T) {
	rdb := newTestRedis(t)
	ctx := context.Background()

	if err := rdb.Set(ctx, trippedKey("m1", "node-1"), "1", 0).Err(); err != nil {
		t.Fatalf("seed tripped key: %v", err)
	}

	tripped, err := IsTripped(ctx, rdb, "m1", "node-1")
	if err != nil {
		t.Fatalf("IsTripped: %v", err)
	}
	if !tripped {
		t.Error("expected IsTripped to be true once circuit:tripped key is set")
	}
}
