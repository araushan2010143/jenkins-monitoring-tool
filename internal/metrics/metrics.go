// Package metrics exposes the observer's minimal production-readiness
// surface: a liveness probe and Prometheus counters for polls, errors,
// and alert volume. Full Grafana dashboards are a later phase; this is
// just enough to answer "is it running and is it doing anything."
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Recorder wraps the Prometheus instruments the poller updates.
type Recorder struct {
	pollsTotal            prometheus.Counter
	pollErrorsTotal       prometheus.Counter
	alertsSentTotal       prometheus.Counter
	alertsSuppressedTotal prometheus.Counter
	offlineGauge          *prometheus.GaugeVec
}

// NewRecorder registers and returns a Recorder on the default registry.
func NewRecorder() *Recorder {
	return &Recorder{
		pollsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "jenkins_observer_polls_total",
			Help: "Total number of per-master poll attempts.",
		}),
		pollErrorsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "jenkins_observer_poll_errors_total",
			Help: "Total number of failed poll attempts.",
		}),
		alertsSentTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "jenkins_observer_alerts_sent_total",
			Help: "Total number of Teams notifications sent.",
		}),
		alertsSuppressedTotal: promauto.NewCounter(prometheus.CounterOpts{
			Name: "jenkins_observer_alerts_suppressed_total",
			Help: "Total number of duplicate alerts suppressed by dedup.",
		}),
		offlineGauge: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: "jenkins_observer_agents_offline",
			Help: "Number of offline agents observed on the last poll, per master.",
		}, []string{"master"}),
	}
}

func (r *Recorder) IncPolls()       { r.pollsTotal.Inc() }
func (r *Recorder) IncPollErrors()  { r.pollErrorsTotal.Inc() }
func (r *Recorder) IncAlertsSent()  { r.alertsSentTotal.Inc() }
func (r *Recorder) IncSuppressed()  { r.alertsSuppressedTotal.Inc() }

// SetOfflineGauge records the offline agent count observed for a master on
// its most recent successful poll.
func (r *Recorder) SetOfflineGauge(master string, n int) {
	r.offlineGauge.WithLabelValues(master).Set(float64(n))
}

// Handler serves Prometheus metrics in the standard exposition format.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HealthzHandler is a trivial liveness probe.
func HealthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
