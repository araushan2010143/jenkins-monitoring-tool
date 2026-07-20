// Command mockjenkins is a minimal stand-in for a Jenkins controller's
// /computer/api/json and /prometheus/ endpoints, so the observer's full
// pipeline (poll -> dedup -> notify -> remediate) and the Grafana
// dashboards can be exercised locally without standing up a real
// multi-agent Jenkins cluster. It serves a static fixture file whose
// content can be swapped via FIXTURE_FILE to simulate different agent
// health scenarios.
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"

	"jenkins-monitoring-tool/internal/promfake"
)

func main() {
	addr := getEnv("LISTEN_ADDR", ":8080")
	fixture := getEnv("FIXTURE_FILE", "testdata/fixtures/one-offline.json")

	mux := http.NewServeMux()
	mux.HandleFunc("/computer/api/json", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(fixture)
		if err != nil {
			http.Error(w, "fixture not found: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(data)
	})
	mux.HandleFunc("/prometheus/", func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(fixture)
		if err != nil {
			http.Error(w, "fixture not found: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var snapshot promfake.Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			http.Error(w, "fixture decode failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(promfake.Render(snapshot)))
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	log.Printf("mockjenkins serving %s on %s", fixture, addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
