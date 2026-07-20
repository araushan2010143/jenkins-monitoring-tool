package dedup

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestDeduplicator(t *testing.T, window time.Duration) (*Deduplicator, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	return New(rdb, window), mr
}

func TestProcess_FirstOccurrenceNotifies(t *testing.T) {
	d, _ := newTestDeduplicator(t, 5*time.Minute)
	ctx := context.Background()

	res, err := d.Process(ctx, "https://m", "node-1", "oom")
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !res.ShouldNotify || !res.FirstSeen || res.Count != 1 {
		t.Errorf("expected first-seen notify with count 1, got %+v", res)
	}
}

func TestProcess_DuplicateSuppressedUntilThreshold(t *testing.T) {
	d, _ := newTestDeduplicator(t, 5*time.Minute)
	ctx := context.Background()

	if _, err := d.Process(ctx, "https://m", "node-1", "oom"); err != nil {
		t.Fatalf("Process (1st): %v", err)
	}

	for i := 2; i <= 4; i++ {
		res, err := d.Process(ctx, "https://m", "node-1", "oom")
		if err != nil {
			t.Fatalf("Process (%d): %v", i, err)
		}
		if res.ShouldNotify {
			t.Errorf("occurrence %d should be suppressed, got ShouldNotify=true", i)
		}
		if res.Count != int64(i) {
			t.Errorf("expected count %d, got %d", i, res.Count)
		}
	}

	// 5th occurrence crosses the first escalation threshold.
	res, err := d.Process(ctx, "https://m", "node-1", "oom")
	if err != nil {
		t.Fatalf("Process (5th): %v", err)
	}
	if !res.ShouldNotify || res.Count != 5 {
		t.Errorf("expected escalation notify at count 5, got %+v", res)
	}
}

func TestProcess_DistinctFingerprintsAreIndependent(t *testing.T) {
	d, _ := newTestDeduplicator(t, 5*time.Minute)
	ctx := context.Background()

	resA, err := d.Process(ctx, "https://m", "node-1", "oom")
	if err != nil {
		t.Fatalf("Process node-1: %v", err)
	}
	resB, err := d.Process(ctx, "https://m", "node-2", "oom")
	if err != nil {
		t.Fatalf("Process node-2: %v", err)
	}
	if !resA.FirstSeen || !resB.FirstSeen {
		t.Errorf("distinct nodes should both be first-seen: %+v %+v", resA, resB)
	}
}

func TestProcess_ExpiresAfterWindow(t *testing.T) {
	d, mr := newTestDeduplicator(t, 1*time.Minute)
	ctx := context.Background()

	if _, err := d.Process(ctx, "https://m", "node-1", "oom"); err != nil {
		t.Fatalf("Process (1st): %v", err)
	}
	mr.FastForward(2 * time.Minute)

	res, err := d.Process(ctx, "https://m", "node-1", "oom")
	if err != nil {
		t.Fatalf("Process (after expiry): %v", err)
	}
	if !res.FirstSeen {
		t.Errorf("expected a fresh incident window after expiry, got %+v", res)
	}
}

func TestFingerprint_IsDeterministicAndDistinct(t *testing.T) {
	a := Fingerprint("https://m", "node-1", "oom")
	b := Fingerprint("https://m", "node-1", "oom")
	c := Fingerprint("https://m", "node-2", "oom")

	if a != b {
		t.Error("expected identical inputs to produce identical fingerprints")
	}
	if a == c {
		t.Error("expected different node names to produce different fingerprints")
	}
}
