#!/bin/bash
# Self-healing remediation runbook for a Jenkins agent host.
# Executed via AWS SSM Run Command (AWS-RunShellScript) by remediation/executor.py.
set -euo pipefail

LOG_FILE="/var/log/jenkins-self-heal.log"
exec > >(tee -a "$LOG_FILE") 2>&1

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Starting automated remediation..."

# 1. Evaluate storage constraints
echo "Checking storage space..."
df -h /

# Clear dangling docker containers, volumes, and image layers
if command -v docker &> /dev/null; then
  echo "Pruning dangling docker volumes and build caches..."
  docker system prune -af --volumes || true
fi

# Clean stale workspace artifacts
WORKSPACE_DIR="/home/jenkins/workspace"
if [ -d "$WORKSPACE_DIR" ]; then
  echo "Pruning temporary log files and test artifacts..."
  find "$WORKSPACE_DIR" -type d -name "target" -mtime +3 -exec rm -rf {} + || true
  find "$WORKSPACE_DIR" -type d -name "node_modules" -mtime +3 -exec rm -rf {} + || true
  find "$WORKSPACE_DIR" -type f -name "*.log" -mtime +5 -delete || true
fi

# Clear global package management caches
rm -rf /home/jenkins/.gradle/caches/* || true
rm -rf /home/jenkins/.m2/repository/.cache/* || true

# 2. Check and recover the JVM agent daemon process
echo "Checking Java executor processes..."
if systemctl list-units --type=service | grep -q "jenkins-agent.service"; then
  echo "Restarting agent service via systemctl..."
  systemctl restart jenkins-agent.service
else
  echo "Systemd service not found. Searching for running Java agents..."
  pkill -u jenkins -f "agent.jar" || true
  sleep 3
  nohup java -Xmx2g -Xms512m \
    -XX:+UseG1GC \
    -XX:MaxGCPauseMillis=200 \
    -jar /home/jenkins/agent.jar \
    -jnlpUrl "${JENKINS_AGENT_JNLP_URL:?JENKINS_AGENT_JNLP_URL not set}" \
    -secret @/home/jenkins/secret-file > /var/log/jenkins/agent.log 2>&1 &
fi

echo "[$(date '+%Y-%m-%d %H:%M:%S')] Remediation workflow complete."
df -h /
