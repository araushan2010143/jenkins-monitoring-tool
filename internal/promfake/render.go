package promfake

import (
	"fmt"
	"strconv"
	"strings"
)

// Render produces Prometheus exposition-format text for the metrics the
// blueprint's Grafana dashboard queries: jenkins_queue_size,
// jenkins_executor_count, jenkins_node_online_status,
// jenkins_node_mem_used_percent, and jenkins_node_disk_free_bytes.
func Render(s Snapshot) string {
	var b strings.Builder

	writeMetricHeader(&b, "jenkins_queue_size", "gauge", "Number of items in the Jenkins build queue.")
	fmt.Fprintf(&b, "jenkins_queue_size %s\n", formatFloat(float64(s.QueueSize)))

	idle := s.ExecutorsTotal - s.ExecutorsBusy
	if idle < 0 {
		idle = 0
	}
	writeMetricHeader(&b, "jenkins_executor_count", "gauge", "Number of Jenkins executors by state.")
	fmt.Fprintf(&b, "jenkins_executor_count{state=\"busy\"} %s\n", formatFloat(float64(s.ExecutorsBusy)))
	fmt.Fprintf(&b, "jenkins_executor_count{state=\"idle\"} %s\n", formatFloat(float64(idle)))

	writeMetricHeader(&b, "jenkins_node_online_status", "gauge", "1 if the agent is online, 0 if offline.")
	for _, c := range s.Computer {
		status := 0.0
		if !c.Offline {
			status = 1.0
		}
		fmt.Fprintf(&b, "jenkins_node_online_status{node=%q} %s\n", c.DisplayName, formatFloat(status))
	}

	writeMetricHeader(&b, "jenkins_node_mem_used_percent", "gauge", "Host memory utilization percentage (stand-in for a node_exporter metric).")
	for _, c := range s.Computer {
		fmt.Fprintf(&b, "jenkins_node_mem_used_percent{node=%q} %s\n", c.DisplayName, formatFloat(c.MemUsedPercent))
	}

	writeMetricHeader(&b, "jenkins_node_disk_free_bytes", "gauge", "Free workspace disk space in bytes (stand-in for a node_exporter metric).")
	for _, c := range s.Computer {
		fmt.Fprintf(&b, "jenkins_node_disk_free_bytes{node=%q} %s\n", c.DisplayName, formatFloat(c.DiskFreeGB*1024*1024*1024))
	}

	return b.String()
}

func writeMetricHeader(b *strings.Builder, name, metricType, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s %s\n", name, metricType)
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
