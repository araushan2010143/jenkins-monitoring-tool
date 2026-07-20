package notify

// Router resolves which Teams webhook an alert should go to, based on the
// offline agent's assigned labels (e.g. team-qa, team-billing, env-prod).
type Router struct {
	Default string
	Routes  map[string]string
}

// NewRouter builds a Router from a label->webhook map and a mandatory
// fallback used when no label matches (or the agent has no labels).
func NewRouter(defaultWebhook string, routes map[string]string) *Router {
	return &Router{Default: defaultWebhook, Routes: routes}
}

// Resolve returns the webhook URL for the first matching label, or Default.
func (r *Router) Resolve(labels []string) string {
	for _, l := range labels {
		if webhook, ok := r.Routes[l]; ok && webhook != "" {
			return webhook
		}
	}
	return r.Default
}
