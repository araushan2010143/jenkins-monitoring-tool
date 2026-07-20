"""Remediation executors.

DryRunExecutor is the default and only executor exercised against
local/mocked infrastructure: it never shells out or calls AWS, so it's safe
to run against the developer's own machine. AWSSSMExecutor performs the
real AWS Systems Manager Run Command call; it's written per the blueprint
but only verified with a mocked boto3 client until real AWS credentials are
available (see README "Running against real infrastructure").
"""
from __future__ import annotations

import logging
from abc import ABC, abstractmethod
from pathlib import Path

logger = logging.getLogger(__name__)

SELF_HEAL_SCRIPT = Path(__file__).parent / "scripts" / "self_heal.sh"


class RemediationExecutor(ABC):
    @abstractmethod
    def execute(self, instance_id: str, node: str) -> bool:
        """Runs the self-heal runbook against instance_id. Returns True if
        the remediation was (at least) successfully submitted."""


class DryRunExecutor(RemediationExecutor):
    def execute(self, instance_id: str, node: str) -> bool:
        logger.info(
            "[dry-run] would run %s via SSM on instance=%s (node=%s)",
            SELF_HEAL_SCRIPT.name, instance_id, node,
        )
        return True


class AWSSSMExecutor(RemediationExecutor):
    def __init__(self, region: str):
        import boto3  # imported lazily: dry-run mode must never require boto3/AWS creds

        self._client = boto3.client("ssm", region_name=region)

    def execute(self, instance_id: str, node: str) -> bool:
        script = SELF_HEAL_SCRIPT.read_text()
        response = self._client.send_command(
            InstanceIds=[instance_id],
            DocumentName="AWS-RunShellScript",
            Parameters={"commands": [script]},
        )
        command_id = response["Command"]["CommandId"]
        logger.info(
            "submitted SSM command %s for instance=%s (node=%s)",
            command_id, instance_id, node,
        )
        return True


def build_executor(mode: str, region: str) -> RemediationExecutor:
    if mode == "dry-run":
        return DryRunExecutor()
    if mode == "aws-ssm":
        return AWSSSMExecutor(region)
    raise ValueError(f"unknown REMEDIATION_MODE {mode!r}, expected 'dry-run' or 'aws-ssm'")
