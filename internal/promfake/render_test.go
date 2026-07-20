package promfake

import (
	"strings"
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

func testSnapshot() Snapshot {
	return Snapshot{
		QueueSize:      18,
		ExecutorsBusy:  3,
		ExecutorsTotal: 4,
		Computer: []ComputerMetrics{
			{DisplayName: "built-in", Offline: false, MemUsedPercent: 42.5, DiskFreeGB: 38.2},
			{DisplayName: "ec2-agent-01", Offline: true, MemUsedPercent: 96.8, DiskFreeGB: 2.1},
		},
	}
}

func parseFamilies(t *testing.T, text string) map[string]*dto.MetricFamily {
	t.Helper()
	parser := expfmt.NewTextParser(model.LegacyValidation)
	families, err := parser.TextToMetricFamilies(strings.NewReader(text))
	if err != nil {
		t.Fatalf("Render output is not valid Prometheus exposition format: %v", err)
	}
	return families
}

func gaugeValue(t *testing.T, families map[string]*dto.MetricFamily, name string, labels map[string]string) float64 {
	t.Helper()
	family, ok := families[name]
	if !ok {
		t.Fatalf("metric family %q not found", name)
	}
	for _, m := range family.Metric {
		if labelsMatch(m.GetLabel(), labels) {
			return m.GetGauge().GetValue()
		}
	}
	t.Fatalf("no metric %q with labels %v found", name, labels)
	return 0
}

func labelsMatch(pairs []*dto.LabelPair, want map[string]string) bool {
	if len(pairs) != len(want) {
		return false
	}
	for _, p := range pairs {
		if want[p.GetName()] != p.GetValue() {
			return false
		}
	}
	return true
}

func TestRender_ProducesValidExpositionFormat(t *testing.T) {
	parseFamilies(t, Render(testSnapshot()))
}

func TestRender_QueueSize(t *testing.T) {
	families := parseFamilies(t, Render(testSnapshot()))
	if got := gaugeValue(t, families, "jenkins_queue_size", nil); got != 18 {
		t.Errorf("expected jenkins_queue_size=18, got %v", got)
	}
}

func TestRender_ExecutorCounts(t *testing.T) {
	families := parseFamilies(t, Render(testSnapshot()))
	if got := gaugeValue(t, families, "jenkins_executor_count", map[string]string{"state": "busy"}); got != 3 {
		t.Errorf("expected busy executors=3, got %v", got)
	}
	if got := gaugeValue(t, families, "jenkins_executor_count", map[string]string{"state": "idle"}); got != 1 {
		t.Errorf("expected idle executors=1, got %v", got)
	}
}

func TestRender_ExecutorCounts_NeverNegativeIdle(t *testing.T) {
	s := testSnapshot()
	s.ExecutorsBusy = 5
	s.ExecutorsTotal = 4 // pathological input: busy > total
	families := parseFamilies(t, Render(s))
	if got := gaugeValue(t, families, "jenkins_executor_count", map[string]string{"state": "idle"}); got != 0 {
		t.Errorf("expected idle executors clamped to 0, got %v", got)
	}
}

func TestRender_NodeOnlineStatus(t *testing.T) {
	families := parseFamilies(t, Render(testSnapshot()))
	if got := gaugeValue(t, families, "jenkins_node_online_status", map[string]string{"node": "built-in"}); got != 1 {
		t.Errorf("expected built-in online status=1, got %v", got)
	}
	if got := gaugeValue(t, families, "jenkins_node_online_status", map[string]string{"node": "ec2-agent-01"}); got != 0 {
		t.Errorf("expected ec2-agent-01 online status=0 (offline), got %v", got)
	}
}

func TestRender_MemAndDisk(t *testing.T) {
	families := parseFamilies(t, Render(testSnapshot()))
	if got := gaugeValue(t, families, "jenkins_node_mem_used_percent", map[string]string{"node": "ec2-agent-01"}); got != 96.8 {
		t.Errorf("expected mem_used_percent=96.8, got %v", got)
	}
	wantBytes := 2.1 * 1024 * 1024 * 1024
	if got := gaugeValue(t, families, "jenkins_node_disk_free_bytes", map[string]string{"node": "ec2-agent-01"}); got != wantBytes {
		t.Errorf("expected disk_free_bytes=%v, got %v", wantBytes, got)
	}
}
