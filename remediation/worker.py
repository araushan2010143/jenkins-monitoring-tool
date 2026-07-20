"""Remediation worker entry point: BLPOPs jobs the Go observer enqueues onto
`remediation:jobs`, applies circuit breaker logic, and executes (or
dry-runs) the self-heal runbook via AWS SSM.
"""
from __future__ import annotations

import json
import logging
import sys

from . import circuit_breaker, config, executor, notify

logger = logging.getLogger("remediation.worker")

JOBS_KEY = "remediation:jobs"


def handle_job(rdb, routing: dict, exec_: executor.RemediationExecutor, job: dict, cfg: config.AppConfig) -> None:
    master, node = job["master"], job["node"]
    webhook = config.resolve_webhook(routing, job.get("labels"))

    decision = circuit_breaker.should_remediate(rdb, master, node)

    if decision is circuit_breaker.Decision.SKIP_TRIPPED:
        logger.warning("circuit tripped, skipping remediation for %s/%s", master, node)
        if circuit_breaker.should_escalate(rdb, master, node):
            notify.circuit_tripped(webhook, job)
        return

    if decision is circuit_breaker.Decision.TRIP_NOW:
        logger.error("%s/%s re-failed within the cooldown window, tripping circuit breaker", master, node)
        if circuit_breaker.should_escalate(rdb, master, node):
            notify.circuit_tripped(webhook, job)
        return

    instance_id = job.get("instance_id")
    if not instance_id:
        logger.warning("job for %s/%s has no instance_id, skipping remediation", master, node)
        return

    if not exec_.execute(instance_id, node):
        logger.error("remediation execution failed for %s/%s", master, node)
        return

    circuit_breaker.mark_remediated(rdb, master, node, cfg.breaker_window_seconds)
    notify.remediation_triggered(webhook, job, cfg.remediation_mode)


def run(cfg: config.AppConfig | None = None) -> None:
    cfg = cfg or config.load_from_env()
    rdb = config.build_redis_client(cfg)
    routing = config.load_routing(cfg.routing_file)
    exec_ = executor.build_executor(cfg.remediation_mode, cfg.aws_region)

    logger.info(
        "remediation worker starting mode=%s breaker_window=%ss",
        cfg.remediation_mode, cfg.breaker_window_seconds,
    )

    while True:
        item = rdb.blpop(JOBS_KEY, timeout=5)
        if item is None:
            continue
        _, raw = item

        try:
            job = json.loads(raw)
        except json.JSONDecodeError:
            logger.error("failed to decode remediation job: %r", raw)
            continue

        try:
            handle_job(rdb, routing, exec_, job, cfg)
        except Exception:  # noqa: BLE001 - a bad job must not kill the worker loop
            logger.exception("unhandled error processing job for %s/%s", job.get("master"), job.get("node"))


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s", stream=sys.stdout)
    run()
