package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Notifier posts Adaptive Card payloads to Microsoft Teams incoming webhooks.
type Notifier struct {
	httpClient *http.Client
}

// New builds a Notifier with the given per-request timeout.
func New(timeout time.Duration) *Notifier {
	return &Notifier{httpClient: &http.Client{Timeout: timeout}}
}

// Send POSTs payload as JSON to webhookURL. Per the blueprint's integration
// note, the payload is sent as a raw pre-formatted JSON body (not templated
// dynamic tokens) to keep the receiving workflow's schema intact.
func (n *Notifier) Send(ctx context.Context, webhookURL string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal teams payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build teams request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("post to teams webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("teams webhook returned status %d: %s", resp.StatusCode, snippet)
	}
	return nil
}
