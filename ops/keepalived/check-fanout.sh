#!/bin/bash
# Keepalived vrrp_script for the DJ-facing VIP.
#
# Exit 0 -> healthy (keep VIP). Non-zero -> unhealthy (drop VIP priority).
#
# Probes the LOCAL grimnir-fanout /healthz on FANOUT_HTTP_PORT (default 8003).
# /healthz returns 200 once every protocol listener has bound; it returns 503
# if a per-session byte-flow watchdog trips. Either failure mode floats the
# DJ VIP to the peer fan-out within ~3s.
#
# Env (set in keepalived.conf via `vrrp_script ... { ... }`):
#   FANOUT_HEALTH_URL  default http://127.0.0.1:8003/healthz
set -eu
URL="${FANOUT_HEALTH_URL:-http://127.0.0.1:8003/healthz}"
curl -fs --max-time 2 "$URL" >/dev/null
