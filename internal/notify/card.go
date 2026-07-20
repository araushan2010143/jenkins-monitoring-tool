package notify

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// AlertEvent carries everything needed to render one offline-agent notification.
type AlertEvent struct {
	MasterName string
	MasterURL  string
	NodeName   string
	Reason     string
	DetectedAt time.Time
	Count      int64
	Escalated  bool
}

// RecoveredEvent carries everything needed to render a recovery notification,
// posted when an agent that was previously offline is seen online again.
type RecoveredEvent struct {
	MasterName  string
	MasterURL   string
	NodeName    string
	RecoveredAt time.Time
}

func statusURL(masterURL, nodeName string) string {
	return strings.TrimRight(masterURL, "/") + "/computer/" + url.PathEscape(nodeName)
}

// BuildOfflineCard renders an Adaptive Card v1.5 payload for a Teams
// incoming webhook, per the blueprint's formatting rules: plain-text header
// (no markdown), a fact set with the key diagnostic fields, and an
// Action.OpenUrl button linking to the agent's status page.
func BuildOfflineCard(e AlertEvent) map[string]any {
	title := fmt.Sprintf("Jenkins Agent Offline: %s", e.NodeName)
	if e.Escalated {
		title = fmt.Sprintf("Jenkins Agent Still Offline (x%d): %s", e.Count, e.NodeName)
	}

	facts := []map[string]any{
		{"title": "Master Controller", "value": e.MasterName},
		{"title": "Agent Name", "value": e.NodeName},
		{"title": "Offline Cause", "value": e.Reason},
		{"title": "Detected At", "value": e.DetectedAt.UTC().Format(time.RFC3339)},
		{"title": "Occurrence Count", "value": fmt.Sprintf("%d", e.Count)},
	}

	return buildCard(title, facts, statusURL(e.MasterURL, e.NodeName))
}

// BuildRecoveredCard renders a Teams Adaptive Card announcing that a
// previously offline agent has reconnected.
func BuildRecoveredCard(e RecoveredEvent) map[string]any {
	title := fmt.Sprintf("Jenkins Agent Recovered: %s", e.NodeName)

	facts := []map[string]any{
		{"title": "Master Controller", "value": e.MasterName},
		{"title": "Agent Name", "value": e.NodeName},
		{"title": "Recovered At", "value": e.RecoveredAt.UTC().Format(time.RFC3339)},
	}

	return buildCard(title, facts, statusURL(e.MasterURL, e.NodeName))
}

// buildCard wraps a title + fact set into a full Teams message payload
// containing a single Adaptive Card v1.5 attachment with a status-page
// Action.OpenUrl button.
func buildCard(title string, facts []map[string]any, agentStatusURL string) map[string]any {
	body := []map[string]any{
		{
			"type":   "TextBlock",
			"text":   title,
			"weight": "Bolder",
			"size":   "Large",
			"wrap":   true,
		},
		{
			"type":  "FactSet",
			"facts": facts,
		},
	}

	actions := []map[string]any{
		{
			"type":  "Action.OpenUrl",
			"title": "View Agent Status",
			"url":   agentStatusURL,
		},
	}

	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.5",
		"body":    body,
		"actions": actions,
	}

	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}
}
