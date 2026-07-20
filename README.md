# Jenkins Agent Observer

A decoupled monitoring and self-healing platform that polls Jenkins
controllers for offline agents, deduplicates repeat failures via Redis,
posts Adaptive Card alerts to Microsoft Teams, triggers automated
remediation with a circuit breaker to stop a persistently-broken node from
being "fixed" on repeat, and ships Grafana dashboards for fleet health. See
[Roadmap](#roadmap) for what's intentionally not built yet.

Two visual walkthroughs live in `docs/`, open either directly in a browser:
- [`docs/how-it-works.html`](docs/how-it-works.html) — no jargon, what each
  part does and why, told through a hospital analogy plus a minute-by-minute
  story of a real outage. Start here if you're not building this yourself.
- [`docs/workflow.html`](docs/workflow.html) — the engineering version:
  architecture diagram, incident lifecycle sequence, circuit-breaker state
  machine, and the Redis key/threshold reference tables.

## How it works

**Detection & notification (Go, `cmd/observer`)**
1. Polls each configured Jenkins master's `/computer/api/json?depth=2` on a timer.
2. Every offline agent is fingerprinted (`SHA256(masterURL+node+reason)`) and
   checked against Redis. First occurrence always alerts; repeats are
   suppressed except at escalating occurrence counts (5, 10, 50, 100) so
   long-lived incidents still surface without spamming Teams every poll.
3. Alerts are routed to a Teams webhook by the agent's Jenkins labels
   (`team-qa`, `env-prod`, ...), falling back to a default webhook.
4. When an agent that was offline is seen online again, a "Recovered" card
   is posted automatically.
5. `/metrics` (Prometheus) and `/healthz` are exposed for basic ops visibility.

**Self-healing (Python, `remediation/`)**
6. On a brand-new offline incident, the observer enqueues a remediation job
   into Redis (unless that node's circuit breaker is already tripped).
7. A standalone worker (`remediation/worker.py`) consumes the queue and
   either dry-runs (default, safe) or actually executes the recovery
   runbook (`remediation/scripts/self_heal.sh`) via AWS SSM Run Command.
8. If the same node fails again while still inside the post-remediation
   cooldown window, the circuit breaker trips: auto-healing is disabled for
   that node and a "Requires SRE Intervention" alert is posted. Clearing a
   tripped breaker is a manual action (see below) — it does not
   self-clear, even if the node recovers on its own.

The Go and Python processes are decoupled — they only share a small Redis
key schema (a job queue + a few `circuit:*` keys), not a direct RPC.

**Observability (Prometheus + Grafana)**
9. The observer already exposes its own operational metrics at `/metrics`
   (alerts sent/suppressed, poll errors — see `internal/metrics`).
10. `cmd/mockjenkins` additionally exposes a `/prometheus/` endpoint
    (fixture-driven) standing in for a real Jenkins controller's
    [Prometheus plugin](https://plugins.jenkins.io/prometheus/), so the
    provisioned Grafana dashboard (Agent Fleet Status, Queue Backlog,
    Memory, Disk) can be demonstrated end to end without real Jenkins/host
    infrastructure. See "Running against real infrastructure" for what a
    real deployment needs instead.

## Repo layout

```
cmd/observer/         the detection/notification daemon
cmd/mockjenkins/       fixture server standing in for a real Jenkins controller (local dev only)
internal/jenkins/      Jenkins Remote Access API client
internal/dedup/        Redis-backed fingerprinting + escalation
internal/notify/       Adaptive Card builder, label router, webhook sender
internal/poller/       ticker loop: detection, notification, recovery, remediation enqueueing
internal/remediation/  Go-side of the remediation job queue + circuit breaker read
internal/metrics/      Prometheus + healthz for the observer itself
internal/promfake/     renders fixtures as Prometheus metrics (mockjenkins's /prometheus/, local dev only)
internal/config/       env + JSON config loading
remediation/           Python remediation worker (see below)
configs/               *.example.json templates + *.local.json used by docker-compose
observability/         Prometheus scrape config + Grafana datasource/dashboard provisioning
testdata/fixtures/     sample Jenkins API payloads, used by tests, mockjenkins, and promfake
```

```
remediation/
├── worker.py           entry point: BLPOPs jobs, applies the circuit breaker, executes/dry-runs
├── circuit_breaker.py  cooldown/tripped/escalated Redis key logic
├── executor.py         DryRunExecutor (default) + AWSSSMExecutor (real boto3 call)
├── notify.py           minimal Teams card poster (Remediation Triggered / Requires SRE Intervention)
├── config.py            env vars + configs/routing.json loading
└── scripts/self_heal.sh the recovery runbook run on the target host via SSM
```

## Running locally (docker-compose)

No real Jenkins, Redis, AWS, or Teams tenant required — everything is
mocked so the full pipeline, including remediation, is runnable end to end:

```bash
make up
```

This starts:
- `redis` — real Redis for dedup + circuit breaker state
- `mockjenkins` — serves `testdata/fixtures/one-offline.json` as its
  `/computer/api/json` response (one agent healthy, one offline)
- `webhook-sink` — a public echo server standing in for the Teams webhook;
  `docker compose logs -f webhook-sink` shows the Adaptive Card payloads posted
- `observer` — the detection daemon, pointed at `configs/masters.local.json`,
  `configs/routing.local.json`, and `configs/instances.local.json`
- `remediation` — the self-healing worker, running in `REMEDIATION_MODE=dry-run`
  (it only logs "would run self_heal.sh via SSM..." — it never shells out or
  touches a real host, so it's always safe to run against your own machine)
- `prometheus` — scrapes `mockjenkins:8080/prometheus/` and `observer:9090/metrics`
  every 5s (`observability/prometheus.yml`)
- `grafana` — auto-provisioned with the Prometheus datasource and the
  `jenkins-fleet` dashboard; open `http://localhost:3000` (anonymous viewer
  access is enabled for local convenience — see the compose file comment)

Watch it work:

```bash
docker compose logs -f observer remediation webhook-sink
```

Within one poll interval (10s in the local compose config) you should see:
`observer` log `remediation job enqueued` then `alert sent`; `remediation`
log `[dry-run] would run self_heal.sh...`; and `webhook-sink` receive both
an "Agent Offline" and a "Remediation Triggered (dry-run)" card.

To try a different scenario, edit the `FIXTURE_FILE` environment variable
under the `mockjenkins` service in `docker-compose.yml` (e.g.
`/testdata/fixtures/all-healthy.json` to see a "Recovered" card, or
`/testdata/fixtures/mixed.json`), then `docker compose up -d --build mockjenkins`.

**Exercising the circuit breaker**: the local compose config intentionally
sets `DEDUP_WINDOW=20s` (observer) shorter than `BREAKER_WINDOW_SECONDS=60s`
(remediation), so if the mock agent stays offline continuously, the second
"first-seen" alert (once the 20s dedup window lapses) arrives while the
Python worker's 60s cooldown from the first remediation is still active —
this is a genuine re-failure-after-remediation, so the breaker trips within
about a minute of `make up`. You'll see `Requires SRE Intervention` posted
to `webhook-sink`, and can confirm the trip directly:
```bash
docker exec jenkins-monitoring-tool-redis-1 redis-cli GET circuit:tripped:local-mock:ec2-agent-01
```
Clear it manually once you're done (mirrors what an SRE would do after a
real fix):
```bash
docker exec jenkins-monitoring-tool-redis-1 redis-cli DEL circuit:tripped:local-mock:ec2-agent-01
```

**Viewing the dashboard**: open `http://localhost:3000` → the "Jenkins Fleet
Observability" dashboard is already provisioned (no login/setup needed).
With the default `one-offline.json` fixture you should see Agent Fleet
Status red for `ec2-agent-01`, Queue Backlog Size in warning (18 > 15),
System Memory Usage red for the offline node (96.8% > 85%), and Disk
Capacity Profile red for it too (2.1 GiB < 5 GiB) — the fixture was
deliberately written to cross every threshold at once. Switch to
`all-healthy.json` (see above) to see it go green. You can also query
Prometheus directly:
```bash
curl -s 'http://localhost:9091/api/v1/query?query=jenkins_queue_size' | jq
curl -s http://localhost:9091/api/v1/targets | jq '.data.activeTargets[] | {job: .labels.job, health}'
```

`make down` tears the stack down. `make logs` tails everything.

## Running against real infrastructure

1. Copy `configs/masters.example.json` → `configs/masters.json` and fill in
   your Jenkins master URL(s) and an API token for a service account with
   read access to `/computer/api/json`.
2. Copy `configs/routing.example.json` → `configs/routing.json` and fill in
   real Teams incoming-webhook URLs (see [Microsoft's guide][teams-webhook]
   for creating one via Workflows).
3. If you want self-healing, copy `configs/instances.example.json` →
   `configs/instances.json` and map each Jenkins node's display name to its
   EC2 instance ID. Nodes absent from this file are still monitored and
   alerted on — they just never get a remediation job enqueued. All three
   files above are gitignored.
4. Point Redis at a real instance via `REDIS_ADDR` (and `REDIS_PASSWORD` if needed).
5. For real remediation, set `REMEDIATION_MODE=aws-ssm` and `AWS_REGION`,
   and give the worker's IAM identity `ssm:SendCommand` on the target
   instances plus the `AWS-RunShellScript` document — and make sure the
   target hosts have the SSM agent running (see the blueprint's "SSM
   Execution Failure" failure mode). Leave it at the default `dry-run`
   until you've reviewed `remediation/scripts/self_heal.sh` against your
   own host layout (workspace paths, service names) — it prunes docker
   volumes and restarts the agent service, which is destructive by design.
6. Build and run:
   ```bash
   make build   # produces bin via a golang container (no local Go needed)
   docker build --target observer -t jenkins-observer .
   docker build --target remediation -t jenkins-remediation .
   docker run --env-file .env -v $(pwd)/configs:/app/configs:ro jenkins-observer
   docker run --env-file .env -v $(pwd)/configs:/app/configs:ro jenkins-remediation
   ```
7. For the Grafana dashboard against real infrastructure, point Prometheus
   at your actual Jenkins controller's
   [Prometheus plugin](https://plugins.jenkins.io/prometheus/) endpoint
   (`/prometheus/` on the Jenkins root URL) instead of `mockjenkins` in
   `observability/prometheus.yml` — **and** run a host-level exporter such
   as [`node_exporter`](https://github.com/prometheus/node_exporter) on
   each Jenkins agent for the System Memory Usage and Disk Capacity Profile
   panels. Those two are not Jenkins metrics; `internal/promfake` folds
   them into the same mock endpoint purely for local-demo convenience —
   don't carry that shortcut into production. You'll also need to update
   the dashboard's `jenkins_node_mem_used_percent` /
   `jenkins_node_disk_free_bytes` queries to match whatever metric names
   your chosen exporter actually uses (e.g. `node_exporter` exposes
   `node_memory_MemAvailable_bytes` and `node_filesystem_free_bytes`, not
   those names). Also double check `jenkins_node_online_status` and
   `jenkins_executor_count` against the metric names your installed
   Prometheus plugin version actually exports — plugin versions have
   changed metric naming before, and this repo's queries were written
   against the blueprint's documented names, not verified against a live
   plugin instance.

See `.env.example` for the full list of environment variables.

[teams-webhook]: https://learn.microsoft.com/en-us/microsoftteams/platform/webhooks-and-connectors/how-to/add-incoming-webhook

## Testing

```bash
make vet
make test          # runs both test-go and test-python
```

Both the Go tests (`golang` container) and Python tests (`python:3.12-slim`
container, `pytest` + `fakeredis`) run inside Docker, so no local Go or
Python toolchain is required, and no running Redis/services are needed —
`miniredis` (Go) and `fakeredis` (Python) provide in-process Redis for the
dedup and circuit-breaker tests.

## Roadmap

- **Phase 1 — Detection & notification**: done. Poll → dedup → Teams alert.
- **Phase 2 — Self-healing**: done. Python remediation worker triggered via
  AWS SSM (dry-run by default), circuit breaker with manual-clear
  escalation, automatic recovery notifications.
- **Phase 3 — Observability dashboards**: done. Prometheus scraping +
  Grafana dashboard (`observability/`) covering Agent Fleet Status, Queue
  Backlog Size, System Memory Usage, and Disk Capacity Profile, per the
  blueprint's panel/threshold spec, plus a bonus Observer Health panel.

Two things remain genuinely unverified against real infrastructure, since
none has been available at any point in this build:

- `AWSSSMExecutor` (real AWS mode, Phase 2) is written and unit-tested
  against a mocked `boto3` client, but has not been exercised against a
  live AWS account. Treat `REMEDIATION_MODE=aws-ssm` as unverified until
  you've run it against a real instance yourself.
- The Grafana dashboard's Jenkins-metric queries (`jenkins_node_online_status`,
  `jenkins_executor_count`, `jenkins_queue_size`) were written against the
  blueprint's documented metric names and validated against `internal/promfake`'s
  mock output — not against a real Jenkins Prometheus plugin install, whose
  exact metric names may differ by version. The System Memory Usage and
  Disk Capacity Profile panels additionally assume a host exporter
  (`node_exporter` or similar) that isn't part of this repo at all; see
  "Running against real infrastructure" above.
