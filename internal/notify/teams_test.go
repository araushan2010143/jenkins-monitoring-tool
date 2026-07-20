package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNotifier_Send_PostsJSONWithContentType(t *testing.T) {
	var gotContentType string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := New(2 * time.Second)
	err := n.Send(context.Background(), srv.URL, map[string]any{"type": "message"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", gotContentType)
	}
	if gotBody["type"] != "message" {
		t.Errorf("unexpected body: %v", gotBody)
	}
}

func TestNotifier_Send_ErrorsOnNonSuccessStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := New(2 * time.Second)
	err := n.Send(context.Background(), srv.URL, map[string]any{"type": "message"})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}
