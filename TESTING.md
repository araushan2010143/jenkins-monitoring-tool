# Testing this project

Two layers: automated tests that prove the logic is correct, and a live
run against the mocked stack that proves the whole pipeline actually
reacts to real events. Do both — passing tests don't prove the pieces
are wired together correctly, and a live run alone doesn't prove the
edge cases (escalation thresholds, circuit breaker transitions) are handled.

## 1. Automated tests (~30s, no Docker services needed)

```bash
make vet
make test          # runs both test-go and test-python
```

- **Go** (32 tests): dedup fingerprinting/escalation, Jenkins API parsing,
  Adaptive Card formatting, label routing, the poller's enqueue/circuit-breaker/
  recovery logic, and the Prometheus metric renderer — all against
  `miniredis` and `httptest`, no real Redis or Jenkins required.
- **Python** (13 tests): circuit breaker state transitions, the dry-run
  executor (asserted to never shell out), and the worker's job-handling
  logic — against `fakeredis`, no real Redis required.

Both run inside Docker containers (`golang:1.25`, `python:3.12-slim`), so
no local Go or Python toolchain is needed either.

## 2. Live smoke test (~30s, proves the pipeline actually runs)

```bash
make up
docker compose logs -f observer remediation webhook-sink
```

Within the first poll cycle (10s in the local compose config) you should
see, in this order:

1. `observer` logs `remediation job enqueued` then `alert sent`
2. `remediation` logs `[dry-run] would run self_heal.sh via SSM...`
3. `webhook-sink` receives two POSTs — **"Agent Offline"**, then
   **"Remediation Triggered (dry-run)"**

Confirm the Teams-shaped payloads directly:

```bash
docker compose logs webhook-sink --no-log-prefix | grep -o '"text": "[^"]*"'
```

## 3. Prove it reacts to change (not just replaying a fixed script)

Edit `docker-compose.yml` — under the `mockjenkins` service, change:

```yaml
FIXTURE_FILE: /testdata/fixtures/one-offline.json
```
to
```yaml
FIXTURE_FILE: /testdata/fixtures/all-healthy.json
```

Then:

```bash
docker compose up -d mockjenkins
docker compose logs observer --no-log-prefix | tail -5
```

Within ~10–20s you should see `recovery alert sent`, and a
**"Recovered"** card in `webhook-sink`'s logs. Set `FIXTURE_FILE` back to
`one-offline.json` and re-run `docker compose up -d mockjenkins` when done
— that's the repo's default state.

Other fixtures to try: `testdata/fixtures/mixed.json` (multiple agents,
mixed states).

## 4. Exercise the circuit breaker

The local compose config sets `DEDUP_WINDOW=20s` (observer) shorter than
`BREAKER_WINDOW_SECONDS=60s` (remediation) on purpose — this creates a
window where a fresh "first-seen" alert can land while the remediation
cooldown is still active, so a genuine re-failure-after-fix is
reproducible in about a minute instead of the 30-minute production default.

Leave the fixture at `one-offline.json` (the default) and wait ~30–60s
after `make up`:

```bash
docker compose logs remediation --no-log-prefix
# expect: "re-failed within the cooldown window, tripping circuit breaker"

docker exec jenkins-monitoring-tool-redis-1 redis-cli \
  GET circuit:tripped:local-mock:ec2-agent-01
```

You should also see a **"Requires SRE Intervention"** card in
`webhook-sink`, and `observer`'s logs should show
`circuit breaker tripped, skipping remediation enqueue` on every poll
after that — proof it stops trying, rather than looping forever.

Clear it manually once you're done, mirroring what an SRE would do after
a real fix:

```bash
docker exec jenkins-monitoring-tool-redis-1 redis-cli \
  DEL circuit:tripped:local-mock:ec2-agent-01
```

## 5. Look at it, not just the logs

- **Grafana** — http://localhost:3000 → Dashboards → "Jenkins Fleet
  Observability". Panels should match whatever fixture is currently
  loaded (red/amber/green per the thresholds documented in
  [`docs/workflow.html`](docs/workflow.html)).
- **Prometheus** — http://localhost:9091 → Graph → try
  `jenkins_node_online_status` or `jenkins_queue_size`.

## 6. Shut down

```bash
make down
```

Tears down all 7 containers and their volumes. `docker-compose.yml`
itself is left as-is — remember to reset `FIXTURE_FILE` back to
`one-offline.json` first if you changed it during testing (step 3), so
the next `make up` starts from the repo's default state.

## What this does and doesn't prove

This whole checklist runs against mocks: `cmd/mockjenkins` instead of a
real Jenkins controller, `DryRunExecutor` instead of real AWS SSM calls,
`webhook-sink` instead of a real Teams channel. That's deliberate — it's
what makes the pipeline's *logic* fully testable without needing real
infrastructure or risking a real host. It does **not** prove the real
AWS SSM path (`REMEDIATION_MODE=aws-ssm`) works against a live account,
or that the Grafana queries match a real Jenkins Prometheus plugin's
actual metric names — see the README's "Running against real
infrastructure" section for what's still unverified there.
