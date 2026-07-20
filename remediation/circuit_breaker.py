"""Circuit breaker state, shared via Redis with the Go observer
(internal/remediation reads circuit:tripped:* read-only). Schema:

  circuit:cooldown:{master}:{node}   TTL=breaker_window  - set after a remediation attempt
  circuit:tripped:{master}:{node}    no TTL               - re-failed within cooldown; manual clear required
  circuit:escalated:{master}:{node}  TTL=1h               - de-dupes the "Requires SRE Intervention" alert
"""
from __future__ import annotations

import enum
import time


class Decision(enum.Enum):
    REMEDIATE = "remediate"
    SKIP_TRIPPED = "skip_tripped"
    TRIP_NOW = "trip_now"


def cooldown_key(master: str, node: str) -> str:
    return f"circuit:cooldown:{master}:{node}"


def tripped_key(master: str, node: str) -> str:
    return f"circuit:tripped:{master}:{node}"


def escalated_key(master: str, node: str) -> str:
    return f"circuit:escalated:{master}:{node}"


def should_remediate(rdb, master: str, node: str) -> Decision:
    """Decides what to do with a new offline incident for (master, node):

    - SKIP_TRIPPED: the breaker is already tripped, do nothing (worker.py
      will post the "Requires SRE Intervention" alert, rate-limited).
    - TRIP_NOW: this node was remediated recently and has failed again —
      trip the breaker now instead of remediating again.
    - REMEDIATE: no active cooldown or trip, go ahead and remediate.
    """
    if rdb.exists(tripped_key(master, node)):
        return Decision.SKIP_TRIPPED
    if rdb.exists(cooldown_key(master, node)):
        rdb.set(tripped_key(master, node), str(time.time()))
        rdb.delete(cooldown_key(master, node))
        return Decision.TRIP_NOW
    return Decision.REMEDIATE


def mark_remediated(rdb, master: str, node: str, window_seconds: int) -> None:
    """Starts the cooldown window after a remediation attempt. A re-failure
    while this key is live means the fix didn't hold."""
    rdb.set(cooldown_key(master, node), str(time.time()), ex=window_seconds)


def should_escalate(rdb, master: str, node: str) -> bool:
    """Returns True only the first time a tripped node would escalate
    within the de-dupe window, so repeated jobs for an already-tripped node
    don't repost "Requires SRE Intervention" on every poll cycle."""
    return bool(rdb.set(escalated_key(master, node), "1", ex=3600, nx=True))
