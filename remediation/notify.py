"""Minimal Teams Adaptive Card poster for the remediation worker.

Kept independent from the Go observer's card builder
(internal/notify/card.go) by design: per the blueprint, the remediation
layer should be adjustable by infra teams without recompiling/redeploying
the main daemon.
"""
from __future__ import annotations

import datetime as dt
import logging

import requests

logger = logging.getLogger(__name__)


def _status_url(job: dict) -> str:
    return job["master_url"].rstrip("/") + "/computer/" + job["node"]


def _card(title: str, facts: list[dict], status_url: str) -> dict:
    return {
        "type": "message",
        "attachments": [
            {
                "contentType": "application/vnd.microsoft.card.adaptive",
                "content": {
                    "$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
                    "type": "AdaptiveCard",
                    "version": "1.5",
                    "body": [
                        {"type": "TextBlock", "text": title, "weight": "Bolder", "size": "Large", "wrap": True},
                        {"type": "FactSet", "facts": facts},
                    ],
                    "actions": [
                        {"type": "Action.OpenUrl", "title": "View Agent Status", "url": status_url},
                    ],
                },
            }
        ],
    }


def post(webhook_url: str, title: str, facts: list[dict], status_url: str, timeout: float = 10.0) -> None:
    payload = _card(title, facts, status_url)
    resp = requests.post(webhook_url, json=payload, timeout=timeout)
    resp.raise_for_status()


def remediation_triggered(webhook_url: str, job: dict, mode: str) -> None:
    suffix = " (dry-run)" if mode == "dry-run" else ""
    title = f"Remediation Triggered{suffix}: {job['node']}"
    facts = [
        {"title": "Master Controller", "value": job["master"]},
        {"title": "Agent Name", "value": job["node"]},
        {"title": "Instance ID", "value": job.get("instance_id") or "unknown"},
        {"title": "Offline Cause", "value": job.get("reason") or "unknown"},
        {"title": "Triggered At", "value": dt.datetime.now(dt.timezone.utc).isoformat()},
    ]
    post(webhook_url, title, facts, _status_url(job))


def circuit_tripped(webhook_url: str, job: dict) -> None:
    title = f"Requires SRE Intervention: {job['node']}"
    facts = [
        {"title": "Master Controller", "value": job["master"]},
        {"title": "Agent Name", "value": job["node"]},
        {"title": "Reason", "value": "Re-failed after automated remediation; auto-healing disabled for this node"},
        {"title": "Escalated At", "value": dt.datetime.now(dt.timezone.utc).isoformat()},
    ]
    post(webhook_url, title, facts, _status_url(job))
