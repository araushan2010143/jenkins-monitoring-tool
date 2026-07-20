// Package promfake stands in for a real Jenkins Prometheus plugin (plus a
// real host exporter like node_exporter for memory/disk) so the full
// metrics -> Prometheus -> Grafana pipeline can be demonstrated locally
// without either. It is a local-demo convenience only — see the README's
// Phase 3 section for what a real deployment needs instead.
package promfake

// Snapshot is decoded from the same fixture JSON cmd/mockjenkins already
// serves at /computer/api/json, with the additional fields this package
// renders as Prometheus metrics.
type Snapshot struct {
	QueueSize      int               `json:"queueSize"`
	ExecutorsBusy  int               `json:"executorsBusy"`
	ExecutorsTotal int               `json:"executorsTotal"`
	Computer       []ComputerMetrics `json:"computer"`
}

// ComputerMetrics is the subset of a fixture's computer entry this package
// cares about.
type ComputerMetrics struct {
	DisplayName    string  `json:"displayName"`
	Offline        bool    `json:"offline"`
	MemUsedPercent float64 `json:"memUsedPercent"`
	DiskFreeGB     float64 `json:"diskFreeGB"`
}
