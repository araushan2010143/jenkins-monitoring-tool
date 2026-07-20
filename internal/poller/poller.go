// Package poller wires the jenkins client, dedup store, and Teams notifier
// together into a periodic polling loop.
package poller

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"jenkins-monitoring-tool/internal/dedup"
	"jenkins-monitoring-tool/internal/jenkins"
	"jenkins-monitoring-tool/internal/metrics"
	"jenkins-monitoring-tool/internal/notify"
	"jenkins-monitoring-tool/internal/remediation"
)

// Poller periodically polls a set of Jenkins masters and routes offline
// agent alerts through dedup and Teams notification.
type Poller struct {
	masters   []jenkins.MasterConfig
	interval  time.Duration
	jc        *jenkins.Client
	dd        *dedup.Deduplicator
	notifier  *notify.Notifier
	router    *notify.Router
	rec       *metrics.Recorder
	log       *slog.Logger
	rdb       *redis.Client
	instances map[string]string // node display name -> EC2 instance ID, for remediation jobs
}

// New builds a Poller from its collaborators. rdb is used directly (beyond
// dedup) for enqueueing remediation jobs, checking circuit breaker state,
// and tracking offline->online recovery. instances maps a node's display
// name to its EC2 instance ID; nodes absent from the map are still polled
// and alerted on, they just never get a remediation job enqueued.
func New(
	masters []jenkins.MasterConfig,
	interval time.Duration,
	jc *jenkins.Client,
	dd *dedup.Deduplicator,
	notifier *notify.Notifier,
	router *notify.Router,
	rec *metrics.Recorder,
	log *slog.Logger,
	rdb *redis.Client,
	instances map[string]string,
) *Poller {
	return &Poller{
		masters:   masters,
		interval:  interval,
		jc:        jc,
		dd:        dd,
		notifier:  notifier,
		router:    router,
		rec:       rec,
		log:       log,
		rdb:       rdb,
		instances: instances,
	}
}

