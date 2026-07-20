import fakeredis
import pytest

from remediation import circuit_breaker as cb


@pytest.fixture
def rdb():
    return fakeredis.FakeRedis(decode_responses=True)


def test_should_remediate_on_first_incident(rdb):
    decision = cb.should_remediate(rdb, "master-1", "node-1")
    assert decision is cb.Decision.REMEDIATE


def test_remediated_node_is_skipped_only_after_trip(rdb):
    cb.mark_remediated(rdb, "master-1", "node-1", window_seconds=1800)

    # A fresh incident while still in cooldown means the fix didn't hold:
    # this call must trip the breaker, not remediate again.
    decision = cb.should_remediate(rdb, "master-1", "node-1")
    assert decision is cb.Decision.TRIP_NOW
    assert rdb.exists(cb.tripped_key("master-1", "node-1"))
    assert not rdb.exists(cb.cooldown_key("master-1", "node-1"))


def test_tripped_breaker_is_sticky(rdb):
    cb.mark_remediated(rdb, "master-1", "node-1", window_seconds=1800)
    cb.should_remediate(rdb, "master-1", "node-1")  # trips it

    decision = cb.should_remediate(rdb, "master-1", "node-1")
    assert decision is cb.Decision.SKIP_TRIPPED


def test_distinct_nodes_are_independent(rdb):
    cb.mark_remediated(rdb, "master-1", "node-1", window_seconds=1800)

    decision = cb.should_remediate(rdb, "master-1", "node-2")
    assert decision is cb.Decision.REMEDIATE


def test_should_escalate_only_once_within_window(rdb):
    first = cb.should_escalate(rdb, "master-1", "node-1")
    second = cb.should_escalate(rdb, "master-1", "node-1")
    assert first is True
    assert second is False
