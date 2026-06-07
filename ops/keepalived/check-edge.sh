#!/bin/bash
# Keepalived vrrp_script for the listener VIP.
#
# Exit 0 -> healthy (keep VIP). Non-zero -> unhealthy (drop VIP priority).
# The script must complete inside the vrrp_script timeout (default 5s).
#
# It probes the LOCAL edge-encoder /healthz; the encoder's own /healthz logic
# is byte-flow-aware (see Section 7 of the HA design doc) so a "process alive
# but not producing bytes" condition fails the probe and forces VIP failover
# to the peer node.
#
# Env (set in keepalived.conf via `vrrp_script ... { ... }`):
#   EDGE_HEALTH_URL  default http://127.0.0.1:8001/healthz
set -eu
URL="${EDGE_HEALTH_URL:-http://127.0.0.1:8001/healthz}"
# -f fails on HTTP >= 400; -s silent; --max-time bounds the probe.
curl -fs --max-time 2 "$URL" >/dev/null
