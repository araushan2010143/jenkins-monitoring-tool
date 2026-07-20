package poller

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"jenkins-monitoring-tool/internal/dedup"
	"jenkins-monitoring-tool/internal/jenkins"
	"jenkins-monitoring-tool/internal/metrics"
	"jenkins-monitoring-tool/internal/notify"
)

// promauto registers on the default registry; sharing one Recorder across
// every test in this file avoids "duplicate metrics collector" panics.
var sharedRecorder = metrics.NewRecorder()

const offlineBody = `{"computer":[{"displayName":"agent-1","offline":true,"offlineCauseReason":"oom","assignedLabels":[{"name":"team-qa"}]}]}`
const healthyBody = `{"computer":[{"displayName":"agent-1","offline":false}]}`

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestRedisClient(t *testing.T) *redis.Client {
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

// jenkinsStub serves a mutable computer-set JSON so tests can flip a node
// between offline and online across poll cycles.
type jenkinsStub struct {
	mu   sync.Mutex
	body string
}

func (s *jenkinsStub) set(body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
}

func (s *jenkinsStub) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(s.body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newWebhookCapture(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var received []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		received = append(received, string(raw))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	get := func() []string {
		mu.Lock()
		defer mu.Unlock()
		out := make([]string, len(received))
		copy(out, received)
		return out
	}
	return srv, get
}

func newTestPoller(jenkinsURL string, rdb *redis.Client, webhookURL string, instances map[string]string) *Poller {
	masters := []jenkins.MasterConfig{{Name: "m1", URL: jenkinsURL}}
	return New(
		masters,
		time.Hour,
		jenkins.NewClient(2*time.Second),
		dedup.New(rdb, 5*time.Minute),
		notify.New(2*time.Second),
		notify.NewRouter(webhookURL, nil),
		sharedRecorder,
		discardLogger(),
		rdb,
		instances,
	)
}

func TestHandleOffline_EnqueuesRemediationOnFirstSeen(t *testing.T) {
	rdb := newTestRedisClient(t)
	stub := &jenkinsStub{}
	stub.set(offlineBody)
	jenkinsSrv := stub.server(t)
	webhookSrv, _ := newWebhookCapture(t)

	p := newTestPoller(jenkinsSrv.URL, rdb, webhookSrv.URL, map[string]string{"agent-1": "i-123"})
	p.pollOnce(context.Background())

	n, err := rdb.LLen(context.Background(), "remediation:jobs").Result()
	if err != nil {
		t.Fatalf("LLen: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 enqueued remediation job, got %d", n)
	}

	raw, err := rdb.LPop(context.Background(), "remediation:jobs").Result()
	if err != nil {
		t.Fatalf("LPop: %v", err)
	}
	var job map[string]any
	if err := json.Unmarshal([]byte(raw), &job); err != nil {
		t.Fatalf("unmarshal job: %v", err)
	}
	if job["instance_id"] != "i-123" || job["node"] != "agent-1" {
		t.Errorf("unexpected job payload: %v", job)
	}
}

func TestHandleOffline_SkipsEnqueueWhenCircuitTripped(t *testing.T) {
	rdb := newTestRedisClient(t)
	rdb.Set(context.Background(), "circuit:tripped:m1:agent-1", "1", 0)

	stub := &jenkinsStub{}
	stub.set(offlineBody)
	jenkinsSrv := stub.server(t)
	webhookSrv, getPosts := newWebhookCapture(t)

	p := newTestPoller(jenkinsSrv.URL, rdb, webhookSrv.URL, map[string]string{"agent-1": "i-123"})
	p.pollOnce(context.Background())

	n, err := rdb.LLen(context.Background(), "remediation:jobs").Result()
	if err != nil {
		t.Fatalf("LLen: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected no remediation job enqueued while circuit tripped, got %d", n)
	}

	// The offline Teams alert should still fire even though remediation is skipped.
	if len(getPosts()) != 1 {
		t.Errorf("expected the offline alert to still be sent, got %d posts", len(getPosts()))
	}
}

func TestHandleOffline_NoInstanceMappingSkipsEnqueue(t *testing.T) {
	rdb := newTestRedisClient(t)
	stub := &jenkinsStub{}
	stub.set(offlineBody)
	jenkinsSrv := stub.server(t)
	webhookSrv, _ := newWebhookCapture(t)

	p := newTestPoller(jenkinsSrv.URL, rdb, webhookSrv.URL, map[string]string{})
	p.pollOnce(context.Background())

	n, err := rdb.LLen(context.Background(), "remediation:jobs").Result()
	if err != nil {
		t.Fatalf("LLen: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected no remediation job without an instance mapping, got %d", n)
	}
}

func TestPoller_DetectsRecoveryAcrossPolls(t *testing.T) {
	rdb := newTestRedisClient(t)
	stub := &jenkinsStub{}
	stub.set(offlineBody)
	jenkinsSrv := stub.server(t)
	webhookSrv, getPosts := newWebhookCapture(t)

	p := newTestPoller(jenkinsSrv.URL, rdb, webhookSrv.URL, map[string]string{"agent-1": "i-123"})

	p.pollOnce(context.Background())
	if exists, _ := rdb.Exists(context.Background(), "active:m1:agent-1").Result(); exists == 0 {
		t.Fatal("expected active-offline marker to be set after first poll")
	}

	stub.set(healthyBody)
	p.pollOnce(context.Background())

	if exists, _ := rdb.Exists(context.Background(), "active:m1:agent-1").Result(); exists != 0 {
		t.Error("expected active-offline marker to be cleared after recovery")
	}

	found := false
	for _, post := range getPosts() {
		if strings.Contains(post, "Recovered") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a Recovered card among posted payloads: %v", getPosts())
	}
}
