import fakeredis

from remediation import circuit_breaker as cb
from remediation import config, worker

ROUTING = {"default": "https://default-webhook", "routes": {"team-qa": "https://qa-webhook"}}


class FakeExecutor:
    def __init__(self):
        self.calls = []

    def execute(self, instance_id, node):
        self.calls.append((instance_id, node))
        return True


def _cfg(mode="dry-run", window=1800):
    return config.AppConfig(
        redis_addr="localhost:6379",
        redis_password="",
        redis_db=0,
        routing_file="configs/routing.json",
        remediation_mode=mode,
        breaker_window_seconds=window,
        aws_region="us-east-1",
    )


def _job(**overrides):
    job = {
        "master": "m1",
        "master_url": "https://jenkins.example.com",
        "node": "ec2-agent-01",
        "instance_id": "i-123",
        "reason": "oom",
        "labels": ["team-qa"],
    }
    job.update(overrides)
    return job


def test_handle_job_remediates_and_sets_cooldown(monkeypatch):
    rdb = fakeredis.FakeRedis(decode_responses=True)
    exec_ = FakeExecutor()
    posted = []
    monkeypatch.setattr(
        "remediation.notify.post",
        lambda url, title, facts, status_url, timeout=10.0: posted.append((url, title)),
    )

    worker.handle_job(rdb, ROUTING, exec_, _job(), _cfg())

    assert exec_.calls == [("i-123", "ec2-agent-01")]
    assert rdb.exists(cb.cooldown_key("m1", "ec2-agent-01"))
    assert posted and posted[0][0] == "https://qa-webhook"
    assert "Remediation Triggered" in posted[0][1]


def test_handle_job_trips_breaker_on_repeat_failure(monkeypatch):
    rdb = fakeredis.FakeRedis(decode_responses=True)
    exec_ = FakeExecutor()
    posted = []
    monkeypatch.setattr(
        "remediation.notify.post",
        lambda url, title, facts, status_url, timeout=10.0: posted.append((url, title)),
    )
    cb.mark_remediated(rdb, "m1", "ec2-agent-01", window_seconds=1800)

    worker.handle_job(rdb, ROUTING, exec_, _job(), _cfg())

    assert exec_.calls == []  # must not remediate again
    assert rdb.exists(cb.tripped_key("m1", "ec2-agent-01"))
    assert posted and "Requires SRE Intervention" in posted[0][1]


def test_handle_job_skips_when_already_tripped(monkeypatch):
    rdb = fakeredis.FakeRedis(decode_responses=True)
    exec_ = FakeExecutor()
    posted = []
    monkeypatch.setattr(
        "remediation.notify.post",
        lambda url, title, facts, status_url, timeout=10.0: posted.append((url, title)),
    )
    rdb.set(cb.tripped_key("m1", "ec2-agent-01"), "1")

    worker.handle_job(rdb, ROUTING, exec_, _job(), _cfg())

    assert exec_.calls == []
    assert posted and "Requires SRE Intervention" in posted[0][1]


def test_handle_job_skips_without_instance_id(monkeypatch):
    rdb = fakeredis.FakeRedis(decode_responses=True)
    exec_ = FakeExecutor()

    def fail_if_called(*_args, **_kwargs):
        raise AssertionError("should not notify when instance_id is missing")

    monkeypatch.setattr("remediation.notify.post", fail_if_called)

    worker.handle_job(rdb, ROUTING, exec_, _job(instance_id=""), _cfg())

    assert exec_.calls == []
