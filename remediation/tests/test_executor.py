import subprocess
from unittest.mock import MagicMock, patch

from remediation import executor


def test_dry_run_executor_never_shells_out():
    with patch.object(subprocess, "run") as run_mock, patch.object(subprocess, "Popen") as popen_mock:
        result = executor.DryRunExecutor().execute("i-fake123", "ec2-agent-01")

    assert result is True
    run_mock.assert_not_called()
    popen_mock.assert_not_called()


def test_build_executor_dry_run_default():
    assert isinstance(executor.build_executor("dry-run", "us-east-1"), executor.DryRunExecutor)


def test_build_executor_rejects_unknown_mode():
    try:
        executor.build_executor("bogus-mode", "us-east-1")
        assert False, "expected ValueError"
    except ValueError:
        pass


def test_aws_ssm_executor_calls_send_command_with_expected_shape():
    fake_client = MagicMock()
    fake_client.send_command.return_value = {"Command": {"CommandId": "cmd-123"}}

    with patch("boto3.client", return_value=fake_client) as client_factory:
        result = executor.AWSSSMExecutor(region="us-west-2").execute("i-real456", "ec2-agent-02")

    assert result is True
    client_factory.assert_called_once_with("ssm", region_name="us-west-2")

    args, kwargs = fake_client.send_command.call_args
    assert kwargs["InstanceIds"] == ["i-real456"]
    assert kwargs["DocumentName"] == "AWS-RunShellScript"
    assert "commands" in kwargs["Parameters"]
    assert "self-heal" in kwargs["Parameters"]["commands"][0].lower() or "remediation" in kwargs["Parameters"]["commands"][0].lower()
