#!/bin/bash
# keepalived state-transition notify script.
#
# Configure in keepalived.conf:
#   notify_master "/etc/keepalived/notify.sh MASTER"
#   notify_backup "/etc/keepalived/notify.sh BACKUP"
#   notify_fault  "/etc/keepalived/notify.sh FAULT"
#
# keepalived passes its own positional args appended to whatever you put on the
# command line. The arg order used here is:
#   $1 = TYPE  (GROUP | INSTANCE)
#   $2 = NAME  (the VIP application name, e.g., "listener")
#   $3 = STATE (MASTER | BACKUP | FAULT)
#
# Output: writes the per-node state into a Redis hash keyed by VIP name.
# The grimnirradio control plane's vrrphealth poller reads this hash and
# updates the grimnir_vrrp_holder_count gauge.
#
# Required environment:
#   REDIS_HOST     - Redis hostname/IP
#   REDIS_PASSWORD - Redis AUTH password (empty if no auth)
set -eu

NODE="$(hostname -s)"
VIP="$2"
STATE="$(echo "$3" | tr '[:upper:]' '[:lower:]')"

redis-cli -h "${REDIS_HOST}" -a "${REDIS_PASSWORD}" --no-auth-warning \
  HSET "grimnir:vrrp:${VIP}" "${NODE}" "${STATE}"
