# keepalived install (listener + DJ VIPs)

This runbook installs the VRRP layer on both HA nodes so the listener &
DJ endpoints float between them on local-component failure. Do it once
per region during initial bring-up; re-run only if you replace a node.

## Prerequisites

- Two HA nodes in the same L2 segment (a flat /24 is fine; the VIP must
  be ARP-reachable from both nodes).
- Stack already running on both nodes (control plane + media engine +
  edge encoder + grimnir-fanout), each reachable on its own management IP.
- A free IP in the lab range for each VIP. This region uses:

  | VIP         | IP            | Backed by                                     |
  |-------------|---------------|-----------------------------------------------|
  | listener    | `<listener-vip>` | edge-encoder HTTP/ICY/HLS on port 8001        |
  | dj          | `<dj-vip>` | grimnir-fanout (Harbor 8000, RTP 5006, SRT 1935, WebRTC 8004) |

  Node IPs in this region: `<node-a-ip>` (node A), `<node-b-ip>` (node B).

- Redis reachable from both nodes (the `notify.sh` writes there; the
  control plane's `vrrphealth` poller reads from there). Export
  `REDIS_HOST` & `REDIS_PASSWORD` in the keepalived environment.

## Step 1: apt install on both nodes

```bash
sudo apt-get update
sudo apt-get install -y keepalived curl
```

Confirm: `keepalived --version` shows >= 2.2.

## Step 2: copy configs & scripts

From a checkout of this repo, on each node:

```bash
sudo install -m 0644 ops/keepalived/keepalived-listener.conf  /etc/keepalived/conf.d/listener.conf
sudo install -m 0644 ops/keepalived/keepalived-dj.conf        /etc/keepalived/conf.d/dj.conf
sudo install -m 0755 ops/keepalived/check-edge.sh             /etc/keepalived/check-edge.sh
sudo install -m 0755 ops/keepalived/check-fanout.sh           /etc/keepalived/check-fanout.sh
sudo install -m 0755 ops/keepalived/notify.sh                 /etc/keepalived/notify.sh
```

If `/etc/keepalived/keepalived.conf` exists from the package install, add
an include line at the top:

```
include /etc/keepalived/conf.d/*.conf
```

## Step 3: edit the per-node bits

The shipped configs are MASTER-priority-110 templates. On NODE B, edit
both files & set:

- `state BACKUP`
- `priority 100`
- `unicast_src_ip <node-b-ip>`
- `unicast_peer { <node-a-ip> }`

On NODE A, leave the shipped values as-is (or confirm they read
`MASTER` / `110` / `<node-a-ip>` / peer `<node-b-ip>`).

Verify the `virtual_router_id` lines DIFFER between the two instances on
the same node: `51` for listener, `52` for DJ. They MUST match between
nodes for the same instance.

## Step 4: change the VRRP passwords

Both config files ship with placeholder `auth_pass` values. Rotate
both per region; commit the production values to your secret store, not
to this repo. Keepalived silently truncates `auth_pass` to 8 chars.

## Step 5: configure the notify.sh environment

`notify.sh` shells out to `redis-cli`; it reads `REDIS_HOST` &
`REDIS_PASSWORD` from its environment. Put them in a drop-in:

```bash
sudo tee /etc/systemd/system/keepalived.service.d/redis-env.conf <<'EOF'
[Service]
Environment=REDIS_HOST=<redis-host>
Environment=REDIS_PASSWORD=__paste_from_secrets__
EOF
sudo systemctl daemon-reload
```

## Step 6: enable & start keepalived on BOTH nodes

```bash
sudo systemctl enable --now keepalived
sudo systemctl status keepalived
```

`status` should show "Active: active (running)" within a second.

## Step 7: verify VRRP is on the wire

On either node:

```bash
sudo tcpdump -i eth0 -n vrrp -c 10
```

You should see VRRPv2 advertisements every ~1s. The MASTER's IP is the
source; the destination is the peer's `unicast_peer` IP. Two flows
appear, one per instance. Silence here means the peers can't see each
other; check L2 connectivity & firewall before going further.

## Step 8: verify VIP assignment

On the node that should be MASTER (per the priority you set):

```bash
ip addr show eth0 | grep -E '10\.10\.0\.19[01]'
```

You should see `<listener-vip>` & `<dj-vip>` on that node's `eth0`. On
the BACKUP node the same `ip addr` command returns nothing for those
addresses; the BACKUP doesn't hold the VIPs in steady state.

## Step 9: verify Redis hash population

`notify.sh` fires on every VRRP state transition. To force a write
without disrupting traffic, restart keepalived on the BACKUP:

```bash
sudo systemctl restart keepalived
redis-cli -h "$REDIS_HOST" -a "$REDIS_PASSWORD" HGETALL grimnir:vrrp:listener
redis-cli -h "$REDIS_HOST" -a "$REDIS_PASSWORD" HGETALL grimnir:vrrp:dj
```

You should see both nodes listed, e.g.:

```
1) "node-a"
2) "master"
3) "node-b"
4) "backup"
```

## Step 10: wire the control plane's poller

On EACH node, add to the grimnir control-plane service env:

```
GRIMNIR_VRRP_VIPS=listener,dj
```

Restart the control plane. Confirm the gauge:

```bash
curl -s http://127.0.0.1:9000/metrics | grep grimnir_vrrp_holder_count
# grimnir_vrrp_holder_count{vip="listener"} 1
# grimnir_vrrp_holder_count{vip="dj"} 1
```

A value of `0` means no node is currently MASTER (page); `2` means
split-brain (page). Wire both into your alertmanager rules.

## Step 11: failover drill

On the MASTER node, simulate a local edge-encoder outage:

```bash
sudo systemctl stop grimnir-edge-encoder
```

Within ~3s `ip addr show eth0` on the BACKUP should show `<listener-vip>`.
Bring the edge encoder back up, restart keepalived on the original
MASTER if needed, & confirm the VIP returns to its preferred holder
(or stays put, per your preference; keepalived respects priority
without preemption only if you set `nopreempt`).

Repeat for the DJ VIP by stopping `grimnir-fanout` on the holder.

## Rollback

If keepalived itself misbehaves & you need traffic restored fast:

```bash
sudo systemctl stop keepalived
sudo ip addr del <listener-vip>/24 dev eth0   # one node only, ONLY if leftover
sudo ip addr del <dj-vip>/24 dev eth0
```

Then revert DNS / nginx upstream back to the per-node management IPs
until you fix the keepalived config.