// Run blocks, polling every interval until ctx is cancelled. It polls
// immediately on start rather than waiting for the first tick.
func (p *Poller) Run(ctx context.Context) {
	p.pollOnce(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Info("poller stopping")
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	var wg sync.WaitGroup
	for _, m := range p.masters {
		wg.Add(1)
		go func(m jenkins.MasterConfig) {
			defer wg.Done()
			p.pollMaster(ctx, m)
		}(m)
	}
	wg.Wait()
}

func (p *Poller) pollMaster(ctx context.Context, m jenkins.MasterConfig) {
	p.rec.IncPolls()

	cs, err := p.jc.FetchComputers(ctx, m)
	if err != nil {
		p.rec.IncPollErrors()
		p.log.Error("poll failed", "master", m.Name, "error", err)
		return
	}

	offlineNow := make(map[string][]string)
	for _, c := range cs.Computer {
		if !c.Offline {
			continue
		}
		offlineNow[c.DisplayName] = c.LabelNames()
		p.handleOffline(ctx, m, c)
	}
	p.rec.SetOfflineGauge(m.Name, len(offlineNow))
	p.trackRecovery(ctx, m, offlineNow)
}

func (p *Poller) handleOffline(ctx context.Context, m jenkins.MasterConfig, c jenkins.Computer) {
	reason := c.Reason()

	res, err := p.dd.Process(ctx, m.URL, c.DisplayName, reason)
	if err != nil {
		p.log.Error("dedup failed", "master", m.Name, "node", c.DisplayName, "error", err)
		return
	}

	if res.FirstSeen {
		p.enqueueRemediation(ctx, m, c, reason)
	}

	if !res.ShouldNotify {
		p.rec.IncSuppressed()
		p.log.Debug("alert suppressed", "master", m.Name, "node", c.DisplayName, "count", res.Count)
		return
	}

	webhook := p.router.Resolve(c.LabelNames())
	if webhook == "" {
		p.log.Warn("no webhook resolved for offline agent", "master", m.Name, "node", c.DisplayName)
		return
	}

	payload := notify.BuildOfflineCard(notify.AlertEvent{
		MasterName: m.Name,
		MasterURL:  m.URL,
		NodeName:   c.DisplayName,
		Reason:     reason,
		DetectedAt: time.Now(),
		Count:      res.Count,
		Escalated:  !res.FirstSeen,
	})

	if err := p.notifier.Send(ctx, webhook, payload); err != nil {
		p.log.Error("notify failed", "master", m.Name, "node", c.DisplayName, "error", err)
		return
	}
	p.rec.IncAlertsSent()
	p.log.Info("alert sent", "master", m.Name, "node", c.DisplayName, "reason", reason, "count", res.Count)
}

// enqueueRemediation hands a brand-new offline incident to the Python
// remediation worker, unless the node's circuit breaker is tripped or it
// has no known EC2 instance mapping.
func (p *Poller) enqueueRemediation(ctx context.Context, m jenkins.MasterConfig, c jenkins.Computer, reason string) {
	tripped, err := remediation.IsTripped(ctx, p.rdb, m.Name, c.DisplayName)
	if err != nil {
		p.log.Error("circuit breaker check failed", "master", m.Name, "node", c.DisplayName, "error", err)
		return
	}
	if tripped {
		p.log.Warn("circuit breaker tripped, skipping remediation enqueue", "master", m.Name, "node", c.DisplayName)
		return
	}

	instanceID := p.instances[c.DisplayName]
	if instanceID == "" {
		p.log.Debug("no instance mapping for node, skipping remediation enqueue", "master", m.Name, "node", c.DisplayName)
		return
	}

	job := remediation.Job{
		Master:      m.Name,
		MasterURL:   m.URL,
		Node:        c.DisplayName,
		InstanceID:  instanceID,
		Reason:      reason,
		Fingerprint: dedup.Fingerprint(m.URL, c.DisplayName, reason),
		Labels:      c.LabelNames(),
		DetectedAt:  time.Now(),
	}
	if err := remediation.Enqueue(ctx, p.rdb, job); err != nil {
		p.log.Error("failed to enqueue remediation job", "master", m.Name, "node", c.DisplayName, "error", err)
		return
	}
	p.log.Info("remediation job enqueued", "master", m.Name, "node", c.DisplayName, "instance_id", instanceID)
}

// activeKeyPrefix returns the Redis key prefix used to track which nodes on
// a master are currently known-offline, so a later poll can detect the
// offline->online transition and announce recovery.
func activeKeyPrefix(master string) string {
	return "active:" + master + ":"
}

// trackRecovery diffs this poll's offline set against the previously
// recorded offline set for the master: nodes that dropped out of the
// offline set have recovered and get a Teams "Recovered" card; nodes still
// offline have their marker refreshed. This does not touch circuit breaker
// state — a tripped breaker requires manual SRE clearing regardless of
// whether the node comes back online on its own.
func (p *Poller) trackRecovery(ctx context.Context, m jenkins.MasterConfig, offlineNow map[string][]string) {
	prefix := activeKeyPrefix(m.Name)

	iter := p.rdb.Scan(ctx, 0, prefix+"*", 0).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()
		node := strings.TrimPrefix(key, prefix)
		if _, stillOffline := offlineNow[node]; stillOffline {
			continue
		}

		labelsCSV, err := p.rdb.Get(ctx, key).Result()
		if err != nil && err != redis.Nil {
			p.log.Error("failed to read active-offline marker", "master", m.Name, "node", node, "error", err)
		}
		p.handleRecovery(ctx, m, node, splitLabels(labelsCSV))

		if err := p.rdb.Del(ctx, key).Err(); err != nil {
			p.log.Error("failed to clear active-offline marker", "master", m.Name, "node", node, "error", err)
		}
	}
	if err := iter.Err(); err != nil {
		p.log.Error("failed to scan active-offline markers", "master", m.Name, "error", err)
	}

	for node, labels := range offlineNow {
		if err := p.rdb.Set(ctx, prefix+node, strings.Join(labels, ","), 0).Err(); err != nil {
			p.log.Error("failed to set active-offline marker", "master", m.Name, "node", node, "error", err)
		}
	}
}

func (p *Poller) handleRecovery(ctx context.Context, m jenkins.MasterConfig, node string, labels []string) {
	webhook := p.router.Resolve(labels)
	if webhook == "" {
		return
	}

	payload := notify.BuildRecoveredCard(notify.RecoveredEvent{
		MasterName:  m.Name,
		MasterURL:   m.URL,
		NodeName:    node,
		RecoveredAt: time.Now(),
	})
	if err := p.notifier.Send(ctx, webhook, payload); err != nil {
		p.log.Error("recovery notify failed", "master", m.Name, "node", node, "error", err)
		return
	}
	p.log.Info("recovery alert sent", "master", m.Name, "node", node)
}

func splitLabels(csv string) []string {
	if csv == "" {
		return nil
	}
	return strings.Split(csv, ",")
}
