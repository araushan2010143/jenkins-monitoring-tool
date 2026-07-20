package jenkins

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// MasterConfig identifies one Jenkins controller to poll.
type MasterConfig struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Username string `json:"username"`
	APIToken string `json:"apiToken"`
}

// Client polls the Jenkins Remote Access API for computer (agent) status.
type Client struct {
	httpClient *http.Client
}

// NewClient builds a Client with the given per-request timeout.
func NewClient(timeout time.Duration) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
	}
}

// FetchComputers queries {master.URL}/computer/api/json?depth=2 and decodes
// the resulting agent status collection.
func (c *Client) FetchComputers(ctx context.Context, m MasterConfig) (*ComputerSet, error) {
	endpoint := strings.TrimRight(m.URL, "/") + "/computer/api/json?depth=2"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for master %q: %w", m.Name, err)
	}
	if m.Username != "" {
		req.SetBasicAuth(m.Username, m.APIToken)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query master %q: %w", m.Name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("master %q returned status %d", m.Name, resp.StatusCode)
	}

	var cs ComputerSet
	if err := json.NewDecoder(resp.Body).Decode(&cs); err != nil {
		return nil, fmt.Errorf("decode response from master %q: %w", m.Name, err)
	}
	return &cs, nil
}
