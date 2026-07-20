// Package jenkins provides a client for the Jenkins Remote Access API,
// focused on the /computer/api/json endpoint used to detect offline agents.
package jenkins

// ComputerSet is the top-level response from /computer/api/json.
type ComputerSet struct {
	Computer []Computer `json:"computer"`
}

// Computer mirrors the subset of hudson.model.Computer fields the observer
// cares about: connection state, offline diagnostics, and routing labels.
type Computer struct {
	DisplayName        string  `json:"displayName"`
	Offline             bool    `json:"offline"`
	TemporarilyOffline   bool    `json:"temporarilyOffline"`
	OfflineCauseReason  string  `json:"offlineCauseReason"`
	Idle                 bool    `json:"idle"`
	AssignedLabels      []Label `json:"assignedLabels"`
}

// Label mirrors hudson.model.Label as embedded in a Computer's assignedLabels.
type Label struct {
	Name string `json:"name"`
}

// LabelNames returns the flat list of label strings for routing lookups.
func (c Computer) LabelNames() []string {
	names := make([]string, 0, len(c.AssignedLabels))
	for _, l := range c.AssignedLabels {
		if l.Name != "" {
			names = append(names, l.Name)
		}
	}
	return names
}

// Reason returns the offline cause, falling back to a stable placeholder so
// dedup fingerprints and alert payloads never carry an empty field.
func (c Computer) Reason() string {
	if c.OfflineCauseReason == "" {
		return "unknown"
	}
	return c.OfflineCauseReason
}
