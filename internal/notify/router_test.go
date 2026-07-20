package notify

import "testing"

func TestRouter_ResolveMatchesLabel(t *testing.T) {
	r := NewRouter("https://default", map[string]string{
		"team-qa": "https://team-qa-webhook",
	})
	got := r.Resolve([]string{"linux", "team-qa"})
	if got != "https://team-qa-webhook" {
		t.Errorf("expected team-qa webhook, got %q", got)
	}
}

func TestRouter_ResolveFallsBackToDefault(t *testing.T) {
	r := NewRouter("https://default", map[string]string{
		"team-qa": "https://team-qa-webhook",
	})
	got := r.Resolve([]string{"linux", "team-billing"})
	if got != "https://default" {
		t.Errorf("expected default webhook, got %q", got)
	}
}

func TestRouter_ResolveNoLabelsUsesDefault(t *testing.T) {
	r := NewRouter("https://default", map[string]string{})
	got := r.Resolve(nil)
	if got != "https://default" {
		t.Errorf("expected default webhook, got %q", got)
	}
}
