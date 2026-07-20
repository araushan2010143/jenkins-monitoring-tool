"""Environment + JSON config loading for the remediation worker.

Reads the same configs/routing.json the Go observer uses (internal/config)
so both processes route to the same Teams webhooks, and the same REDIS_ADDR
family of env vars so both processes share the same Redis.
"""
from __future__ import annotations

import json
import os
from dataclasses import dataclass
from pathlib import Path

import redis


@dataclass(frozen=True)
class AppConfig:
    redis_addr: str
    redis_password: str
    redis_db: int
    routing_file: str
    remediation_mode: str  # "dry-run" or "aws-ssm"
    breaker_window_seconds: int
    aws_region: str


def load_from_env() -> AppConfig:
    return AppConfig(
        redis_addr=os.environ.get("REDIS_ADDR", "localhost:6379"),
        redis_password=os.environ.get("REDIS_PASSWORD", ""),
        redis_db=int(os.environ.get("REDIS_DB", "0")),
        routing_file=os.environ.get("ROUTING_FILE", "configs/routing.json"),
        remediation_mode=os.environ.get("REMEDIATION_MODE", "dry-run"),
        breaker_window_seconds=int(os.environ.get("BREAKER_WINDOW_SECONDS", "1800")),
        aws_region=os.environ.get("AWS_REGION", "us-east-1"),
    )


def build_redis_client(cfg: AppConfig) -> "redis.Redis":
    auth = f":{cfg.redis_password}@" if cfg.redis_password else ""
    url = f"redis://{auth}{cfg.redis_addr}/{cfg.redis_db}"
    return redis.Redis.from_url(url, decode_responses=True)


def load_routing(path: str) -> dict:
    data = json.loads(Path(path).read_text())
    if not data.get("default"):
        raise ValueError(f"routing file {path!r} must set a default webhook")
    return data


def resolve_webhook(routing: dict, labels: list[str] | None) -> str:
    """Mirrors internal/notify.Router.Resolve: first label match wins,
    falling back to the routing table's default webhook."""
    routes = routing.get("routes", {})
    for label in labels or []:
        webhook = routes.get(label)
        if webhook:
            return webhook
    return routing["default"]
