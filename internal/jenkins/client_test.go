package jenkins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func serveFixture(t *testing.T, path string) *httptest.Server {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/computer/api/json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	}))
}

func TestFetchComputers_ParsesOfflineAgent(t *testing.T) {
	srv := serveFixture(t, "../../testdata/fixtures/one-offline.json")
	defer srv.Close()

	c := NewClient(2 * time.Second)
	cs, err := c.FetchComputers(context.Background(), MasterConfig{Name: "test", URL: srv.URL})
	if err != nil {
		t.Fatalf("FetchComputers: %v", err)
	}

	if len(cs.Computer) != 2 {
		t.Fatalf("expected 2 computers, got %d", len(cs.Computer))
	}

	var agent *Computer
	for i := range cs.Computer {
		if cs.Computer[i].DisplayName == "ec2-agent-01" {
			agent = &cs.Computer[i]
		}
	}
	if agent == nil {
		t.Fatal("ec2-agent-01 not found in parsed response")
	}
	if !agent.Offline {
		t.Error("expected ec2-agent-01 to be offline")
	}
	if agent.Reason() == "unknown" {
		t.Error("expected a non-empty offline reason")
	}
	labels := agent.LabelNames()
	if len(labels) != 2 || labels[0] != "team-qa" {
		t.Errorf("unexpected labels: %v", labels)
	}
}

func TestFetchComputers_AllHealthy(t *testing.T) {
	srv := serveFixture(t, "../../testdata/fixtures/all-healthy.json")
	defer srv.Close()

	c := NewClient(2 * time.Second)
	cs, err := c.FetchComputers(context.Background(), MasterConfig{Name: "test", URL: srv.URL})
	if err != nil {
		t.Fatalf("FetchComputers: %v", err)
	}
	for _, comp := range cs.Computer {
		if comp.Offline {
			t.Errorf("expected all computers healthy, %s is offline", comp.DisplayName)
		}
	}
}

func TestFetchComputers_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := NewClient(2 * time.Second)
	_, err := c.FetchComputers(context.Background(), MasterConfig{Name: "test", URL: srv.URL})
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
}

func TestComputer_ReasonFallback(t *testing.T) {
	c := Computer{DisplayName: "x", Offline: true}
	if c.Reason() != "unknown" {
		t.Errorf("expected fallback reason 'unknown', got %q", c.Reason())
	}
}
