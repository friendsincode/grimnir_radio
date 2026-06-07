# fanout-down (live DJ ingest unreachable)

## Symptom

A DJ reports their stream client can't connect, or alerting fires on
`grimnir_fanout_up == 0` for both fan-out instances. The scheduled
automation keeps playing because engines still produce PCM from their
playout pipelines; only live shows are affected.

## What's actually broken

`grimnir-fanout` is the binary that accepts the DJ connection over Harbor /
RTP / SRT / WebRTC & duplicates it to every engine. Two instances run, one
per node, both behind the VIP. If both are down, no live show can start &
any in-progress live show goes silent the moment the DJ's source client
times out & disconnects.

The engines themselves keep running. So does scheduled playout. So does
the edge encoder. Listeners hear the automation block that was queued
behind the live show; they don't hear silence unless the live show was the
ONLY thing scheduled for that hour.

## First check: is it actually both?

```bash
# From any monitoring host:
curl -sf http://node-a.internal:8003/healthz && echo "A up"
curl -sf http://node-b.internal:8003/healthz && echo "B up"
```

If one is up, VRRP should already have floated the VIP. Tell the DJ to
reconnect. The auth cache is replicated through Redis, so the DJ's token
lookup is warm on the peer.

If neither responds, continue.

## Operator action

### Step 1: rule out the network

```bash
# On a node:
ss -lntu | grep -E ':(8000|5006|1935|8004)\b'
```

If none of the listener ports are bound, the binary crashed. If they are
bound but `/healthz` is failing, the gRPC dial-out to the control plane is
hung — see Step 3.

### Step 2: restart the binary

```bash
# On the failing node:
cd /srv/docker/grimnir_radio
./grimnir restart grimnir-fanout
./grimnir logs grimnir-fanout --tail=200
```

Look for the startup line:

```
grimnir-fanout starting; version=… grpc_port=9093 http_port=8003 metrics_port=9193 harbor=8000 rtp=5006 srt=1935 webrtc=8004 engine_a=… engine_b=…
```

If the binary exits with `config error: FANOUT_ENGINE_A_RTP … is required`,
the override file lost a variable; see the env block in
`cmd/grimnir-fanout/README.md`.

### Step 3: rule out the control plane

The fan-out dials `FANOUT_CONTROL_PLANE_GRPC` on every DJ auth lookup, with
a 3-second timeout. If the control plane is hard-down, fan-out boots fine
but every DJ connect attempt fails with `auth: control plane unreachable`.

```bash
grimnir-deploy verify --focus=control-plane
```

If control plane is the actual outage, follow [verify.md](./verify.md);
once it recovers the fan-out auth cache repopulates without restart.

### Step 4: DJ-side failover

While you're fixing the node, the DJ should:

1. Point their source client at the VIP, not the per-node IP. The VIP is in
   the Harbor URL the DJ was given in their show invite.
2. Retry. If the DJ's client supports `auto-reconnect=true` (Mixxx, BUTT,
   FFmpeg's `-stream_loop -1`), the reconnect happens on its own once the
   peer is healthy.
3. If neither fan-out comes back inside 5 minutes, the DJ can switch to the
   automation fallback (a pre-recorded block scheduled with priority below
   `Live Scheduled`); the executor crossfades into it automatically when
   the live show window ends.

## Why both can fail at once

The only shared failure modes are:

- **Control plane down.** Every DJ auth lookup fails with `Unavailable`.
  Fan-outs stay up; new connections are rejected; existing connections
  keep running (auth is cached for the lifetime of the session).
- **Redis down.** Session-state replication stops. Existing sessions are
  unaffected. New sessions land on whichever fan-out the VIP picked &
  cannot be recovered if that fan-out then dies before Redis comes back.
- **Both nodes lost the VIP.** keepalived crash on both nodes; almost
  certainly a kernel/network issue affecting more than just fan-out. Treat
  as a node outage; see [drain-a-node.md](./drain-a-node.md) for the
  recovery sequence.

## Audit trail

`grimnir-deploy` subcommands write to `audit_log` & post to
`grimnir-audit-<region>` as usual. The `./grimnir restart` call does NOT
post audit rows — same documented limitation as the rest of the compose
wrapper.

## Related

- `cmd/grimnir-fanout/README.md` — env vars, port assignments, build
- `internal/grimnirfanout/auth_grpc.go` — auth cache + revocation behaviour
- `docs/superpowers/plans/2026-06-05-live-input-fan-out.md` — design notes
