package notify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBuildOfflineCard_Structure(t *testing.T) {
	evt := AlertEvent{
		MasterName: "prod-master-1",
		MasterURL:  "https://jenkins.example.com/",
		NodeName:   "ec2-agent-01",
		Reason:     "Native System Memory Exhaustion",
		DetectedAt: time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC),
		Count:      1,
		Escalated:  false,
	}

	payload := BuildOfflineCard(evt)

	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if decoded["type"] != "message" {
		t.Errorf("expected type=message, got %v", decoded["type"])
	}

	attachments, ok := decoded["attachments"].([]any)
	if !ok || len(attachments) != 1 {
		t.Fatalf("expected exactly 1 attachment, got %v", decoded["attachments"])
	}
	attachment := attachments[0].(map[string]any)
	if attachment["contentType"] != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("unexpected contentType: %v", attachment["contentType"])
	}

	card := attachment["content"].(map[string]any)
	if card["version"] != "1.5" {
		t.Errorf("expected Adaptive Card version 1.5, got %v", card["version"])
	}
	if card["type"] != "AdaptiveCard" {
		t.Errorf("expected type AdaptiveCard, got %v", card["type"])
	}

	actions, ok := card["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("expected exactly 1 action, got %v", card["actions"])
	}
	action := actions[0].(map[string]any)
	if action["type"] != "Action.OpenUrl" {
		t.Errorf("expected Action.OpenUrl, got %v", action["type"])
	}
	wantURL := "https://jenkins.example.com/computer/ec2-agent-01"
	if action["url"] != wantURL {
		t.Errorf("expected status url %q, got %v", wantURL, action["url"])
	}
}

func TestBuildOfflineCard_EscalatedTitleIncludesCount(t *testing.T) {
	evt := AlertEvent{
		MasterName: "m",
		MasterURL:  "https://jenkins.example.com",
		NodeName:   "agent-x",
		Reason:     "disk full",
		DetectedAt: time.Now(),
		Count:      10,
		Escalated:  true,
	}
	payload := BuildOfflineCard(evt)
	raw, _ := json.Marshal(payload)
	if !strings.Contains(string(raw), "x10") {
		t.Errorf("expected escalated title to reference occurrence count, payload: %s", raw)
	}
}

func TestBuildRecoveredCard_Structure(t *testing.T) {
	evt := RecoveredEvent{
		MasterName:  "prod-master-1",
		MasterURL:   "https://jenkins.example.com/",
		NodeName:    "ec2-agent-01",
		RecoveredAt: time.Date(2026, 7, 21, 12, 5, 0, 0, time.UTC),
	}

	payload := BuildRecoveredCard(evt)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if !strings.Contains(string(raw), "Recovered") {
		t.Errorf("expected recovered card title to mention recovery, payload: %s", raw)
	}
	wantURL := "https://jenkins.example.com/computer/ec2-agent-01"
	if !strings.Contains(string(raw), wantURL) {
		t.Errorf("expected status url %q in payload, got: %s", wantURL, raw)
	}
}
