# HA Track B-4: Observability, Secrets, Audit Log & Alerting Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the operational glue for v2 HA: Prometheus metrics on every binary, self-hosted ntfy.sh paging, a deploy/operator audit log with phone notifications, a pluggable secrets backend (`.env` baseline + Vault), and the auto-rollback wire between the post-deploy soak window and `grimnir-deploy --rollback`.

**Architecture:** Each Go binary exposes `/metrics` over HTTP using the existing `internal/telemetry/` registry plus a new HA-specific metrics module. Prometheus (one per region) scrapes all targets; alert rules live in-tree as YAML. A self-hosted ntfy.sh server on a separate VPS (outside any grimnir region, deliberately uncorrelated with grimnir failures) is the page/audit target. `internal/notify/` wraps the ntfy HTTP API with three severity tiers. `internal/audit/` gains a writer for operator-action rows (deploy subcommands, secret rotations) and fires an ntfy audit notification on every write. `internal/secrets/` is a new pluggable interface with `.env` + Vault implementations, selected per-region via env var. Auto-rollback is a small webhook handler embedded in `grimnir-deploy` that Prometheus' Alertmanager POSTs to when the soak-window alert fires.

**Tech Stack:** Go 1.24, `github.com/prometheus/client_golang` (already in go.mod), Prometheus 2.x + Alertmanager 0.27.x (operational; running on a VPS), `binwiederhier/ntfy` v2.11+ (self-hosted), HashiCorp Vault 1.16+ (community), `github.com/hashicorp/vault/api` SDK, Grafana 10.x for dashboards (JSON committed in-tree), `github.com/joho/godotenv` for the .env backend.

**Issue:** TBD — file when first chunk merges to v2-dev.

**Parent design:** `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md` Section 8 (full observability/security architecture) and Section 9.1 Track B-4 (positions this plan in the build sequence).

**Decisions locked in the parent design (Q-F2..Q-F6 + Q-F2a):**

| Q | Decision | Source |
|---|---|---|
| Q-F2 | **C** — self-hosted ntfy.sh for page-tier alerts | Section 8.1 |
| Q-F2a | Solo on-call for phase 1; design admits a second operator with no architectural change (append an age key) | Section 8.1 |
| Q-F4 | `.env` (baseline) **+** Vault (optional) pluggable backends in `internal/secrets/` | Section 8.3 |
| Q-F5 | Audit via Postgres `audit_log` table **+** ntfy notification on every operator action | Section 8.3 |
| Q-F6 | **C** — WireGuard mesh for in-region traffic, ZeroTier reserved for cross-region operator access | Section 8.3 |

**Honest scope:** 11 chunks (Chunk 0 spike + Chunks 1–10). Estimate **4–6 calendar weeks** at solo pace. Two chunks (3 ntfy.sh self-host, 9 dashboards) are mostly operational and might land in a day or two if the VPS and Grafana are already set up; the code chunks (1, 2, 4, 5, 6, 7, 8) are the bulk of the engineering work. The novel pieces are Chunk 6 (Vault backend) and Chunk 8 (alert-to-rollback wiring); the rest reuse existing patterns (`internal/telemetry/` for metrics, `internal/audit/` for the writer scaffolding, `internal/notifications/` for HTTP-client patterns).

**Dependencies on other Track B plans:**

- **Track B-2 (`grimnir-deploy` binary)** — Chunks 5 and 8 wire into it. Chunk 5 (audit writer) is consumable as a library by B-2 before the deploy binary itself ships. Chunk 8 (auto-rollback) cannot complete until B-2 has at least a `--rollback` flag implemented; this plan blocks at Chunk 8 step "wire to grimnir-deploy" until B-2 lands that flag.
- **Track A all steps** — Chunks 1 and 2 add HA-specific metrics. Some labels (e.g., `engine` for `grimnir_pcm_input_packets_per_second`) only exist once Track A step 4 (edge encoder + PCM transport, see `2026-06-03-edge-encoder-pcm-transport.md`) ships. This plan ships those metrics with the labels defined; the time-series only populates once the producing code exists. Documented per-metric in Chunk 1.

---

## File structure

| File | Status | Responsibility |
|---|---|---|
| `internal/metrics/ha.go` | Create | HA-specific Prometheus metric definitions (listener_reconnect_rate, vrrp_holder_count, postgres_replication_lag, etc.). Companion to existing `internal/telemetry/metrics.go` — kept separate so the HA stack can be removed independently if a deployment opts out. |
| `internal/metrics/ha_test.go` | Create | Registration + label-cardinality tests for each metric. |
| `internal/metrics/registry.go` | Create | Thin wrapper around `prometheus.DefaultRegisterer` that supports a per-binary subregistry; lets `cmd/edge-encoder` and future binaries register HA metrics without colliding. |
| `internal/metrics/registry_test.go` | Create | Subregistry isolation tests. |
| `internal/metrics/handler.go` | Create | Centralized `/metrics` handler factory — returns `http.Handler` wired to the subregistry. Replaces ad-hoc `promhttp.Handler()` calls in `cmd/mediaengine/main.go` and `internal/server/server.go`. |
| `internal/server/server.go` | Modify (line 846) | Switch from `telemetry.Handler()` to `metrics.Handler(metrics.GrimnirRadioRegistry)`. |
| `cmd/mediaengine/main.go` | Modify (line 187) | Switch from `telemetry.Handler()` to `metrics.Handler(metrics.MediaEngineRegistry)`. |
| `cmd/edge-encoder/main.go` | Modify (assumes B-3/edge-encoder plan landed) | Wire `metrics.Handler(metrics.EdgeEncoderRegistry)` onto its existing HTTP mux. |
| `cmd/grimnir-fanout/main.go` | Future create (live-input fan-out plan delivers) | Wire `metrics.Handler` once the binary exists. This plan ships the registry stub. |
| `cmd/grimnir-deploy/main.go` | Future create (Track B-2 delivers) | Embed audit writer (Chunk 5) and auto-rollback webhook receiver (Chunk 8). |
| `ops/ntfy/server.yml` | Create | `ntfy.sh` server config (topic ACLs, base URL, attachment dir, web push). |
| `ops/ntfy/grimnir.service` | Create | systemd unit for the ntfy server (Type=notify, Restart=always, dynamic user). |
| `ops/ntfy/topics.md` | Create | Topic naming conventions + token rotation procedure. Operational doc. |
| `ops/ntfy/provision.sh` | Create | Idempotent provision script for the ntfy VPS: install ntfy binary, drop config, enable systemd unit, open firewall port, configure caddy reverse proxy for TLS. |
| `internal/notify/notify.go` | Create | ntfy HTTP client. Three exported methods: `Notify(ctx, msg)`, `Page(ctx, msg)`, `PageAndRollback(ctx, msg)`. |
| `internal/notify/notify_test.go` | Create | Client tests against an httptest.Server stand-in for ntfy. |
| `internal/notify/config.go` | Create | Config loader (`GRIMNIR_NTFY_URL`, `GRIMNIR_NTFY_TOKEN_PAGE`, `GRIMNIR_NTFY_TOKEN_AUDIT`, `GRIMNIR_REGION`). |
| `internal/notify/config_test.go` | Create | Config defaults + error handling. |
| `migrations/047_audit_log.sql` | Create (next free number — verify with `ls migrations/` at execution time) | `audit_log` table per Section 8.3 schema; expand-only (no DROP, no rename). |
| `migrations/047_audit_log_down.sql` | Create | Down migration: DROP TABLE. Manual rollback only. |
| `internal/models/audit_log.go` | Create | GORM model matching the table. |
| `internal/audit/writer.go` | Create | `OperatorAction` writer with `Start(ctx, args)` and `Complete(ctx, outcome, notes)` API. Inserts the row, posts an ntfy audit-topic notification. Separate from existing `internal/audit/service.go` (which is event-bus driven for runtime priority/DJ events; the new writer is for operator actions only). |
| `internal/audit/writer_test.go` | Create | Writer tests: success path, ntfy failure does not block DB write, args redaction. |
| `internal/audit/redact.go` | Create | Argument redaction: walks an args map, replaces any value whose key matches a secret-name pattern (`password`, `token`, `secret`, `key`) with `"<redacted>"`. |
| `internal/audit/redact_test.go` | Create | Redaction tests with adversarial inputs. |
| `internal/secrets/secrets.go` | Create | `Backend` interface: `Get(ctx, name) (string, error)`, `Put(ctx, name, value) error`, `List(ctx, prefix) ([]string, error)`, `Rotate(ctx, name, newValue) (oldValue string, err error)`. `Open(ctx, backendType)` factory. |
| `internal/secrets/secrets_test.go` | Create | Interface contract tests parameterized over each backend implementation. |
| `internal/secrets/env_backend.go` | Create | `.env` file backend using `godotenv`. Read-only `Get` and `List`; `Put`/`Rotate` rewrite the file atomically. |
| `internal/secrets/env_backend_test.go` | Create | File-rewrite atomicity, missing-file behavior, environment-variable fallback. |
| `internal/secrets/vault_backend.go` | Create | Vault KV v2 backend using `github.com/hashicorp/vault/api`. AppRole auth. Wraps every operation in a context with a short timeout. |
| `internal/secrets/vault_backend_test.go` | Create | Tests against `vault server -dev` spawned by the test (see Chunk 6 spike). |
| `internal/secrets/rotation.go` | Create | `Rotate` helper: writes the new value, calls a registered "verifier" callback to prove the new value works, then commits. On verifier failure, rolls back to the old value. |
| `internal/secrets/rotation_test.go` | Create | Verifier-pass and verifier-fail paths. |
| `prometheus/grimnir.rules.yml` | Create | Alerting rules: replication-lag tiers, vrrp-holder-count, engine-health, soak-window listener-reconnect-rate, deploy-history-failed-count, redis-unreachable. |
| `prometheus/grimnir.rules_test.yml` | Create | `promtool test rules` cases for each rule. |
| `prometheus/alertmanager.yml` | Create | Alertmanager routing: tier-1 → chat webhook, tier-2 → ntfy page topic, tier-3 → ntfy page topic AND `grimnir-deploy` rollback webhook. |
| `prometheus/prometheus.yml` | Create | Scrape config: control-plane, media-engine, edge-encoder, fan-out, deploy binary; node-exporter on each HA node; alertmanager federation. |
| `ops/prometheus/provision.sh` | Create | Idempotent provisioning script for the prometheus host (mirrors `ops/ntfy/provision.sh` shape). |
| `cmd/grimnir-deploy/rollback_webhook.go` | Create (this plan; B-2 binary stub assumed) | HTTP handler that validates an Alertmanager webhook payload, audits a `--auto-rollback` action, and invokes the deploy library's rollback path. |
| `cmd/grimnir-deploy/rollback_webhook_test.go` | Create | Webhook payload validation, replay protection (idempotency key), auth header check. |
| `dashboards/grimnir-region.json` | Create | Per-region Grafana dashboard (listener counts, engine health, replication lag, byte-flow, VRRP holder). |
| `dashboards/grimnir-cross-region.json` | Create | Cross-region overview dashboard. |
| `dashboards/grimnir-deploy.json` | Create | Deploy soak-window dashboard (listener-reconnect-rate, byte-flow per node, deploy_history_failed_count). |
| `dashboards/README.md` | Create | How to import dashboards into Grafana; how to dump edits back to JSON via `jsonnet` or `grafana-dashboard-cli`. |
| `docs/runbooks/index.md` | Create | Symptom → subcommand index per Section 8.2. This plan seeds it; Track B-6 fills in entries. |
| `docs/runbooks/rotate-ntfy-token.md` | Create | ntfy token rotation procedure. |
| `docs/runbooks/rotate-vault-root.md` | Create | Vault root-token rotation procedure. |
| `docs/runbooks/rotate-jwt-signing-key.md` | Create | JWT signing key rotation (acknowledges the running-fleet token-invalidation cost; documents the staged rollout). |
| `docs/runbooks/auto-rollback.md` | Create | What auto-rollback does, what audit + ntfy notifications to expect, how to override (`--no-auto-rollback`). |
| `Makefile` | Modify | Add targets: `prometheus-validate` (`promtool check rules`, `promtool check config`), `dashboards-validate` (JSON schema check via `jq`). Add both to `make ci`. |
| `CLAUDE.md` | Modify | Section on `internal/secrets/` (which backend to use locally), `internal/metrics/` (where to add a new metric), ntfy.sh self-host pointer. |
| `internal/version/version.go` | Modify | Bump to `v2.0.0-alpha.5` on the chunk that lands the audit writer + secrets interface (first user-visible change); bump again to `v2.0.0-alpha.6` when the auto-rollback hook lands. |
| `go.mod` / `go.sum` | Modify | Add `github.com/hashicorp/vault/api`, `github.com/joho/godotenv` (if not present). |

**Decomposition principle:** observability is N independent subsystems (metrics, alerting, audit, secrets, dashboards). Each gets its own package with one responsibility. The `internal/notify/` package is the only shared dependency between subsystems; it stays small (one client + tier methods) so the surface area is auditable. The audit writer lives next to the existing `internal/audit/` service (which handles runtime event-bus audits) but in a separate file — the existing service stays untouched.

---

## Chunk 0: Spike — validate ntfy.sh self-host + Vault dev mode

> **This chunk produces no production code.** It's a 1-day disposable validation of two external dependencies. If either fails, that part of the plan needs redesign before code lands.

### Task 0.1: ntfy.sh self-host smoke test

**Files:**
- Create: `docs/superpowers/spikes/2026-06-06-ntfy-spike.md` (one report covers both spikes)

**Context:**
The design banks on a single self-hosted ntfy server outside any grimnir region serving as the page target. If ntfy can't be stood up cleanly on a 1-vCPU VPS, or if its message-throughput/delivery-latency don't match what the runbooks promise, this needs to be known **before** Chunk 3 spends operational effort on a real VPS.

For the spike, run ntfy locally in Docker — no VPS required. The validation isn't "does it scale," it's "can a Go HTTP client and a phone subscriber both deliver in under 5 seconds end-to-end."

- [ ] **Step 1: Pull ntfy and run locally**

```bash
docker pull binwiederhier/ntfy:v2.11.0
docker run --rm -p 8090:80 -e NTFY_BEHIND_PROXY=false \
  binwiederhier/ntfy:v2.11.0 serve
```

Expected: `Listening on :80` log line; `curl http://localhost:8090/v1/health` returns `{"healthy":true}`.

- [ ] **Step 2: Subscribe a phone to a topic**

Install the ntfy mobile app (F-Droid or Play Store). Add `http://<workhorse-LAN-IP>:8090` as a custom server. Subscribe to topic `grimnir-spike`.

- [ ] **Step 3: Publish from a Go test client**

Throwaway one-liner:

```bash
curl -d "spike test message" http://localhost:8090/grimnir-spike
```

Expected: phone receives the push notification within 5 seconds.

- [ ] **Step 4: Measure round-trip latency over a 50-message burst**

```bash
for i in $(seq 1 50); do
  start=$(date +%s%N)
  curl -s -d "msg $i" -H "Title: t$i" http://localhost:8090/grimnir-spike > /dev/null
  end=$(date +%s%N)
  echo "msg $i: $(( (end - start) / 1000000 ))ms"
done
```

Expected: median publish-to-200-OK well under 50ms. Phone-side receipt within 5 seconds for all 50.

- [ ] **Step 5: Validate token auth**

Restart ntfy with auth enabled:

```bash
docker run --rm -p 8090:80 \
  -e NTFY_AUTH_FILE=/var/cache/ntfy/user.db \
  -e NTFY_AUTH_DEFAULT_ACCESS=deny-all \
  -v /tmp/ntfy-cache:/var/cache/ntfy \
  binwiederhier/ntfy:v2.11.0 serve
```

Create a user, grant publish access:

```bash
docker exec <container> ntfy user add --role=admin admin
docker exec <container> ntfy access admin grimnir-spike rw
```

Verify denied without token, accepted with:

```bash
curl -d "denied" http://localhost:8090/grimnir-spike
# Expect: 401 or 403
curl -u admin:<password> -d "accepted" http://localhost:8090/grimnir-spike
# Expect: 200
```

- [ ] **Step 6: Decide go/no-go**

Write the spike report. The report MUST contain:

- Latency numbers (median + p99) from Step 4.
- Phone-delivery success rate from Step 4.
- Token auth confirmation from Step 5.
- A go/no-go for the design.
- If no-go: what specifically failed and what the alternative is (Pushover, gotify, signal-cli).

### Task 0.2: Vault dev mode + Go SDK smoke test

**Context:**
Vault is the alternative secrets backend; the design ships with `.env` as default so Vault must be optional in code. The spike verifies (a) `vault server -dev` actually works in the test runner, (b) the Go SDK's KV v2 API reads/writes round-trip, (c) AppRole auth works end-to-end. If the SDK has surprises, surface them now.

- [ ] **Step 1: Install Vault on the workhorse**

```bash
# Arch
sudo pacman -S vault
# OR Debian
curl -fsSL https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list
sudo apt-get update && sudo apt-get install -y vault
```

Verify: `vault --version` prints `Vault v1.16.x` or higher.

- [ ] **Step 2: Start dev server**

```bash
vault server -dev -dev-root-token-id=spike-root
```

Capture root token from log line. In a second shell:

```bash
export VAULT_ADDR=http://127.0.0.1:8200
export VAULT_TOKEN=spike-root
vault kv put secret/grimnir/test foo=bar baz=qux
vault kv get secret/grimnir/test
```

Expected: get returns the keys.

- [ ] **Step 3: Enable AppRole, create a role, generate role_id + secret_id**

```bash
vault auth enable approle
vault write auth/approle/role/grimnir-spike \
  token_policies=default \
  token_ttl=1h \
  token_max_ttl=4h
vault read auth/approle/role/grimnir-spike/role-id
vault write -f auth/approle/role/grimnir-spike/secret-id
```

Capture both IDs.

- [ ] **Step 4: Go round-trip test**

Throwaway `/tmp/vault-spike/main.go`:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	vault "github.com/hashicorp/vault/api"
	approle "github.com/hashicorp/vault/api/auth/approle"
)

func main() {
	config := vault.DefaultConfig()
	config.Address = "http://127.0.0.1:8200"
	client, err := vault.NewClient(config)
	if err != nil {
		log.Fatal(err)
	}

	secretID := &approle.SecretID{FromString: os.Getenv("SECRET_ID")}
	auth, err := approle.NewAppRoleAuth(os.Getenv("ROLE_ID"), secretID)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := client.Auth().Login(context.Background(), auth); err != nil {
		log.Fatal(err)
	}

	if _, err := client.KVv2("secret").Put(context.Background(), "grimnir/spike-go", map[string]interface{}{
		"value": "round-trip-test",
	}); err != nil {
		log.Fatal(err)
	}

	got, err := client.KVv2("secret").Get(context.Background(), "grimnir/spike-go")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("got=%v\n", got.Data["value"])
}
```

Run:

```bash
cd /tmp/vault-spike
go mod init vault-spike
go get github.com/hashicorp/vault/api github.com/hashicorp/vault/api/auth/approle
ROLE_ID=<id> SECRET_ID=<id> go run main.go
```

Expected: `got=round-trip-test`.

- [ ] **Step 5: Write the spike report**

Append to `docs/superpowers/spikes/2026-06-06-ntfy-spike.md` (single combined report):

- Vault version installed
- Whether AppRole worked first try or needed debugging
- Round-trip test outcome
- Any surprises in the SDK API surface (e.g., context propagation, error types)
- Confirm the SDK is fit for Chunk 6, or specify an alternative (raw KV REST API, infisical, etc.)

- [ ] **Step 6: Clean up**

```bash
docker stop <ntfy-container-id>
# Vault dev server: Ctrl-C
rm -rf /tmp/vault-spike /tmp/ntfy-cache
```

- [ ] **Step 7: Commit the spike report only — no code**

```bash
cd /home/code/projects/grimnir_radio
git add docs/superpowers/spikes/2026-06-06-ntfy-spike.md
git commit -m "spike: validate ntfy self-host + Vault dev mode for HA Track B-4"
```

---
## Chunk 1: Prometheus metrics package (`internal/metrics/`)

**Why a new package alongside `internal/telemetry/`:** `internal/telemetry/metrics.go` uses `promauto` against the default global registry. That's fine for shared metrics (scheduler, executor, playout, recording — already there) but wrong for HA metrics that need to be (a) added to a binary-specific registry so edge-encoder and fan-out don't have to import scheduler-only metrics, (b) testable in isolation without leaking into the global registry between tests. The new package defines per-binary registries and the HA metric set.

### Task 1.1: Define the registry abstraction

**Files:**
- Create: `internal/metrics/registry.go`
- Test: `internal/metrics/registry_test.go`

- [ ] **Step 1: Write the failing test**

`internal/metrics/registry_test.go`:

```go
package metrics

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestRegistryIsIsolated(t *testing.T) {
	r1 := NewRegistry("test1")
	r2 := NewRegistry("test2")

	c1 := prometheus.NewCounter(prometheus.CounterOpts{Name: "shared_name"})
	c2 := prometheus.NewCounter(prometheus.CounterOpts{Name: "shared_name"})

	if err := r1.Register(c1); err != nil {
		t.Fatalf("r1 register: %v", err)
	}
	// Same name on a *separate* registry must NOT collide.
	if err := r2.Register(c2); err != nil {
		t.Fatalf("r2 register: %v", err)
	}

	c1.Inc()
	c1.Inc()
	if got := testutil.ToFloat64(c1); got != 2 {
		t.Errorf("c1 = %v, want 2", got)
	}
	if got := testutil.ToFloat64(c2); got != 0 {
		t.Errorf("c2 = %v, want 0 (isolated)", got)
	}
}

func TestRegistryHandlerEmitsRegisteredMetrics(t *testing.T) {
	r := NewRegistry("test-handler")
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: "handler_test_total", Help: "x"})
	r.MustRegister(c)
	c.Add(7)

	body := scrapeRegistry(t, r)
	if !strings.Contains(body, "handler_test_total 7") {
		t.Errorf("scrape output missing metric: %s", body)
	}
}
```

Helper `scrapeRegistry` will be defined inline once the handler exists; the test compiles after Step 3.

- [ ] **Step 2: Run the test to verify it fails**

```bash
cd /home/code/projects/grimnir_radio
go test ./internal/metrics/... 2>&1 | head -20
```

Expected: build failure ("package internal/metrics not found" or undefined `NewRegistry`).

- [ ] **Step 3: Write the registry implementation**

`internal/metrics/registry.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package metrics provides per-binary Prometheus registries for the HA stack.
//
// This is intentionally separate from internal/telemetry, which uses the
// global default registry for cross-binary shared metrics (scheduler,
// executor, playout). The HA metrics defined here are per-binary so that
// edge-encoder and grimnir-fanout don't have to import scheduler-only
// definitions, and so tests stay isolated.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Registry wraps prometheus.Registry with a human-readable name for diagnostics.
type Registry struct {
	*prometheus.Registry
	Name string
}

// NewRegistry creates an isolated registry pre-loaded with go-runtime and
// process collectors.
func NewRegistry(name string) *Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector())
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &Registry{Registry: r, Name: name}
}

// Per-binary registries. Each Go binary picks the one it owns at init time
// and registers HA-specific metrics into it.
var (
	GrimnirRadioRegistry = NewRegistry("grimnirradio")
	MediaEngineRegistry  = NewRegistry("mediaengine")
	EdgeEncoderRegistry  = NewRegistry("edge-encoder")
	FanoutRegistry       = NewRegistry("grimnir-fanout")
	DeployRegistry       = NewRegistry("grimnir-deploy")
)
```

- [ ] **Step 4: Add the test helper + handler stub**

Append to `internal/metrics/registry_test.go`:

```go
import (
	"io"
	"net/http/httptest"
)

func scrapeRegistry(t *testing.T, r *Registry) string {
	t.Helper()
	srv := httptest.NewServer(Handler(r))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(body)
}
```

(Note: also import `net/http` at the test file head.)

- [ ] **Step 5: Implement the handler**

`internal/metrics/handler.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handler returns an http.Handler that scrapes the given registry.
// One handler per binary; mount at /metrics.
func Handler(r *Registry) http.Handler {
	return promhttp.HandlerFor(r.Registry, promhttp.HandlerOpts{
		Registry:          r.Registry,
		EnableOpenMetrics: true,
	})
}
```

- [ ] **Step 6: Run the test to verify it passes**

```bash
go test -run TestRegistry ./internal/metrics/... -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/metrics/registry.go internal/metrics/handler.go internal/metrics/registry_test.go
git commit -m "feat(metrics): add per-binary Prometheus registry for HA stack"
```

### Task 1.2: Define the HA metric set

**Files:**
- Create: `internal/metrics/ha.go`
- Test: `internal/metrics/ha_test.go`

**Context:**
Each metric from Section 8.1 lives here. A metric is registered into whichever per-binary registries actually produce it. The producer side (the code that calls `.Inc()` or `.Set()`) lives in Chunk 2; this task only defines and registers.

Per-metric ownership map (drives which registry each metric registers into):

| Metric | Registries |
|---|---|
| `grimnir_listener_reconnect_rate_per_5min` | EdgeEncoderRegistry (counted at the edge encoder's listener disconnect handler) |
| `grimnir_edge_encoder_bytes_per_second{node}` | EdgeEncoderRegistry |
| `grimnir_postgres_replication_lag_seconds` | GrimnirRadioRegistry (control plane queries `pg_stat_replication`) |
| `grimnir_vrrp_holder_count{vip}` | GrimnirRadioRegistry (control plane queries `keepalived`'s notify script output via Redis) |
| `grimnir_engine_health{node}` | MediaEngineRegistry (engine exposes its own health) AND EdgeEncoderRegistry (mirrored from gRPC health subscription) |
| `grimnir_pcm_input_packets_per_second{engine,source}` | EdgeEncoderRegistry |
| `grimnir_deploy_history_failed_count` | DeployRegistry |
| `grimnir_redis_unreachable_seconds` | GrimnirRadioRegistry (control plane Redis client circuit-breaker) |
| `grimnir_cache_hit_rate_per_hour` | GrimnirRadioRegistry (media cache + general cache) |

- [ ] **Step 1: Write the failing test**

`internal/metrics/ha_test.go`:

```go
package metrics

import (
	"strings"
	"testing"
)

// Each HA metric must be registered into its declared registry and exposed
// via that registry's /metrics handler. The body check is intentionally a
// string-contains rather than testutil.CollectAndCount so we catch metric
// name typos that would otherwise pass under a name-keyed lookup.
func TestHAMetricsRegisteredInExpectedRegistries(t *testing.T) {
	tests := []struct {
		registry *Registry
		want     []string
	}{
		{EdgeEncoderRegistry, []string{
			"grimnir_listener_reconnect_total",
			"grimnir_edge_encoder_bytes_total",
			"grimnir_pcm_input_packets_total",
			"grimnir_engine_health",
		}},
		{GrimnirRadioRegistry, []string{
			"grimnir_postgres_replication_lag_seconds",
			"grimnir_vrrp_holder_count",
			"grimnir_redis_unreachable_seconds",
			"grimnir_cache_hit_rate_ratio",
		}},
		{MediaEngineRegistry, []string{
			"grimnir_engine_health",
		}},
		{DeployRegistry, []string{
			"grimnir_deploy_history_failed_total",
		}},
	}
	for _, tt := range tests {
		body := scrapeRegistry(t, tt.registry)
		for _, name := range tt.want {
			if !strings.Contains(body, "# HELP "+name) {
				t.Errorf("registry %q missing %q in:\n%s", tt.registry.Name, name, body)
			}
		}
	}
}
```

> **Note on metric names:** the design doc lists e.g. `grimnir_listener_reconnect_rate_per_5min`. That's a derived view, not a raw metric. Prometheus convention is to expose a `_total` counter and derive `rate(... [5m])` in queries. The implementation uses `_total`; the alert rule (Chunk 7) does the rate derivation. Documented in `ha.go` doc comments.

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestHAMetrics ./internal/metrics/... -v
```

Expected: FAIL — metrics not registered yet.

- [ ] **Step 3: Implement the HA metric definitions**

`internal/metrics/ha.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package metrics

import "github.com/prometheus/client_golang/prometheus"

// HA metrics — see docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md
// Section 8.1 for the policy these metrics drive.
//
// Naming follows Prometheus conventions: counters end in _total, gauges
// describe the current value, histograms end in _seconds for timings.
// Per-binary registration: see registry assignments at the bottom of this file.

var (
	// ListenerReconnectTotal — increments each time a listener's TCP stream
	// reconnects within a short window. Rate over 5m drives the soak-window
	// auto-rollback alert.
	ListenerReconnectTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_listener_reconnect_total",
			Help: "Total listener reconnects (rate [5m] feeds the soak-window auto-rollback alert).",
		},
		[]string{"mount"},
	)

	// EdgeEncoderBytesTotal — bytes the edge encoder has shipped to clients
	// per node. Tier-3 alert: both nodes hit zero during soak window.
	EdgeEncoderBytesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_edge_encoder_bytes_total",
			Help: "Total bytes shipped by the edge encoder; rate gives bytes/sec.",
		},
		[]string{"node"},
	)

	// PostgresReplicationLagSeconds — primary-to-replica WAL lag.
	// Tier-1 alert > 5s; tier-2 alert > 30s.
	PostgresReplicationLagSeconds = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "grimnir_postgres_replication_lag_seconds",
			Help: "Streaming replication lag in seconds (queried from pg_stat_replication).",
		},
	)

	// VrrpHolderCount — count of nodes claiming a given VIP. Should always
	// equal 1. Tier-2 alert at 0 (no holder) or 2 (split-brain).
	VrrpHolderCount = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_vrrp_holder_count",
			Help: "Number of nodes claiming a given VIP (must equal 1).",
		},
		[]string{"vip"},
	)

	// EngineHealth — per-node engine health: 1=serving, 0=not_serving.
	// Tier-2 alert when both nodes in a region report 0.
	EngineHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_engine_health",
			Help: "Engine health (1=serving, 0=not_serving), per node.",
		},
		[]string{"node"},
	)

	// PcmInputPacketsTotal — RTP packet arrival count per engine-source pair.
	// Tier-1 alert when rate falls below an engine-specific threshold; the
	// edge encoder switches internally, this metric is observational.
	PcmInputPacketsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_pcm_input_packets_total",
			Help: "RTP PCM packets received per engine-source pair.",
		},
		[]string{"engine", "source"},
	)

	// DeployHistoryFailedTotal — incremented by grimnir-deploy on a failed
	// deploy. An increment is itself the tier-2 alert condition (alertmanager
	// fires on rate > 0 over 5m).
	DeployHistoryFailedTotal = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "grimnir_deploy_history_failed_total",
			Help: "Total failed deploys (an increment is the alert condition).",
		},
	)

	// RedisUnreachableSeconds — cumulative seconds the control plane's
	// Redis client has been unable to reach Redis. Tier-2 alert > 60s.
	RedisUnreachableSeconds = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "grimnir_redis_unreachable_seconds",
			Help: "Cumulative seconds Redis has been unreachable.",
		},
	)

	// CacheHitRateRatio — rolling hourly hit rate for the media cache.
	// Tier-1 alert < 0.8 (informational; capacity-planning signal, not paging).
	CacheHitRateRatio = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "grimnir_cache_hit_rate_ratio",
			Help: "Rolling 1h media-cache hit rate (0.0..1.0).",
		},
	)
)

func init() {
	// Edge encoder owns listener-facing metrics + the PCM input view.
	EdgeEncoderRegistry.MustRegister(
		ListenerReconnectTotal,
		EdgeEncoderBytesTotal,
		PcmInputPacketsTotal,
		EngineHealth, // mirror of the engines it talks to
	)

	// Control plane owns DB + Redis + VIP + cache metrics.
	GrimnirRadioRegistry.MustRegister(
		PostgresReplicationLagSeconds,
		VrrpHolderCount,
		RedisUnreachableSeconds,
		CacheHitRateRatio,
	)

	// Engine self-reports its own health.
	MediaEngineRegistry.MustRegister(
		EngineHealth,
	)

	// Deploy binary owns its failure counter.
	DeployRegistry.MustRegister(
		DeployHistoryFailedTotal,
	)
}
```

> **Init-time double-registration caveat:** `EngineHealth` registers into TWO registries (EdgeEncoderRegistry and MediaEngineRegistry). That's the intent — the engine sets its own value, the edge encoder mirrors what it learns from gRPC. Both observations are independently useful in Prometheus. If a future binary tries to register the SAME `*GaugeVec` instance into an additional registry, that's fine; if it accidentally creates a new `GaugeVec` with the same name and registers it into the same registry, Prometheus panics at init. Both behaviors are correct.

- [ ] **Step 4: Run the tests**

```bash
go test ./internal/metrics/... -v
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/ha.go internal/metrics/ha_test.go
git commit -m "feat(metrics): define HA metric set for Track B-4"
```

### Task 1.3: Document the metrics package in CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Find the "Architecture / Key Directories" list in CLAUDE.md and add an entry**

Use grep:

```bash
grep -n "internal/telemetry\|internal/api" CLAUDE.md
```

- [ ] **Step 2: Add the directory entry**

Append after the existing `internal/telemetry/` mention (or near the relevant block):

```markdown
- `internal/metrics/` - HA-specific Prometheus metrics with per-binary registries. Add new HA metrics here; use `internal/telemetry/` for legacy/cross-binary shared metrics.
```

- [ ] **Step 3: Run `make ci` to verify nothing broke**

```bash
make ci
```

Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document internal/metrics in CLAUDE.md"
```

---
## Chunk 2: Wire metrics into existing services

This chunk produces no new files but lights up every metric defined in Chunk 1. Each metric gets its producer wired up. Order is "easiest to verify first" so each wiring lands with a passing observability test.

### Task 2.1: Switch grimnirradio /metrics to the new registry

**Files:**
- Modify: `internal/server/server.go` (line 846 — existing `telemetry.Handler()` call)

**Context:**
The existing handler scrapes the default Prometheus registry, which gets the legacy `internal/telemetry/` metrics. The new handler also needs to expose those — otherwise this change is a regression. Approach: register the default registry's collectors into the new registry too, so a single `/metrics` endpoint emits both.

- [ ] **Step 1: Write the failing test**

`internal/server/server_metrics_test.go` (create new file):

```go
package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

// /metrics must expose BOTH legacy telemetry metrics (default registry)
// and new HA metrics (GrimnirRadioRegistry).
func TestMetricsHandlerExposesLegacyAndHA(t *testing.T) {
	srv := httptest.NewServer(metrics.Handler(metrics.GrimnirRadioRegistry))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	s := string(body)

	// HA metric from GrimnirRadioRegistry:
	if !strings.Contains(s, "grimnir_postgres_replication_lag_seconds") {
		t.Errorf("missing HA metric in scrape: %s", s)
	}
	// Legacy metric from internal/telemetry (default registry, gathered via collector bridge):
	if !strings.Contains(s, "grimnir_scheduler_ticks_total") {
		t.Errorf("missing legacy metric in scrape: %s", s)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

```bash
go test -run TestMetricsHandlerExposesLegacyAndHA ./internal/server/...
```

Expected: FAIL — only HA metrics show, legacy metrics absent.

- [ ] **Step 3: Bridge the default registry into the new registries**

Modify `internal/metrics/registry.go` to add a bridge helper:

```go
// BridgeDefaultRegistry causes the given registry's Gather() to ALSO include
// metrics from prometheus.DefaultGatherer. This preserves backwards-
// compatibility with packages using promauto against the default registry
// (e.g., internal/telemetry).
//
// Call once per binary, AFTER all internal/telemetry init() functions have
// run (they run at import time, so calling this from main is sufficient).
func (r *Registry) BridgeDefaultRegistry() {
	// prometheus.DefaultGatherer is the package-default *Registry. Wrap it
	// in a collector that the new registry can register.
	r.MustRegister(defaultRegistryCollector{})
}

type defaultRegistryCollector struct{}

func (defaultRegistryCollector) Describe(ch chan<- *prometheus.Desc) {
	// Indicate "unchecked collector" by sending no descs. The bridge does
	// not need static description; descriptions come from each collected
	// metric at scrape time.
}

func (defaultRegistryCollector) Collect(ch chan<- prometheus.Metric) {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return
	}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			// Emit each metric as an "unchecked" metric. This is the same
			// pattern used by go-promhttp's MultiGatherer.
			ch <- &bridgedMetric{family: mf, metric: m}
		}
	}
}
```

(Plus the `bridgedMetric` adapter type — about 40 lines, implementing `prometheus.Metric`. The reviewer should confirm this against `go doc prometheus.Metric` at execution time; if simpler, replace with `prometheus.NewMultiRegistry`-style approach using `promhttp.MultiGatherer` directly in `Handler()`.)

> **Alternative implementation (simpler if MultiGatherer covers the use case):** in `internal/metrics/handler.go`, replace `promhttp.HandlerFor(r.Registry, ...)` with `promhttp.HandlerFor(prometheus.Gatherers{r.Registry, prometheus.DefaultGatherer}, ...)`. This avoids the custom collector entirely. Try this approach FIRST; only fall back to the custom collector if it fails.

- [ ] **Step 4: Re-run the test**

```bash
go test -run TestMetricsHandlerExposesLegacyAndHA ./internal/server/...
```

Expected: PASS.

- [ ] **Step 5: Switch server.go to the new handler**

In `internal/server/server.go` line 846:

```go
// Was:  s.router.Handle("/metrics", telemetry.Handler())
s.router.Handle("/metrics", metrics.Handler(metrics.GrimnirRadioRegistry))
```

Add the import: `"github.com/friendsincode/grimnir_radio/internal/metrics"`.

- [ ] **Step 6: Run the full server tests**

```bash
go test ./internal/server/... -v
```

Expected: PASS, no regressions.

- [ ] **Step 7: Commit**

```bash
git add internal/metrics/handler.go internal/metrics/registry.go internal/server/server.go internal/server/server_metrics_test.go
git commit -m "feat(metrics): bridge default registry; switch server /metrics to GrimnirRadioRegistry"
```

### Task 2.2: Same switch for mediaengine

**Files:**
- Modify: `cmd/mediaengine/main.go` (line 187)

- [ ] **Step 1: Write the failing test**

`cmd/mediaengine/main_metrics_test.go` — mirror of Task 2.1's test but for `MediaEngineRegistry`. Verify both `grimnir_engine_health` (HA) and any existing mediaengine telemetry metrics surface.

- [ ] **Step 2: Run, see fail.**

- [ ] **Step 3: Edit the file**

In `cmd/mediaengine/main.go` line 187:

```go
metricsMux.Handle("/metrics", metrics.Handler(metrics.MediaEngineRegistry))
```

- [ ] **Step 4: Run again, expect pass.**

- [ ] **Step 5: Commit**

```bash
git add cmd/mediaengine/main.go cmd/mediaengine/main_metrics_test.go
git commit -m "feat(metrics): switch mediaengine /metrics to MediaEngineRegistry"
```

### Task 2.3: Wire `PostgresReplicationLagSeconds`

**Files:**
- Create: `internal/dbhealth/replication.go`
- Test: `internal/dbhealth/replication_test.go`

**Context:**
This needs a Postgres primary + replica to test end-to-end. For unit tests, mock the database; for an integration test (run only when `RLM_INTEGRATION_DB=1`), point at a real pair. Phase 1 ships with the unit test only; the integration test runs as part of Track A step 1 acceptance.

- [ ] **Step 1: Write the unit test**

```go
package dbhealth

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

func TestReplicationLagPoller_PrimaryWithReplica(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	rows := sqlmock.NewRows([]string{"lag_seconds"}).AddRow(2.5)
	mock.ExpectQuery("EXTRACT.*FROM pg_stat_replication").WillReturnRows(rows)

	gdb, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}

	p := NewReplicationLagPoller(gdb)
	if err := p.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := testutil.ToFloat64(metrics.PostgresReplicationLagSeconds); got != 2.5 {
		t.Errorf("lag = %v, want 2.5", got)
	}
}

func TestReplicationLagPoller_NoReplicaReportsZero(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	mock.ExpectQuery("pg_stat_replication").WillReturnRows(sqlmock.NewRows([]string{"lag_seconds"}))

	gdb, _ := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{})
	p := NewReplicationLagPoller(gdb)
	if err := p.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Zero rows -> 0 lag (no replica connected) is the correct value.
	if got := testutil.ToFloat64(metrics.PostgresReplicationLagSeconds); got != 0 {
		t.Errorf("lag = %v, want 0 when no replica", got)
	}
}
```

- [ ] **Step 2: Run, fail, write implementation**

`internal/dbhealth/replication.go`:

```go
package dbhealth

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

type ReplicationLagPoller struct {
	db *gorm.DB
}

func NewReplicationLagPoller(db *gorm.DB) *ReplicationLagPoller {
	return &ReplicationLagPoller{db: db}
}

// Poll runs once. Caller (cmd/grimnirradio/main.go) tickers this at 10s.
func (p *ReplicationLagPoller) Poll(ctx context.Context) error {
	var lag float64
	row := p.db.WithContext(ctx).Raw(`
		SELECT COALESCE(EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp())), 0)
		FROM pg_stat_replication
		LIMIT 1
	`).Row()
	// row.Scan returns sql.ErrNoRows when there is no replica yet; treat as 0.
	_ = row.Scan(&lag)
	metrics.PostgresReplicationLagSeconds.Set(lag)
	return nil
}

func (p *ReplicationLagPoller) Run(ctx context.Context) {
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = p.Poll(ctx)
		}
	}
}
```

> **SQL note:** the query above is wrong for the primary-side view of lag — `pg_last_xact_replay_timestamp()` is a REPLICA-side function. On the primary, use `pg_stat_replication.replay_lag` (an interval). The corrected query:
>
> ```sql
> SELECT COALESCE(EXTRACT(EPOCH FROM replay_lag), 0) FROM pg_stat_replication LIMIT 1
> ```
>
> The poller runs on the **control plane**, which connects to pgbouncer → primary. So the primary-side query is correct. The replica-side query in the snippet above is a bug; fix during implementation. Tests above mock the result and don't care which query produces it; integration tests will catch a real-query bug.

- [ ] **Step 3: Wire into grimnirradio main**

In `cmd/grimnirradio/main.go`, after the DB is opened:

```go
go dbhealth.NewReplicationLagPoller(db).Run(ctx)
```

- [ ] **Step 4: Run tests + manual scrape**

```bash
go test ./internal/dbhealth/...
go test ./...   # full suite
make run-control &
sleep 5
curl -s localhost:8080/metrics | grep replication_lag
```

Expected: a value (0 if no replica) for `grimnir_postgres_replication_lag_seconds`.

- [ ] **Step 5: Commit**

```bash
git add internal/dbhealth cmd/grimnirradio/main.go
git commit -m "feat(metrics): wire postgres replication lag poller"
```

### Task 2.4: Wire `VrrpHolderCount`

**Files:**
- Create: `internal/vrrphealth/vrrp.go`
- Test: `internal/vrrphealth/vrrp_test.go`

**Context:**
keepalived calls `notify_master` / `notify_backup` / `notify_fault` scripts on VRRP state transitions. The script writes the state to Redis under a per-VIP key. The grimnirradio control plane polls Redis and sets the gauge per VIP. With two HA nodes, the gauge should sum to 1 (one master, one backup) — except during a transition or split-brain.

For this chunk, deliver the Go-side poller + the script. keepalived itself is operationally installed in Track A step 7; the poller can be written and tested without keepalived running.

- [ ] **Step 1: Write the failing test**

```go
func TestVrrpHolder_OneMaster(t *testing.T) {
	rdb := redismock.NewClient()
	rdb.HSet(ctx, "grimnir:vrrp:listener", "node-1", "master")
	rdb.HSet(ctx, "grimnir:vrrp:listener", "node-2", "backup")

	p := NewVrrpPoller(rdb, []string{"listener"})
	p.Poll(ctx)
	if got := testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener")); got != 1 {
		t.Errorf("holder count = %v, want 1", got)
	}
}

func TestVrrpHolder_SplitBrain(t *testing.T) {
	rdb := redismock.NewClient()
	rdb.HSet(ctx, "grimnir:vrrp:listener", "node-1", "master")
	rdb.HSet(ctx, "grimnir:vrrp:listener", "node-2", "master")

	p := NewVrrpPoller(rdb, []string{"listener"})
	p.Poll(ctx)
	if got := testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener")); got != 2 {
		t.Errorf("holder count = %v, want 2 (split brain)", got)
	}
}

func TestVrrpHolder_NoHolder(t *testing.T) {
	rdb := redismock.NewClient()
	rdb.HSet(ctx, "grimnir:vrrp:listener", "node-1", "fault")
	rdb.HSet(ctx, "grimnir:vrrp:listener", "node-2", "backup")

	p := NewVrrpPoller(rdb, []string{"listener"})
	p.Poll(ctx)
	if got := testutil.ToFloat64(metrics.VrrpHolderCount.WithLabelValues("listener")); got != 0 {
		t.Errorf("holder count = %v, want 0", got)
	}
}
```

- [ ] **Step 2: Implement**

```go
// internal/vrrphealth/vrrp.go
package vrrphealth

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/friendsincode/grimnir_radio/internal/metrics"
)

type Poller struct {
	rdb  *redis.Client
	vips []string
}

func NewVrrpPoller(rdb *redis.Client, vips []string) *Poller {
	return &Poller{rdb: rdb, vips: vips}
}

func (p *Poller) Poll(ctx context.Context) {
	for _, vip := range p.vips {
		states, err := p.rdb.HGetAll(ctx, "grimnir:vrrp:"+vip).Result()
		if err != nil {
			continue
		}
		count := 0
		for _, s := range states {
			if s == "master" {
				count++
			}
		}
		metrics.VrrpHolderCount.WithLabelValues(vip).Set(float64(count))
	}
}

func (p *Poller) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.Poll(ctx)
		}
	}
}
```

Also drop a keepalived notify script for operators to install (delivered in Chunk 3's ops dir):

`ops/keepalived/notify.sh`:

```bash
#!/bin/bash
# Called by keepalived on state transitions.
# Args: $1=TYPE $2=NAME $3=STATE
# Example: GROUP listener MASTER
NODE="$(hostname -s)"
redis-cli -h "$REDIS_HOST" -a "$REDIS_PASSWORD" \
  HSET "grimnir:vrrp:$2" "$NODE" "$(echo $3 | tr 'A-Z' 'a-z')"
```

- [ ] **Step 3: Test + commit**

```bash
go test ./internal/vrrphealth/...
git add internal/vrrphealth ops/keepalived
git commit -m "feat(metrics): wire VRRP holder-count gauge from Redis state"
```

### Task 2.5: Wire remaining metrics (sketch)

The remaining HA metrics follow the same pattern. Each gets a short PR.

| Metric | Producer location | Notes |
|---|---|---|
| `ListenerReconnectTotal` | edge-encoder broadcast.Mount on client disconnect+reconnect | See edge-encoder plan Chunk 6 (broadcast adapter) — wire there. |
| `EdgeEncoderBytesTotal` | edge-encoder broadcast loop, += len(chunk) per write | Trivial; one line in the broadcast loop. |
| `EngineHealth` (engine side) | mediaengine gRPC `GetStatus`/internal health monitor | Set to 1 on healthy state, 0 on degraded. |
| `EngineHealth` (edge encoder side) | edge-encoder health.go (already in edge-encoder plan) | Mirror what gRPC subscription reports. |
| `PcmInputPacketsTotal` | edge-encoder health.go packet-arrival watchdog | Increment per RTP packet received. |
| `DeployHistoryFailedTotal` | grimnir-deploy (Track B-2) on a failure exit | One line in the deploy main's error path. |
| `RedisUnreachableSeconds` | internal/eventbus/redis bus circuit-breaker | Add a stopwatch around the breaker's open-state duration; increment counter on each Close. |
| `CacheHitRateRatio` | internal/cache + internal/media cache | Compute rolling 1h ratio from hit/miss counters; tickered. |

Each is a self-contained <1-day task. Land them as they're touched by Track A milestones, not all at once. Skip the metrics whose producers don't exist yet (PcmInputPackets, EngineHealth-from-engine, ListenerReconnectTotal — those land with edge-encoder and engine work).

- [ ] **Step 1: For each row above, create an issue or PR comment with the file + line to wire.**

- [ ] **Step 2: Wire `EdgeEncoderBytesTotal` now (edge-encoder plan Chunk 6 lands first, and it's a one-liner)**

In the edge encoder's broadcast loop (per the edge-encoder plan, file `internal/edgeencoder/broadcast.go`):

```go
n, err := w.Write(chunk)
if err == nil {
    metrics.EdgeEncoderBytesTotal.WithLabelValues(nodeName).Add(float64(n))
}
```

- [ ] **Step 3: Commit per wiring as it lands**

Format: `feat(metrics): wire <metric_name> producer`.

---
## Chunk 3: ntfy.sh self-host

This is mostly operational. The only "code" is YAML, a systemd unit, and a provisioning script. The deliverable is a running ntfy server at a stable URL (e.g., `https://ntfy.grimnir.example`) with three topics per region and per-topic tokens.

### Task 3.1: Provision the VPS

**Files:**
- Create: `ops/ntfy/provision.sh`

**Context:**
The ntfy host MUST be on infrastructure outside any grimnir region. Per Section 8.1: "The alerting target failing should not correlate with Grimnir incidents." A 1-vCPU/1GB VPS at a separate provider is enough (ntfy memory footprint is ~50MB). Hetzner CAX11 or DigitalOcean s-1vcpu-1gb both work.

This task assumes you've manually created the VPS, set up SSH access, and pointed `ntfy.grimnir.example` DNS at it. The script is idempotent so you can re-run it for upgrades.

- [ ] **Step 1: Create the provisioning script**

```bash
#!/usr/bin/env bash
# ops/ntfy/provision.sh — idempotent ntfy.sh server provisioning.
# Run as root on the ntfy VPS.
set -euo pipefail

NTFY_VERSION="${NTFY_VERSION:-2.11.0}"
NTFY_USER="${NTFY_USER:-ntfy}"
DOMAIN="${DOMAIN:?DOMAIN env var required, e.g. ntfy.grimnir.example}"
ADMIN_EMAIL="${ADMIN_EMAIL:?ADMIN_EMAIL env var required (for Let's Encrypt)}"

# 1. Install ntfy
if ! command -v ntfy >/dev/null; then
  curl -fL "https://github.com/binwiederhier/ntfy/releases/download/v${NTFY_VERSION}/ntfy_${NTFY_VERSION}_linux_amd64.deb" \
    -o /tmp/ntfy.deb
  dpkg -i /tmp/ntfy.deb
  rm /tmp/ntfy.deb
fi

# 2. Install Caddy (reverse proxy + automatic TLS)
if ! command -v caddy >/dev/null; then
  apt-get install -y debian-keyring debian-archive-keyring apt-transport-https
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | \
    gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' | \
    tee /etc/apt/sources.list.d/caddy-stable.list
  apt-get update
  apt-get install -y caddy
fi

# 3. Drop ntfy server config
install -d -o "$NTFY_USER" -g "$NTFY_USER" -m 0755 /var/cache/ntfy /var/log/ntfy /etc/ntfy

cat >/etc/ntfy/server.yml <<EOF
# Generated by ops/ntfy/provision.sh — do not edit by hand.
base-url: "https://${DOMAIN}"
listen-http: "127.0.0.1:2586"
auth-file: /var/cache/ntfy/user.db
auth-default-access: deny-all
behind-proxy: true
attachment-cache-dir: /var/cache/ntfy/attachments
attachment-total-size-limit: "1G"
attachment-file-size-limit: "5M"
attachment-expiry-duration: "12h"
web-push-public-key: ""   # filled by 'ntfy webpush keys' on first run, see Task 3.2
web-push-private-key: ""
web-push-email: "${ADMIN_EMAIL}"
EOF
chown "$NTFY_USER:$NTFY_USER" /etc/ntfy/server.yml
chmod 0640 /etc/ntfy/server.yml

# 4. systemd unit (ntfy ships one; this just ensures it's enabled)
systemctl enable --now ntfy.service

# 5. Caddy reverse proxy
cat >/etc/caddy/Caddyfile <<EOF
${DOMAIN} {
  reverse_proxy 127.0.0.1:2586
  encode gzip
  header {
    Strict-Transport-Security "max-age=31536000; includeSubDomains"
  }
}
EOF
systemctl reload caddy

# 6. Firewall: only 22, 80, 443
ufw allow 22/tcp
ufw allow 80/tcp
ufw allow 443/tcp
ufw --force enable

echo "Provision complete. Verify:"
echo "  curl https://${DOMAIN}/v1/health"
```

- [ ] **Step 2: Run on the VPS**

```bash
scp ops/ntfy/provision.sh root@ntfy.grimnir.example:/tmp/
ssh root@ntfy.grimnir.example DOMAIN=ntfy.grimnir.example ADMIN_EMAIL=ops@grimnir.example bash /tmp/provision.sh
```

Verify: `curl https://ntfy.grimnir.example/v1/health` returns `{"healthy":true}`.

- [ ] **Step 3: Commit the script**

```bash
git add ops/ntfy/provision.sh
git commit -m "ops(ntfy): add idempotent VPS provisioning script"
```

### Task 3.2: Create topics + per-region tokens

**Files:**
- Create: `ops/ntfy/topics.md`

**Context:**
Per Section 8.3: per-region tokens scoped so a leak doesn't expose other regions. With one region in phase 1, that's three topics, three tokens. The topic naming convention extends cleanly when phase 2 adds regions.

Topic names (phase 1):

| Topic | Purpose |
|---|---|
| `grimnir-region-default-page` | Tier-2 and tier-3 alerts for the default region |
| `grimnir-audit-default` | Audit log echoes (every operator action) |
| `grimnir-region-default-rollback` | Tier-3 auto-rollback echoes (separate from page so phone tone differs) |

For phase 2: substitute the region's short name (e.g., `us-east`, `eu-west`) for `default`.

- [ ] **Step 1: On the ntfy VPS, create users and tokens**

```bash
# Admin (only used by the provisioning operator):
ntfy user add --role=admin admin
# Set password when prompted; store it in 1Password under "ntfy admin".

# Per-topic publisher tokens. Each grimnir binary uses a token, not a password.
ntfy user add --role=user grimnir-page
ntfy access grimnir-page grimnir-region-default-page rw

ntfy user add --role=user grimnir-audit
ntfy access grimnir-audit grimnir-audit-default rw

ntfy user add --role=user grimnir-rollback
ntfy access grimnir-rollback grimnir-region-default-rollback rw

# Generate tokens
ntfy token add grimnir-page --label "Phase 1 page-tier publisher"
ntfy token add grimnir-audit --label "Phase 1 audit-topic publisher"
ntfy token add grimnir-rollback --label "Phase 1 rollback-topic publisher"
```

Capture the printed tokens. Store them in the secrets backend (Chunk 6) once it ships; until then, store in 1Password under "ntfy publisher tokens".

- [ ] **Step 2: Configure phone subscription**

On the ntfy mobile app, subscribe to all three topics on `https://ntfy.grimnir.example`. Set per-topic notification tones so you can tell page (loud) from audit (soft) from rollback (distinct alarm).

- [ ] **Step 3: Write the topics doc**

`ops/ntfy/topics.md`:

```markdown
# ntfy.sh topic conventions

## Per-region topics

For a region with short name `R`:

- `grimnir-region-R-page` — tier-2 and tier-3 page alerts
- `grimnir-region-R-rollback` — tier-3 alerts that triggered an auto-rollback
- `grimnir-audit-R` — every operator action (deploys, rotations, manual interventions)

Phase 1 uses `R=default`. Phase 2 substitutes the real region short name.

## Tokens

One publisher token per topic. Tokens are scoped to publish-only on a single topic; a leak exposes one topic in one region. Operator rotates a token via the [rotate-ntfy-token runbook](../../docs/runbooks/rotate-ntfy-token.md).

## Phone subscriptions

Subscribe to ALL topics in all regions. Set per-topic tones so you can tell at 3am whether the buzz is a page (act), audit (read), or rollback (informational, system already acted).

## Adding a region

1. Pick the region short name (e.g., `eu-west`).
2. SSH to the ntfy VPS.
3. Run:
   ```
   ntfy user add --role=user grimnir-page-eu-west
   ntfy access grimnir-page-eu-west grimnir-region-eu-west-page rw
   ntfy token add grimnir-page-eu-west
   # repeat for -rollback and -audit
   ```
4. Store the three tokens in the new region's secrets backend.
5. Subscribe phone to the three new topics.
```

- [ ] **Step 4: Commit**

```bash
git add ops/ntfy/topics.md
git commit -m "ops(ntfy): document topic conventions + token scopes"
```

### Task 3.3: Add backup of the ntfy user database

**Files:**
- Modify: `ops/ntfy/provision.sh`

**Context:**
`/var/cache/ntfy/user.db` holds users + tokens. Losing it means re-creating every user and re-distributing every token. Daily backup to an offsite location is cheap insurance.

- [ ] **Step 1: Add a daily backup cron to the provision script**

Append to `provision.sh`:

```bash
# 7. Daily backup of user.db to a remote
cat >/etc/cron.d/ntfy-backup <<'EOF'
0 4 * * * root rclone copy /var/cache/ntfy/user.db r2:grimnir-ntfy-backups/ --quiet
EOF
chmod 0644 /etc/cron.d/ntfy-backup
```

(Assumes `rclone` is installed and configured with R2 credentials on the ntfy VPS. If not, add an install step.)

- [ ] **Step 2: Test the backup**

```bash
ssh root@ntfy.grimnir.example "rclone copy /var/cache/ntfy/user.db r2:grimnir-ntfy-backups/ -v"
```

Expected: file uploaded.

- [ ] **Step 3: Commit**

```bash
git add ops/ntfy/provision.sh
git commit -m "ops(ntfy): add daily user.db backup to R2"
```

---
## Chunk 4: `internal/notify/` — ntfy client + tier abstraction

Pure Go. One client struct, three exported methods matching the three tiers from Section 8.1.

### Task 4.1: Define the client + config

**Files:**
- Create: `internal/notify/config.go`
- Test: `internal/notify/config_test.go`

- [ ] **Step 1: Write the failing test**

```go
package notify

import (
	"os"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("GRIMNIR_NTFY_URL", "https://ntfy.example")
	t.Setenv("GRIMNIR_NTFY_TOKEN_PAGE", "tk_page")
	t.Setenv("GRIMNIR_NTFY_TOKEN_AUDIT", "tk_audit")
	t.Setenv("GRIMNIR_NTFY_TOKEN_ROLLBACK", "tk_rollback")
	t.Setenv("GRIMNIR_REGION", "us-east")

	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL != "https://ntfy.example" {
		t.Errorf("base url = %q", cfg.BaseURL)
	}
	if cfg.PageTopic() != "grimnir-region-us-east-page" {
		t.Errorf("page topic = %q", cfg.PageTopic())
	}
	if cfg.AuditTopic() != "grimnir-audit-us-east" {
		t.Errorf("audit topic = %q", cfg.AuditTopic())
	}
	if cfg.RollbackTopic() != "grimnir-region-us-east-rollback" {
		t.Errorf("rollback topic = %q", cfg.RollbackTopic())
	}
}

func TestConfigFromEnv_MissingURLIsError(t *testing.T) {
	os.Unsetenv("GRIMNIR_NTFY_URL")
	if _, err := LoadConfigFromEnv(); err == nil {
		t.Error("expected error when URL unset")
	}
}

func TestConfigFromEnv_MissingRegionDefaultsToDefault(t *testing.T) {
	t.Setenv("GRIMNIR_NTFY_URL", "https://x")
	t.Setenv("GRIMNIR_NTFY_TOKEN_PAGE", "tk")
	t.Setenv("GRIMNIR_NTFY_TOKEN_AUDIT", "tk")
	t.Setenv("GRIMNIR_NTFY_TOKEN_ROLLBACK", "tk")
	os.Unsetenv("GRIMNIR_REGION")
	cfg, err := LoadConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Region != "default" {
		t.Errorf("region = %q, want default", cfg.Region)
	}
}
```

- [ ] **Step 2: Run, fail, implement**

`internal/notify/config.go`:

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package notify

import (
	"fmt"
	"os"
)

type Config struct {
	BaseURL       string
	Region        string
	PageToken     string
	AuditToken    string
	RollbackToken string
}

func (c Config) PageTopic() string     { return "grimnir-region-" + c.Region + "-page" }
func (c Config) AuditTopic() string    { return "grimnir-audit-" + c.Region }
func (c Config) RollbackTopic() string { return "grimnir-region-" + c.Region + "-rollback" }

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		BaseURL:       os.Getenv("GRIMNIR_NTFY_URL"),
		Region:        getEnvDefault("GRIMNIR_REGION", "default"),
		PageToken:     os.Getenv("GRIMNIR_NTFY_TOKEN_PAGE"),
		AuditToken:    os.Getenv("GRIMNIR_NTFY_TOKEN_AUDIT"),
		RollbackToken: os.Getenv("GRIMNIR_NTFY_TOKEN_ROLLBACK"),
	}
	if cfg.BaseURL == "" {
		return cfg, fmt.Errorf("notify: GRIMNIR_NTFY_URL is required")
	}
	return cfg, nil
}

func getEnvDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 3: Test, commit**

```bash
go test ./internal/notify/...
git add internal/notify/config.go internal/notify/config_test.go
git commit -m "feat(notify): config loader for ntfy client"
```

### Task 4.2: Implement the client

**Files:**
- Create: `internal/notify/notify.go`
- Test: `internal/notify/notify_test.go`

**Context:**
ntfy's HTTP API is a `POST /<topic>` with the body as the message and headers for title, priority, tags, attach, click. Token auth via `Authorization: Bearer <token>`.

Three exported methods match the three tiers. The internal `post()` does the HTTP work; the three methods set tier-specific defaults (topic, priority, tags).

- [ ] **Step 1: Write the failing test**

```go
package notify

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capturedRequest struct {
	Path        string
	Body        string
	AuthHeader  string
	TitleHeader string
	PrioHeader  string
	TagsHeader  string
}

func newFakeNtfy(t *testing.T, capture *capturedRequest) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capture.Path = r.URL.Path
		capture.Body = string(body)
		capture.AuthHeader = r.Header.Get("Authorization")
		capture.TitleHeader = r.Header.Get("Title")
		capture.PrioHeader = r.Header.Get("Priority")
		capture.TagsHeader = r.Header.Get("Tags")
		w.WriteHeader(http.StatusOK)
	}))
}

func TestClient_Notify_HitsAuditTopic(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeNtfy(t, cap)
	defer srv.Close()

	c := NewClient(Config{
		BaseURL: srv.URL, Region: "default",
		AuditToken: "tk_audit",
	})
	if err := c.Notify(context.Background(), Message{Title: "test", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/grimnir-audit-default" {
		t.Errorf("path = %q", cap.Path)
	}
	if cap.AuthHeader != "Bearer tk_audit" {
		t.Errorf("auth = %q", cap.AuthHeader)
	}
	if cap.Body != "hi" {
		t.Errorf("body = %q", cap.Body)
	}
	if cap.PrioHeader != "3" {
		t.Errorf("priority = %q, want 3 for Notify", cap.PrioHeader)
	}
}

func TestClient_Page_HitsPageTopicAtHighPriority(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeNtfy(t, cap)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk_page"})
	if err := c.Page(context.Background(), Message{Title: "wake up", Body: "engines dead"}); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/grimnir-region-default-page" {
		t.Errorf("path = %q", cap.Path)
	}
	if cap.PrioHeader != "5" {
		t.Errorf("priority = %q, want 5 (max) for Page", cap.PrioHeader)
	}
	if !strings.Contains(cap.TagsHeader, "rotating_light") {
		t.Errorf("tags missing rotating_light: %q", cap.TagsHeader)
	}
}

func TestClient_PageAndRollback_HitsRollbackTopic(t *testing.T) {
	cap := &capturedRequest{}
	srv := newFakeNtfy(t, cap)
	defer srv.Close()

	c := NewClient(Config{BaseURL: srv.URL, Region: "default", RollbackToken: "tk_roll"})
	if err := c.PageAndRollback(context.Background(), Message{Body: "auto-rollback fired"}); err != nil {
		t.Fatal(err)
	}
	if cap.Path != "/grimnir-region-default-rollback" {
		t.Errorf("path = %q", cap.Path)
	}
	if cap.AuthHeader != "Bearer tk_roll" {
		t.Errorf("auth = %q", cap.AuthHeader)
	}
}

func TestClient_NetworkErrorReturned(t *testing.T) {
	c := NewClient(Config{BaseURL: "http://127.0.0.1:1", Region: "default", PageToken: "tk"})
	err := c.Page(context.Background(), Message{Body: "x"})
	if err == nil {
		t.Error("expected network error")
	}
}

func TestClient_Non2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk"})
	err := c.Page(context.Background(), Message{Body: "x"})
	if err == nil {
		t.Error("expected 401 error")
	}
}
```

- [ ] **Step 2: Run, see fail, implement**

`internal/notify/notify.go`:

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package notify provides a typed client for the self-hosted ntfy.sh server.
//
// Three exported methods match the three alert tiers in Section 8.1 of the
// HA design:
//
//   - Notify           — tier-1 (informational, audit-grade)
//   - Page             — tier-2 (wake the operator)
//   - PageAndRollback  — tier-3 (operator is informed; the system has
//                                 already triggered grimnir-deploy --rollback)
//
// The tier abstraction lives in the caller. The client just routes each
// method to the configured per-region topic with the right priority.
package notify

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Title   string   // ntfy "Title" header
	Body    string   // request body
	Tags    []string // ntfy "Tags" header (emoji short names)
	Click   string   // optional URL on tap
	Actions []string // optional ntfy actions (max 3)
}

type Client struct {
	cfg  Config
	http *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) Notify(ctx context.Context, m Message) error {
	return c.post(ctx, c.cfg.AuditTopic(), c.cfg.AuditToken, m, 3, []string{"information_source"})
}

func (c *Client) Page(ctx context.Context, m Message) error {
	return c.post(ctx, c.cfg.PageTopic(), c.cfg.PageToken, m, 5, []string{"rotating_light"})
}

func (c *Client) PageAndRollback(ctx context.Context, m Message) error {
	return c.post(ctx, c.cfg.RollbackTopic(), c.cfg.RollbackToken, m, 5, []string{"arrows_counterclockwise", "rotating_light"})
}

func (c *Client) post(ctx context.Context, topic, token string, m Message, prio int, defaultTags []string) error {
	url := strings.TrimRight(c.cfg.BaseURL, "/") + "/" + topic
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(m.Body))
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if m.Title != "" {
		req.Header.Set("Title", m.Title)
	}
	req.Header.Set("Priority", fmt.Sprintf("%d", prio))
	tags := m.Tags
	if len(tags) == 0 {
		tags = defaultTags
	}
	req.Header.Set("Tags", strings.Join(tags, ","))
	if m.Click != "" {
		req.Header.Set("Click", m.Click)
	}
	for _, a := range m.Actions {
		req.Header.Add("Actions", a)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("notify: post %s: %w", topic, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("notify: %s returned %d: %s", topic, resp.StatusCode, string(body))
	}
	return nil
}
```

- [ ] **Step 3: Test, commit**

```bash
go test ./internal/notify/...
git add internal/notify/notify.go internal/notify/notify_test.go
git commit -m "feat(notify): typed ntfy client with tier-based methods"
```

### Task 4.3: Retry + circuit breaker

**Files:**
- Modify: `internal/notify/notify.go`

**Context:**
The notify client is in the failure path for several alerts. If the ntfy server is briefly down, a single attempt fails and the alert is lost. Add a short retry (3 attempts, exponential backoff capped at 2s) for transient errors. Don't retry on 4xx — those are config errors and won't recover.

If the ntfy server is sustained-down (10+ consecutive failures), open a circuit breaker and log loudly (zerolog `Error`). The breaker resets on the next successful call. The point isn't graceful degradation — losing pages is unacceptable — but to avoid blocking the caller forever. The caller is expected to time out independently.

- [ ] **Step 1: Write the failing tests**

```go
func TestClient_RetriesOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk"})
	if err := c.Page(context.Background(), Message{Body: "x"}); err != nil {
		t.Fatalf("expected success after retries: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestClient_DoesNotRetryOn4xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := NewClient(Config{BaseURL: srv.URL, Region: "default", PageToken: "tk"})
	_ = c.Page(context.Background(), Message{Body: "x"})
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1 (no retry on 401)", attempts)
	}
}
```

- [ ] **Step 2: Implement retry wrapper around `post`**

Add a `postWithRetry` that wraps `post`. 3 attempts, backoff 200ms / 500ms / 1500ms. Skip retry on 4xx (parse the error message — fragile but the alternative is rebuilding the response).

Better: change `post` to return a typed error including the status code, then `postWithRetry` inspects it.

- [ ] **Step 3: Test, commit**

```bash
go test ./internal/notify/...
git add internal/notify/notify.go internal/notify/notify_test.go
git commit -m "feat(notify): add retry on 5xx, no retry on 4xx"
```

---
## Chunk 5: audit_log table + writer

> **Status:** Shipped in B-2 Chunk 1 (commit fa8646a). The `audit_log` table, GORM model, Recorder, & cobra middleware all live under `internal/grimnirdeploy/audit/`. No work remains in this chunk; future-reader, read the B-2 plan for the as-shipped design.

### Task 5.1: Migration for `audit_log` table

**Files:**
- Create: `migrations/NNN_audit_log.sql` (NNN = next free number, verify with `ls migrations/`)
- Create: `migrations/NNN_audit_log_down.sql`

**Context:**
Schema is dictated by Section 8.3. Expand-only per Track B-1: no DROP COLUMN, no ALTER TYPE on existing columns. The table is brand new so this constraint is trivial here, but it matters when the table evolves.

- [ ] **Step 1: Identify the next migration number**

```bash
ls migrations/ | sort -V | tail -5
```

Pick the next free number; this plan uses `047` as a placeholder. Verify and replace at execution time.

- [ ] **Step 2: Write the up migration**

`migrations/047_audit_log.sql`:

```sql
-- Track B-4 audit log per Section 8.3 of the HA design.
-- See docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md.

CREATE TABLE IF NOT EXISTS audit_log (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ts           TIMESTAMPTZ NOT NULL DEFAULT now(),
    operator     TEXT NOT NULL,
    source_ip    TEXT NOT NULL DEFAULT '',
    subcommand   TEXT NOT NULL,
    args_json    JSONB NOT NULL DEFAULT '{}'::jsonb,
    phase        TEXT NOT NULL CHECK (phase IN ('started', 'completed', 'failed')),
    outcome      TEXT,
    duration_ms  BIGINT,
    notes        TEXT,
    -- Correlation id so a 'started' row and a 'completed' row for the same
    -- invocation can be paired up. Set by the writer; not user-provided.
    correlation_id UUID NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_log_ts ON audit_log (ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_operator ON audit_log (operator);
CREATE INDEX IF NOT EXISTS idx_audit_log_correlation ON audit_log (correlation_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_subcommand ON audit_log (subcommand);

COMMENT ON TABLE audit_log IS 'Operator-action audit log per Section 8.3 of the HA design. Writer: internal/audit/writer.go.';
```

> **gen_random_uuid:** requires the `pgcrypto` extension. The grimnir DB already enables it for other tables — if not, add `CREATE EXTENSION IF NOT EXISTS pgcrypto;` at the top of the migration.

- [ ] **Step 3: Write the down migration**

`migrations/047_audit_log_down.sql`:

```sql
DROP TABLE IF EXISTS audit_log;
```

> Down migrations are manual-only in this repo. They exist for emergency rollback during the dev cycle; never run in prod.

- [ ] **Step 4: Run the migration against dev DB**

```bash
make dev-stack
psql "$GRIMNIR_DB_DSN" -f migrations/047_audit_log.sql
psql "$GRIMNIR_DB_DSN" -c "\d audit_log"
```

Expected: table created, indexes present.

- [ ] **Step 5: Verify expand/contract lint passes**

```bash
make ci   # includes the Track B-1 migration lint
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add migrations/047_audit_log.sql migrations/047_audit_log_down.sql
git commit -m "feat(audit): create audit_log table per Section 8.3"
```

### Task 5.2: GORM model

**Files:**
- Create: `internal/models/audit_log.go`

- [ ] **Step 1: Write the failing test**

```go
package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAuditLogTableName(t *testing.T) {
	a := AuditLog{}
	if a.TableName() != "audit_log" {
		t.Errorf("table = %q", a.TableName())
	}
}

func TestAuditLogValidatesPhase(t *testing.T) {
	cases := []struct {
		phase string
		ok    bool
	}{
		{"started", true},
		{"completed", true},
		{"failed", true},
		{"in_progress", false},
		{"", false},
	}
	for _, tc := range cases {
		a := AuditLog{
			ID:            uuid.New(),
			TS:            time.Now(),
			Operator:      "x",
			Subcommand:    "test",
			Phase:         tc.phase,
			CorrelationID: uuid.New(),
		}
		err := a.Validate()
		if tc.ok && err != nil {
			t.Errorf("phase=%q expected ok, got %v", tc.phase, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("phase=%q expected error", tc.phase)
		}
	}
}
```

- [ ] **Step 2: Implement**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package models

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type AuditLog struct {
	ID            uuid.UUID       `gorm:"type:uuid;primaryKey" json:"id"`
	TS            time.Time       `gorm:"column:ts;not null;index:idx_audit_log_ts,sort:desc" json:"ts"`
	Operator      string          `gorm:"not null;index" json:"operator"`
	SourceIP      string          `gorm:"column:source_ip;not null;default:''" json:"source_ip"`
	Subcommand    string          `gorm:"not null;index" json:"subcommand"`
	ArgsJSON      json.RawMessage `gorm:"column:args_json;type:jsonb;not null;default:'{}'::jsonb" json:"args_json"`
	Phase         string          `gorm:"not null" json:"phase"`
	Outcome       *string         `json:"outcome,omitempty"`
	DurationMS    *int64          `gorm:"column:duration_ms" json:"duration_ms,omitempty"`
	Notes         *string         `json:"notes,omitempty"`
	CorrelationID uuid.UUID       `gorm:"column:correlation_id;type:uuid;not null;index" json:"correlation_id"`
}

func (AuditLog) TableName() string { return "audit_log" }

func (a AuditLog) Validate() error {
	switch a.Phase {
	case "started", "completed", "failed":
	default:
		return fmt.Errorf("audit: invalid phase %q (want started|completed|failed)", a.Phase)
	}
	if a.Operator == "" {
		return fmt.Errorf("audit: operator required")
	}
	if a.Subcommand == "" {
		return fmt.Errorf("audit: subcommand required")
	}
	return nil
}
```

- [ ] **Step 3: Test, commit**

```bash
go test ./internal/models/... -run AuditLog
git add internal/models/audit_log.go internal/models/audit_log_test.go
git commit -m "feat(models): add AuditLog GORM model"
```

### Task 5.3: Argument redaction

**Files:**
- Create: `internal/audit/redact.go`
- Test: `internal/audit/redact_test.go`

**Context:**
The writer captures `args_json`, which often contains secrets (passwords, tokens, signing keys). Redaction walks the args map and replaces values whose KEY matches a known-secret pattern. Conservative on false positives: any key containing "password", "secret", "token", "key", "credential", "auth" is redacted. Numeric values, even at suspicious keys, are NOT redacted (a "port_key" is fine to log).

- [ ] **Step 1: Write the failing test**

```go
package audit

import (
	"encoding/json"
	"testing"
)

func TestRedact_FlatMap(t *testing.T) {
	in := map[string]interface{}{
		"region":   "us-east",
		"password": "hunter2",
		"token":    "tk_secret",
		"api_key":  "kk",
		"port":     8080,
	}
	out := Redact(in)
	for _, k := range []string{"password", "token", "api_key"} {
		if out[k] != "<redacted>" {
			t.Errorf("%s = %v, want <redacted>", k, out[k])
		}
	}
	if out["region"] != "us-east" {
		t.Errorf("region was redacted")
	}
	if out["port"] != 8080 {
		t.Errorf("port = %v, want 8080", out["port"])
	}
}

func TestRedact_NestedMap(t *testing.T) {
	in := map[string]interface{}{
		"db": map[string]interface{}{
			"host":     "localhost",
			"password": "x",
		},
	}
	out := Redact(in)
	nested := out["db"].(map[string]interface{})
	if nested["host"] != "localhost" {
		t.Error("nested host was redacted")
	}
	if nested["password"] != "<redacted>" {
		t.Error("nested password was not redacted")
	}
}

func TestRedact_FromJSON(t *testing.T) {
	raw := json.RawMessage(`{"username":"alice","secret_key":"shh"}`)
	out, err := RedactJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(out), `"username":"alice"`) {
		t.Errorf("username missing in %s", string(out))
	}
	if !contains(string(out), `"secret_key":"<redacted>"`) {
		t.Errorf("secret_key not redacted in %s", string(out))
	}
}

func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }
```

(Add `strings` import.)

- [ ] **Step 2: Implement**

```go
package audit

import (
	"encoding/json"
	"regexp"
)

var secretKeyPattern = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key|credential|auth|signing)`)

// Redact walks an args map and replaces any string value at a secret-named
// key with "<redacted>". Non-string values at secret-named keys are kept
// as-is (numbers, booleans, port numbers).
func Redact(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args))
	for k, v := range args {
		switch vv := v.(type) {
		case map[string]interface{}:
			out[k] = Redact(vv)
		case string:
			if secretKeyPattern.MatchString(k) {
				out[k] = "<redacted>"
			} else {
				out[k] = vv
			}
		default:
			out[k] = v
		}
	}
	return out
}

func RedactJSON(raw json.RawMessage) (json.RawMessage, error) {
	var args map[string]interface{}
	if err := json.Unmarshal(raw, &args); err != nil {
		return nil, err
	}
	return json.Marshal(Redact(args))
}
```

- [ ] **Step 3: Test, commit**

```bash
go test ./internal/audit/... -run Redact
git add internal/audit/redact.go internal/audit/redact_test.go
git commit -m "feat(audit): add secret-key redaction for args_json"
```

### Task 5.4: Writer with start/complete API

**Files:**
- Create: `internal/audit/writer.go`
- Test: `internal/audit/writer_test.go`

**Context:**
Per Section 8.2: "Every subcommand supports `--dry-run`, has a `--help` describing the procedure, writes an audit log entry, and posts an ntfy notification on completion." The writer captures both the START of an action (intent + args) and its COMPLETION (outcome + duration). The same correlation_id ties them together.

API shape:

```go
action := writer.Start(ctx, "promote-replica", args)   // writes phase=started row
defer action.Complete(ctx, "ok", "")                   // writes phase=completed row
// ... do work ...
action.Fail(ctx, err.Error())                          // alternative: phase=failed
```

ntfy notification fires on Complete/Fail, not on Start (to avoid double-notification noise).

- [ ] **Step 1: Write the failing test**

```go
package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

type fakeNotifier struct {
	calls []string
}

func (f *fakeNotifier) Notify(ctx context.Context, msg notifyMessage) error {
	f.calls = append(f.calls, msg.Body)
	return nil
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.AuditLog{}))
	return db
}

func TestWriter_StartThenComplete(t *testing.T) {
	db := newTestDB(t)
	notifier := &fakeNotifier{}
	w := NewWriter(db, notifier, "alice")

	action := w.Start(context.Background(), "promote-replica",
		map[string]interface{}{"region": "us-east"})

	require.NotEqual(t, uuid.Nil, action.CorrelationID)

	var rows []models.AuditLog
	require.NoError(t, db.Order("ts asc").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, "started", rows[0].Phase)
	require.Equal(t, "alice", rows[0].Operator)

	time.Sleep(2 * time.Millisecond) // ensure duration > 0
	action.Complete(context.Background(), "ok", "promoted node-2")

	require.NoError(t, db.Order("ts asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "completed", rows[1].Phase)
	require.Equal(t, rows[0].CorrelationID, rows[1].CorrelationID)
	require.Greater(t, *rows[1].DurationMS, int64(0))

	require.Len(t, notifier.calls, 1, "notifier called once on Complete")
	require.Contains(t, notifier.calls[0], "promote-replica")
	require.Contains(t, notifier.calls[0], "alice")
}

func TestWriter_RedactsArgs(t *testing.T) {
	db := newTestDB(t)
	w := NewWriter(db, &fakeNotifier{}, "bob")
	w.Start(context.Background(), "rotate-secret",
		map[string]interface{}{"name": "vault-token", "new_value": "hunter2"})

	var rows []models.AuditLog
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 1)
	require.NotContains(t, string(rows[0].ArgsJSON), "hunter2")
}

func TestWriter_FailWritesFailedRow(t *testing.T) {
	db := newTestDB(t)
	notifier := &fakeNotifier{}
	w := NewWriter(db, notifier, "carol")

	action := w.Start(context.Background(), "drain", nil)
	action.Fail(context.Background(), errors.New("VRRP would not yield"))

	var rows []models.AuditLog
	require.NoError(t, db.Order("ts asc").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.Equal(t, "failed", rows[1].Phase)
	require.Equal(t, "VRRP would not yield", *rows[1].Outcome)
}

func TestWriter_NotifierFailureDoesNotBlockDBWrite(t *testing.T) {
	db := newTestDB(t)
	bad := &failingNotifier{}
	w := NewWriter(db, bad, "dave")

	action := w.Start(context.Background(), "verify", nil)
	action.Complete(context.Background(), "ok", "")

	var count int64
	db.Model(&models.AuditLog{}).Where("phase = ?", "completed").Count(&count)
	require.Equal(t, int64(1), count, "DB write succeeded despite notifier failure")
}

type failingNotifier struct{}

func (f *failingNotifier) Notify(ctx context.Context, _ notifyMessage) error {
	return errors.New("ntfy down")
}
```

- [ ] **Step 2: Implement**

`internal/audit/writer.go`:

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// notifyMessage is the writer's internal view of a notification payload.
// Decoupled from internal/notify.Message so the writer doesn't import notify
// (avoids cycles when notify wants to log via audit later).
type notifyMessage struct {
	Title string
	Body  string
}

type Notifier interface {
	Notify(ctx context.Context, msg notifyMessage) error
}

type Writer struct {
	db       *gorm.DB
	notifier Notifier
	operator string
}

func NewWriter(db *gorm.DB, notifier Notifier, operator string) *Writer {
	return &Writer{db: db, notifier: notifier, operator: operator}
}

type Action struct {
	w             *Writer
	CorrelationID uuid.UUID
	Subcommand    string
	StartedAt     time.Time
}

// Start writes the "started" row and returns an Action handle.
// Args is redacted before storage.
func (w *Writer) Start(ctx context.Context, subcommand string, args map[string]interface{}) *Action {
	cid := uuid.New()
	now := time.Now().UTC()

	argsJSON, err := json.Marshal(Redact(args))
	if err != nil {
		argsJSON = []byte(`{}`)
	}

	row := &models.AuditLog{
		ID:            uuid.New(),
		TS:            now,
		Operator:      w.operator,
		Subcommand:    subcommand,
		ArgsJSON:      argsJSON,
		Phase:         "started",
		CorrelationID: cid,
	}
	if err := w.db.WithContext(ctx).Create(row).Error; err != nil {
		log.Error().Err(err).Str("subcommand", subcommand).Msg("audit start write failed")
	}

	return &Action{
		w:             w,
		CorrelationID: cid,
		Subcommand:    subcommand,
		StartedAt:     now,
	}
}

func (a *Action) Complete(ctx context.Context, outcome, notes string) {
	a.writeCompletion(ctx, "completed", outcome, notes)
}

func (a *Action) Fail(ctx context.Context, err error) {
	a.writeCompletion(ctx, "failed", err.Error(), "")
}

func (a *Action) writeCompletion(ctx context.Context, phase, outcome, notes string) {
	durMS := time.Since(a.StartedAt).Milliseconds()
	row := &models.AuditLog{
		ID:            uuid.New(),
		TS:            time.Now().UTC(),
		Operator:      a.w.operator,
		Subcommand:    a.Subcommand,
		ArgsJSON:      []byte(`{}`),
		Phase:         phase,
		Outcome:       &outcome,
		DurationMS:    &durMS,
		Notes:         strPtr(notes),
		CorrelationID: a.CorrelationID,
	}
	if err := a.w.db.WithContext(ctx).Create(row).Error; err != nil {
		log.Error().Err(err).Str("subcommand", a.Subcommand).Msg("audit complete write failed")
		// Fall through — still fire notification.
	}

	// Best-effort notification. Failure logs but does not propagate.
	msg := notifyMessage{
		Title: fmt.Sprintf("[grimnir] %s %s by %s", a.Subcommand, phase, a.w.operator),
		Body:  fmt.Sprintf("subcommand=%s phase=%s outcome=%s duration_ms=%d notes=%s", a.Subcommand, phase, outcome, durMS, notes),
	}
	if err := a.w.notifier.Notify(ctx, msg); err != nil {
		log.Warn().Err(err).Str("subcommand", a.Subcommand).Msg("audit ntfy failed")
	}
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
```

- [ ] **Step 3: Adapter to the real ntfy client**

`internal/audit/notifier_adapter.go`:

```go
package audit

import (
	"context"

	"github.com/friendsincode/grimnir_radio/internal/notify"
)

type ntfyAdapter struct {
	c *notify.Client
}

func NewNotifyAdapter(c *notify.Client) Notifier {
	return &ntfyAdapter{c: c}
}

func (n *ntfyAdapter) Notify(ctx context.Context, m notifyMessage) error {
	return n.c.Notify(ctx, notify.Message{Title: m.Title, Body: m.Body})
}
```

- [ ] **Step 4: Test, commit**

```bash
go test ./internal/audit/...
git add internal/audit/writer.go internal/audit/writer_test.go internal/audit/notifier_adapter.go
git commit -m "feat(audit): operator-action writer with start/complete/fail + ntfy"
```

### Task 5.5: Bump version

**Files:**
- Modify: `internal/version/version.go`

- [ ] **Step 1: Bump to v2.0.0-alpha.5**

```go
var Version = "2.0.0-alpha.5"
```

- [ ] **Step 2: Commit + tag + push per CLAUDE.md**

```bash
git add internal/version/version.go && \
  git commit -m "Bump to v2.0.0-alpha.5: audit writer + metrics package (v2-dev)" && \
  git tag -a v2.0.0-alpha.5 -m "v2-dev: audit writer + HA metrics" && \
  git push origin v2-dev && git push origin v2.0.0-alpha.5
```

---
## Chunk 6: `internal/secrets/` — pluggable backend with .env + Vault

### Task 6.1: Backend interface + factory

**Files:**
- Create: `internal/secrets/secrets.go`
- Test: `internal/secrets/secrets_test.go`

**Context:**
Two backends ship: `.env` (always supported, single-instance baseline per Q6) and Vault (optional, HA-friendly). Selected per-region via `GRIMNIR_SECRETS_BACKEND=env|vault`. The interface is small on purpose: Get, Put, List, Rotate. No bulk-export (would be a security smell), no watch (callers re-read on rotation events).

Interface contract tests run against EVERY backend, parameterized. This guarantees both backends honor the same semantics.

- [ ] **Step 1: Define the interface**

`internal/secrets/secrets.go`:

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package secrets is a pluggable secret-store abstraction.
//
// Two backends ship in phase 1:
//
//   - env   — .env file (default; matches single-instance + local-disk philosophy)
//   - vault — HashiCorp Vault KV v2 with AppRole auth
//
// Backend is selected via GRIMNIR_SECRETS_BACKEND. Both backends honor the
// same Backend interface so callers never branch on the backend type.
package secrets

import (
	"context"
	"errors"
	"fmt"
	"os"
)

var ErrNotFound = errors.New("secrets: not found")

type Backend interface {
	Get(ctx context.Context, name string) (string, error)
	Put(ctx context.Context, name, value string) error
	List(ctx context.Context, prefix string) ([]string, error)
	// Rotate stages a new value, verifies it via the verifier callback,
	// then commits. On verifier failure the old value is restored.
	// Returns the OLD value for emergency manual restore by the caller.
	Rotate(ctx context.Context, name, newValue string, verify func(ctx context.Context, candidate string) error) (oldValue string, err error)
	// Close releases backend resources (file handles, Vault tokens, etc).
	Close() error
}

func Open(ctx context.Context) (Backend, error) {
	backend := os.Getenv("GRIMNIR_SECRETS_BACKEND")
	if backend == "" {
		backend = "env"
	}
	switch backend {
	case "env":
		path := os.Getenv("GRIMNIR_SECRETS_ENV_FILE")
		if path == "" {
			path = ".env"
		}
		return NewEnvBackend(path)
	case "vault":
		return NewVaultBackend(ctx, VaultConfig{
			Address:     os.Getenv("VAULT_ADDR"),
			RoleID:      os.Getenv("VAULT_ROLE_ID"),
			SecretID:    os.Getenv("VAULT_SECRET_ID"),
			MountPath:   getEnvDefault("VAULT_MOUNT", "secret"),
			PathPrefix:  getEnvDefault("VAULT_PATH_PREFIX", "grimnir"),
		})
	default:
		return nil, fmt.Errorf("secrets: unknown backend %q", backend)
	}
}

func getEnvDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
```

- [ ] **Step 2: Write the contract test**

`internal/secrets/secrets_test.go`:

```go
package secrets

import (
	"context"
	"errors"
	"sort"
	"testing"
)

// backendFactory returns a fresh backend for each test. Each implementation
// registers a factory here; the contract suite runs against each.
type backendFactory struct {
	name string
	make func(t *testing.T) Backend
}

func allBackends(t *testing.T) []backendFactory {
	t.Helper()
	return []backendFactory{
		{"env", func(t *testing.T) Backend {
			path := t.TempDir() + "/.env"
			b, err := NewEnvBackend(path)
			if err != nil {
				t.Fatal(err)
			}
			return b
		}},
		// vault factory added in Task 6.3.
	}
}

func TestContract_PutThenGet(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			if err := b.Put(ctx, "FOO", "bar"); err != nil {
				t.Fatal(err)
			}
			got, err := b.Get(ctx, "FOO")
			if err != nil {
				t.Fatal(err)
			}
			if got != "bar" {
				t.Errorf("got %q, want bar", got)
			}
		})
	}
}

func TestContract_MissingReturnsErrNotFound(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			_, err := b.Get(context.Background(), "DOES_NOT_EXIST")
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("err = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestContract_List(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			b.Put(ctx, "A_ONE", "1")
			b.Put(ctx, "A_TWO", "2")
			b.Put(ctx, "B_ONE", "3")
			got, _ := b.List(ctx, "A_")
			sort.Strings(got)
			if len(got) != 2 || got[0] != "A_ONE" || got[1] != "A_TWO" {
				t.Errorf("list = %v, want [A_ONE A_TWO]", got)
			}
		})
	}
}

func TestContract_RotateSuccess(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			b.Put(ctx, "KEY", "old")
			old, err := b.Rotate(ctx, "KEY", "new", func(_ context.Context, v string) error {
				if v != "new" {
					t.Errorf("verifier got %q", v)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if old != "old" {
				t.Errorf("old = %q, want old", old)
			}
			now, _ := b.Get(ctx, "KEY")
			if now != "new" {
				t.Errorf("after rotate, got %q, want new", now)
			}
		})
	}
}

func TestContract_RotateVerifierFailureRollsBack(t *testing.T) {
	for _, bf := range allBackends(t) {
		t.Run(bf.name, func(t *testing.T) {
			b := bf.make(t)
			defer b.Close()
			ctx := context.Background()
			b.Put(ctx, "KEY", "old")
			_, err := b.Rotate(ctx, "KEY", "new", func(_ context.Context, _ string) error {
				return errors.New("does not authenticate")
			})
			if err == nil {
				t.Error("expected rotate error")
			}
			got, _ := b.Get(ctx, "KEY")
			if got != "old" {
				t.Errorf("after failed rotate, got %q, want old (rolled back)", got)
			}
		})
	}
}
```

- [ ] **Step 3: Test (will fail until 6.2 implements env backend)**

```bash
go test ./internal/secrets/...
```

Expected: FAIL (no env backend yet).

- [ ] **Step 4: Commit the interface + tests**

```bash
git add internal/secrets/secrets.go internal/secrets/secrets_test.go
git commit -m "feat(secrets): pluggable backend interface + contract tests"
```

### Task 6.2: .env backend

**Files:**
- Create: `internal/secrets/env_backend.go`
- Test: `internal/secrets/env_backend_test.go`

**Context:**
Atomic file rewrite to avoid partial-write corruption. Use `os.WriteFile` to a temp file in the same directory, then `os.Rename`. Same-FS rename is atomic on Linux. Use `flock` to serialize concurrent writes from multiple grimnir processes on the same host (only single-instance mode uses .env, but defensive).

`Get` reads from process env first (so operator-overridden values via `export FOO=bar` win), falls back to the file. `Put` only writes to the file; process env is never mutated.

- [ ] **Step 1: Implement**

`internal/secrets/env_backend.go`:

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package secrets

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/joho/godotenv"
)

type EnvBackend struct {
	path string
	mu   sync.Mutex // serializes file rewrites within the process
}

func NewEnvBackend(path string) (*EnvBackend, error) {
	// Touch the file if it doesn't exist; downstream operations assume readable.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("secrets/env: create %s: %w", path, err)
		}
		f.Close()
	}
	return &EnvBackend{path: path}, nil
}

func (e *EnvBackend) Close() error { return nil }

func (e *EnvBackend) Get(ctx context.Context, name string) (string, error) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v, nil
	}
	kv, err := godotenv.Read(e.path)
	if err != nil {
		return "", fmt.Errorf("secrets/env: read %s: %w", e.path, err)
	}
	v, ok := kv[name]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (e *EnvBackend) Put(ctx context.Context, name, value string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rewrite(func(kv map[string]string) {
		kv[name] = value
	})
}

func (e *EnvBackend) List(ctx context.Context, prefix string) ([]string, error) {
	kv, err := godotenv.Read(e.path)
	if err != nil {
		return nil, err
	}
	var out []string
	for k := range kv {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (e *EnvBackend) Rotate(ctx context.Context, name, newValue string, verify func(context.Context, string) error) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	kv, err := godotenv.Read(e.path)
	if err != nil {
		return "", err
	}
	old := kv[name]

	// Stage: write new value to file.
	if err := e.rewriteUnlocked(func(m map[string]string) { m[name] = newValue }); err != nil {
		return old, err
	}

	// Verify.
	if err := verify(ctx, newValue); err != nil {
		// Roll back.
		if rbErr := e.rewriteUnlocked(func(m map[string]string) { m[name] = old }); rbErr != nil {
			return old, fmt.Errorf("verify failed (%v) AND rollback failed (%v)", err, rbErr)
		}
		return old, fmt.Errorf("verify failed; rolled back: %w", err)
	}
	return old, nil
}

func (e *EnvBackend) rewrite(mutate func(map[string]string)) error {
	return e.rewriteUnlocked(mutate)
}

func (e *EnvBackend) rewriteUnlocked(mutate func(map[string]string)) error {
	kv, err := godotenv.Read(e.path)
	if err != nil {
		return err
	}
	mutate(kv)

	// Atomic: write to temp file, fsync, rename over original. flock the
	// original to serialize against other processes.
	f, err := os.OpenFile(e.path, os.O_RDONLY, 0600)
	if err == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		defer func() {
			_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
		}()
	}

	tmp := e.path + ".tmp"
	if err := godotenv.Write(kv, tmp); err != nil {
		return err
	}
	// Set tight perms before rename (godotenv.Write may use defaults).
	_ = os.Chmod(tmp, 0600)

	// fsync the temp file before rename.
	if tf, err := os.OpenFile(tmp, os.O_RDONLY, 0600); err == nil {
		_ = tf.Sync()
		tf.Close()
	}

	if err := os.Rename(tmp, e.path); err != nil {
		os.Remove(tmp)
		return err
	}

	// Best-effort fsync the directory.
	if dir, err := os.Open(filepath.Dir(e.path)); err == nil {
		_ = dir.Sync()
		dir.Close()
	}
	return nil
}
```

- [ ] **Step 2: Add env-specific tests**

`internal/secrets/env_backend_test.go`:

```go
package secrets

import (
	"context"
	"os"
	"testing"
)

func TestEnvBackend_ProcessEnvWinsOverFile(t *testing.T) {
	path := t.TempDir() + "/.env"
	b, _ := NewEnvBackend(path)
	defer b.Close()
	ctx := context.Background()
	b.Put(ctx, "FOO", "from-file")
	t.Setenv("FOO", "from-env")
	got, err := b.Get(ctx, "FOO")
	if err != nil {
		t.Fatal(err)
	}
	if got != "from-env" {
		t.Errorf("got %q, want from-env", got)
	}
}

func TestEnvBackend_FilePermissions(t *testing.T) {
	path := t.TempDir() + "/.env"
	b, _ := NewEnvBackend(path)
	defer b.Close()
	b.Put(context.Background(), "FOO", "bar")
	info, _ := os.Stat(path)
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("perm = %o, want 0600", mode)
	}
}

func TestEnvBackend_AtomicRewrite(t *testing.T) {
	// Inspect that no .tmp file is left after a successful Put.
	path := t.TempDir() + "/.env"
	b, _ := NewEnvBackend(path)
	defer b.Close()
	b.Put(context.Background(), "FOO", "bar")
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp left behind: %v", err)
	}
}
```

- [ ] **Step 3: Run the contract suite + env-specific tests**

```bash
go test ./internal/secrets/... -v
```

Expected: env tests + contract tests (under "env" subtest name) pass.

- [ ] **Step 4: Commit**

```bash
git add internal/secrets/env_backend.go internal/secrets/env_backend_test.go go.mod go.sum
git commit -m "feat(secrets): .env backend with atomic file rewrite"
```

### Task 6.3: Vault backend

**Files:**
- Create: `internal/secrets/vault_backend.go`
- Test: `internal/secrets/vault_backend_test.go`

**Context:**
KV v2 (versioned) so a rotation keeps the previous value retrievable for `--force-through-rotation`. AppRole auth. Path layout: `{mount}/data/{prefix}/{name}`.

The test spawns `vault server -dev` as a subprocess. Tests skip if `vault` is not on `$PATH` (CI runners install it; local skip is fine). Test parallel-safe via per-test unique mount path.

- [ ] **Step 1: Implement**

`internal/secrets/vault_backend.go`:

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package secrets

import (
	"context"
	"errors"
	"fmt"
	"strings"

	vault "github.com/hashicorp/vault/api"
	approle "github.com/hashicorp/vault/api/auth/approle"
)

type VaultConfig struct {
	Address    string
	RoleID     string
	SecretID   string
	MountPath  string // "secret" by default
	PathPrefix string // "grimnir" by default; secrets stored under {mount}/data/{prefix}/{name}
}

type VaultBackend struct {
	client *vault.Client
	cfg    VaultConfig
}

func NewVaultBackend(ctx context.Context, cfg VaultConfig) (*VaultBackend, error) {
	if cfg.Address == "" {
		return nil, errors.New("secrets/vault: VAULT_ADDR required")
	}
	if cfg.RoleID == "" || cfg.SecretID == "" {
		return nil, errors.New("secrets/vault: VAULT_ROLE_ID and VAULT_SECRET_ID required")
	}
	if cfg.MountPath == "" {
		cfg.MountPath = "secret"
	}
	if cfg.PathPrefix == "" {
		cfg.PathPrefix = "grimnir"
	}

	vc := vault.DefaultConfig()
	vc.Address = cfg.Address
	client, err := vault.NewClient(vc)
	if err != nil {
		return nil, fmt.Errorf("secrets/vault: client: %w", err)
	}

	auth, err := approle.NewAppRoleAuth(cfg.RoleID, &approle.SecretID{FromString: cfg.SecretID})
	if err != nil {
		return nil, fmt.Errorf("secrets/vault: approle: %w", err)
	}
	if _, err := client.Auth().Login(ctx, auth); err != nil {
		return nil, fmt.Errorf("secrets/vault: login: %w", err)
	}

	return &VaultBackend{client: client, cfg: cfg}, nil
}

func (v *VaultBackend) Close() error {
	v.client.ClearToken()
	return nil
}

func (v *VaultBackend) path(name string) string {
	return v.cfg.PathPrefix + "/" + name
}

func (v *VaultBackend) Get(ctx context.Context, name string) (string, error) {
	sec, err := v.client.KVv2(v.cfg.MountPath).Get(ctx, v.path(name))
	if err != nil {
		// Vault SDK returns a *ResponseError for 404; check by string.
		if strings.Contains(err.Error(), "404") {
			return "", ErrNotFound
		}
		return "", err
	}
	if sec == nil || sec.Data == nil {
		return "", ErrNotFound
	}
	val, ok := sec.Data["value"].(string)
	if !ok {
		return "", ErrNotFound
	}
	return val, nil
}

func (v *VaultBackend) Put(ctx context.Context, name, value string) error {
	_, err := v.client.KVv2(v.cfg.MountPath).Put(ctx, v.path(name), map[string]interface{}{
		"value": value,
	})
	return err
}

func (v *VaultBackend) List(ctx context.Context, prefix string) ([]string, error) {
	// KV v2 metadata list at the prefix path.
	listPath := strings.TrimSuffix(v.cfg.PathPrefix+"/"+prefix, "/")
	sec, err := v.client.Logical().ListWithContext(ctx, v.cfg.MountPath+"/metadata/"+listPath)
	if err != nil {
		return nil, err
	}
	if sec == nil {
		return nil, nil
	}
	keys, ok := sec.Data["keys"].([]interface{})
	if !ok {
		return nil, nil
	}
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if s, ok := k.(string); ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (v *VaultBackend) Rotate(ctx context.Context, name, newValue string, verify func(context.Context, string) error) (string, error) {
	old, err := v.Get(ctx, name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if err := v.Put(ctx, name, newValue); err != nil {
		return old, err
	}
	if err := verify(ctx, newValue); err != nil {
		// Roll back. (KV v2 keeps the prior version available; for simplicity
		// we re-Put the old value rather than calling the version-rollback API.)
		_ = v.Put(ctx, name, old)
		return old, fmt.Errorf("verify failed; rolled back: %w", err)
	}
	return old, nil
}
```

- [ ] **Step 2: Add the Vault test harness**

`internal/secrets/vault_backend_test.go`:

```go
package secrets

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func startDevVault(t *testing.T) (addr, roleID, secretID string) {
	t.Helper()
	if _, err := exec.LookPath("vault"); err != nil {
		t.Skip("vault binary not on PATH; skipping Vault backend tests")
	}
	// Spawn a dev server on a random port.
	// In practice, use TestMain to share one dev server across all tests in
	// this file; skipping for brevity here.
	// ...
	return "http://127.0.0.1:18200", "<role>", "<secret>"
}

func TestVaultBackend_ContractSuite(t *testing.T) {
	addr, roleID, secretID := startDevVault(t)
	b, err := NewVaultBackend(context.Background(), VaultConfig{
		Address: addr, RoleID: roleID, SecretID: secretID,
		MountPath: "secret", PathPrefix: "test-" + t.Name(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	// Run each contract test manually against this backend.
	ctx := context.Background()
	if err := b.Put(ctx, "K", "v"); err != nil {
		t.Fatal(err)
	}
	got, err := b.Get(ctx, "K")
	if err != nil || got != "v" {
		t.Errorf("get = %v, %v", got, err)
	}
	_ = time.Now()
}
```

(Full harness omitted here for brevity. The implementer should add an `os/exec`-based `vault server -dev -dev-listen-address=127.0.0.1:RANDOM -dev-root-token-id=root` spawner in a `TestMain`, enable AppRole, create a role, capture role_id + secret_id, and run all contract tests against it.)

Add to the `allBackends` helper in `secrets_test.go`:

```go
{"vault", func(t *testing.T) Backend {
    addr, roleID, secretID := startDevVault(t)
    b, err := NewVaultBackend(context.Background(), VaultConfig{
        Address: addr, RoleID: roleID, SecretID: secretID,
        MountPath: "secret", PathPrefix: "test-" + t.Name(),
    })
    if err != nil { t.Fatal(err) }
    return b
}},
```

- [ ] **Step 3: Test**

```bash
# Assumes vault is on PATH; otherwise tests skip cleanly.
go test ./internal/secrets/...
```

Expected: contract tests pass against both backends.

- [ ] **Step 4: Commit**

```bash
git add internal/secrets/vault_backend.go internal/secrets/vault_backend_test.go internal/secrets/secrets_test.go go.mod go.sum
git commit -m "feat(secrets): Vault KV v2 backend with AppRole auth"
```

### Task 6.4: Rotation runbooks

**Files:**
- Create: `docs/runbooks/rotate-ntfy-token.md`
- Create: `docs/runbooks/rotate-vault-root.md`
- Create: `docs/runbooks/rotate-jwt-signing-key.md`

**Context:**
Per Section 8.3: "Rotation is a documented procedure per secret in docs/runbooks/rotate-<secret>.md." This task ships three. The remaining secrets (MinIO/R2, VRRP password, NetClock secret, pgbouncer creds, Redis password, pgbackrest creds) get their own runbooks as those subsystems land; this plan establishes the template.

Template each runbook follows: When to rotate; Pre-flight checks; Rotation steps; Verification; Roll-back-if-it-broke; Audit log entry expected.

- [ ] **Step 1: Write `rotate-ntfy-token.md`**

```markdown
# Rotate an ntfy.sh publisher token

## When

- Suspected token leak (token in a git diff, screen-share, etc.)
- Quarterly cadence per security policy
- Operator handover

## Pre-flight

- Confirm phone is subscribed to the topic and current token still works:
  ```
  curl -H "Authorization: Bearer <current>" -d "rotation test" \
    https://ntfy.grimnir.example/grimnir-region-default-page
  ```
- Phone should receive the test.

## Rotate

1. SSH to ntfy VPS.
2. Generate new token:
   ```
   ntfy token add grimnir-page --label "Rotated $(date -I)"
   ```
3. Capture the new token.
4. Update the secret in every region's backend:
   ```
   grimnir-deploy rotate-secret --name NTFY_TOKEN_PAGE --value <new> --region default
   ```
   The deploy script `Rotate`s through `internal/secrets/`; verifier hits the ntfy server once with the new token before committing.
5. Once all regions report success, revoke the old token:
   ```
   ntfy token remove grimnir-page <old-token>
   ```

## Verify

- Test page from each region:
  ```
  grimnir-deploy verify --emit-test-page --region default
  ```
- Phone receives the test from each region.
- audit_log shows a `rotate-secret` row for NTFY_TOKEN_PAGE per region.

## Roll back

- If verifier fails during `rotate-secret`, the secret is automatically restored.
- If something breaks AFTER the old token is revoked, re-add it: `ntfy token add grimnir-page --label "Emergency restore"` and write the regenerated token back.

## Expected audit entries

- `subcommand=rotate-secret operator=<you> phase=started/completed args={"name":"NTFY_TOKEN_PAGE","new_value":"<redacted>"}`
- `subcommand=verify operator=<you> phase=completed notes="emit-test-page"`
```

- [ ] **Step 2: Write `rotate-vault-root.md` (similar shape)**

Covers: generating a new root token via `vault operator generate-root`, distributing key shares to operators, revoking the old root, smoke-testing AppRole login against the new root.

- [ ] **Step 3: Write `rotate-jwt-signing-key.md` (similar shape; documents the running-fleet invalidation cost)**

Covers: dual-key window (old + new accepted in parallel for 24h), rolling restart of grimnir control planes, audit-log inspection for unexpected unauthorized requests after cutover.

- [ ] **Step 4: Commit**

```bash
git add docs/runbooks/rotate-*.md
git commit -m "docs(runbooks): seed rotation runbooks for ntfy, vault, jwt"
```

---
## Chunk 7: Prometheus alerting rules + Alertmanager → ntfy

### Task 7.1: Alerting rules

**Files:**
- Create: `prometheus/grimnir.rules.yml`
- Create: `prometheus/grimnir.rules_test.yml`

**Context:**
One rule per metric tier-condition pair from Section 8.1. Each rule has `severity: notify|page|page-and-rollback` as a label; Alertmanager routes on that label.

- [ ] **Step 1: Write the rules file**

`prometheus/grimnir.rules.yml`:

```yaml
groups:
  - name: grimnir-ha-tier1-notify
    interval: 30s
    rules:
      - alert: PostgresReplicationLagWarn
        expr: grimnir_postgres_replication_lag_seconds > 5
        for: 2m
        labels:
          severity: notify
        annotations:
          summary: "Postgres replication lag > 5s"
          description: "Lag = {{ $value }}s for over 2 minutes."

      - alert: PcmInputStarved
        expr: rate(grimnir_pcm_input_packets_total[1m]) < 1
        for: 1m
        labels:
          severity: notify
        annotations:
          summary: "PCM input {{ $labels.engine }}/{{ $labels.source }} not receiving packets"
          description: "rate(packets) = {{ $value }}/s for 1 minute. Edge encoder will switch internally; this is observational."

      - alert: CacheHitRateLow
        expr: grimnir_cache_hit_rate_ratio < 0.8
        for: 30m
        labels:
          severity: notify
        annotations:
          summary: "Media cache hit rate < 80% for 30m"
          description: "Hit rate = {{ $value | humanizePercentage }}."

  - name: grimnir-ha-tier2-page
    interval: 30s
    rules:
      - alert: PostgresReplicationLagCritical
        expr: grimnir_postgres_replication_lag_seconds > 30
        for: 2m
        labels:
          severity: page
        annotations:
          summary: "Postgres replication lag > 30s"
          runbook: "https://github.com/friendsincode/grimnir_radio/blob/main/docs/runbooks/index.md#replication-lag"

      - alert: VrrpSplitBrain
        expr: grimnir_vrrp_holder_count > 1
        for: 30s
        labels:
          severity: page
        annotations:
          summary: "VRRP split brain on {{ $labels.vip }} ({{ $value }} holders)"
          runbook: "https://github.com/friendsincode/grimnir_radio/blob/main/docs/runbooks/index.md#vrrp-split-brain"

      - alert: VrrpNoHolder
        expr: grimnir_vrrp_holder_count == 0
        for: 30s
        labels:
          severity: page
        annotations:
          summary: "No VRRP holder for {{ $labels.vip }}"

      - alert: BothEnginesUnhealthy
        expr: sum(grimnir_engine_health) == 0
        for: 30s
        labels:
          severity: page
        annotations:
          summary: "Both engines unhealthy in region"

      - alert: RedisUnreachable
        expr: increase(grimnir_redis_unreachable_seconds[2m]) > 60
        labels:
          severity: page
        annotations:
          summary: "Redis unreachable for > 60s in last 2m"

      - alert: DeployFailed
        expr: increase(grimnir_deploy_history_failed_total[5m]) > 0
        labels:
          severity: page
        annotations:
          summary: "grimnir-deploy reported a failed deploy"

  - name: grimnir-ha-tier3-page-and-rollback
    interval: 15s
    rules:
      # The "soak window" is the 5 minutes after a deploy. The deploy emits
      # a synthetic series `grimnir_deploy_soak_active == 1` for that period.
      # This alert ONLY fires while soak is active.
      - alert: ListenerReconnectSpike
        expr: |
          (rate(grimnir_listener_reconnect_total[1m]) > 2 * avg_over_time(rate(grimnir_listener_reconnect_total[1m])[1h:1m]))
          and on() (grimnir_deploy_soak_active == 1)
        for: 30s
        labels:
          severity: page-and-rollback
        annotations:
          summary: "Listener reconnect rate spike during deploy soak"
          description: "Current rate = {{ $value }}/s. Auto-rollback triggered."

      - alert: EdgeEncoderByteFlowZero
        expr: |
          (sum by (node) (rate(grimnir_edge_encoder_bytes_total[1m])) == 0)
          and on() (grimnir_deploy_soak_active == 1)
        for: 1m
        labels:
          severity: page-and-rollback
        annotations:
          summary: "Edge encoder byte flow ZERO during soak"
```

- [ ] **Step 2: Write `promtool` rule tests**

`prometheus/grimnir.rules_test.yml`:

```yaml
rule_files:
  - grimnir.rules.yml

evaluation_interval: 30s

tests:
  - interval: 30s
    input_series:
      - series: 'grimnir_postgres_replication_lag_seconds'
        values: '0+1x3 6 6 6 6 6 6'  # cross 5s threshold then sit at 6
    alert_rule_test:
      - eval_time: 2m30s
        alertname: PostgresReplicationLagWarn
        exp_alerts:
          - exp_labels:
              severity: notify
            exp_annotations:
              summary: "Postgres replication lag > 5s"
              description: "Lag = 6s for over 2 minutes."

  - interval: 30s
    input_series:
      - series: 'grimnir_vrrp_holder_count{vip="listener"}'
        values: '1 1 1 2 2 2 2'
    alert_rule_test:
      - eval_time: 2m
        alertname: VrrpSplitBrain
        exp_alerts:
          - exp_labels:
              severity: page
              vip: listener
            exp_annotations:
              summary: "VRRP split brain on listener (2 holders)"
              runbook: "https://github.com/friendsincode/grimnir_radio/blob/main/docs/runbooks/index.md#vrrp-split-brain"
```

- [ ] **Step 3: Verify rules + tests**

```bash
docker run --rm -v $PWD/prometheus:/p prom/prometheus:v2.55.0 \
  promtool check rules /p/grimnir.rules.yml
docker run --rm -v $PWD/prometheus:/p prom/prometheus:v2.55.0 \
  promtool test rules /p/grimnir.rules_test.yml
```

Both should print `SUCCESS`.

- [ ] **Step 4: Add `prometheus-validate` Makefile target**

```makefile
.PHONY: prometheus-validate
prometheus-validate:
	docker run --rm -v $(PWD)/prometheus:/p prom/prometheus:v2.55.0 promtool check rules /p/grimnir.rules.yml
	docker run --rm -v $(PWD)/prometheus:/p prom/prometheus:v2.55.0 promtool test rules /p/grimnir.rules_test.yml

# Add to ci
ci: verify fmt-check prometheus-validate
```

- [ ] **Step 5: Commit**

```bash
git add prometheus/grimnir.rules.yml prometheus/grimnir.rules_test.yml Makefile
git commit -m "feat(alerts): three-tier alerting rules + promtool tests"
```

### Task 7.2: Alertmanager config

**Files:**
- Create: `prometheus/alertmanager.yml`

**Context:**
Routes on the `severity` label:
- `notify` → chat webhook (Slack/Discord — operator-configurable URL; this plan leaves it as `webhook_configs: url: REPLACE_ME`)
- `page` → ntfy.sh page topic via webhook
- `page-and-rollback` → ntfy.sh rollback topic AND grimnir-deploy webhook (Chunk 8)

Alertmanager doesn't speak ntfy natively. Use the generic `webhook_configs` with ntfy's webhook-compatible endpoint (ntfy accepts the Alertmanager payload via its `/v1/webhook` endpoint as of v2.7+; verify against the installed ntfy version).

> **If ntfy webhook compat is broken:** drop a tiny `cmd/grimnir-alertbridge/main.go` HTTP service that accepts Alertmanager webhooks and translates to ntfy POSTs. Defer that decision until live testing in Chunk 9.

- [ ] **Step 1: Write the config**

```yaml
global:
  resolve_timeout: 5m

route:
  receiver: notify-chat
  group_by: [alertname, severity]
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 4h
  routes:
    - matchers:
        - severity = page
      receiver: page-ntfy
      group_wait: 10s
      repeat_interval: 30m

    - matchers:
        - severity = page-and-rollback
      receiver: page-and-rollback
      group_wait: 0s
      repeat_interval: 15m
      continue: true

receivers:
  - name: notify-chat
    webhook_configs:
      - url: 'REPLACE_ME_CHAT_WEBHOOK'
        send_resolved: true

  - name: page-ntfy
    webhook_configs:
      - url: 'https://ntfy.grimnir.example/grimnir-region-default-page'
        send_resolved: true
        http_config:
          authorization:
            type: Bearer
            credentials: '${NTFY_TOKEN_PAGE}'

  - name: page-and-rollback
    webhook_configs:
      - url: 'https://ntfy.grimnir.example/grimnir-region-default-rollback'
        send_resolved: false
        http_config:
          authorization:
            type: Bearer
            credentials: '${NTFY_TOKEN_ROLLBACK}'
      - url: 'http://grimnir-deploy.internal:9100/webhook/auto-rollback'
        send_resolved: false
        http_config:
          authorization:
            type: Bearer
            credentials: '${GRIMNIR_DEPLOY_WEBHOOK_TOKEN}'
```

- [ ] **Step 2: Validate**

```bash
docker run --rm -v $PWD/prometheus:/p prom/alertmanager:v0.27.0 \
  amtool check-config /p/alertmanager.yml
```

Expected: `Checking '/p/alertmanager.yml'  SUCCESS`.

- [ ] **Step 3: Commit**

```bash
git add prometheus/alertmanager.yml
git commit -m "feat(alerts): Alertmanager routing for three tiers"
```

### Task 7.3: Prometheus scrape config

**Files:**
- Create: `prometheus/prometheus.yml`

- [ ] **Step 1: Write the scrape config**

```yaml
global:
  scrape_interval: 15s
  evaluation_interval: 30s
  external_labels:
    region: default

rule_files:
  - grimnir.rules.yml

alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - alertmanager:9093

scrape_configs:
  - job_name: grimnirradio
    static_configs:
      - targets:
          - 'node-1:8080'
          - 'node-2:8080'
    metrics_path: /metrics

  - job_name: mediaengine
    static_configs:
      - targets:
          - 'node-1:9090'
          - 'node-2:9090'

  - job_name: edge-encoder
    static_configs:
      - targets:
          - 'node-1:9095'
          - 'node-2:9095'

  - job_name: grimnir-fanout
    static_configs:
      - targets:
          - 'node-1:9097'
          - 'node-2:9097'

  - job_name: grimnir-deploy
    static_configs:
      - targets:
          - 'grimnir-deploy.internal:9100'

  - job_name: node-exporter
    static_configs:
      - targets:
          - 'node-1:9100'
          - 'node-2:9100'
```

- [ ] **Step 2: Validate**

```bash
docker run --rm -v $PWD/prometheus:/p prom/prometheus:v2.55.0 \
  promtool check config /p/prometheus.yml
```

Expected: `SUCCESS`.

- [ ] **Step 3: Commit**

```bash
git add prometheus/prometheus.yml
git commit -m "feat(prometheus): scrape config for all HA targets"
```

---
## Chunk 8: Auto-rollback hook from soak window

This chunk wires the soak-window alert (Chunk 7) to `grimnir-deploy --rollback` (Track B-2). Requires B-2 has the `--rollback` flag landed first; this plan blocks until that's true.

### Task 8.1: Webhook receiver in grimnir-deploy

**Files:**
- Create: `cmd/grimnir-deploy/rollback_webhook.go`
- Test: `cmd/grimnir-deploy/rollback_webhook_test.go`

**Context:**
Alertmanager POSTs a JSON payload to the webhook URL when the alert fires. Payload shape (Alertmanager v2):

```json
{
  "version": "4",
  "groupKey": "{}/{severity=\"page-and-rollback\"}",
  "status": "firing",
  "receiver": "page-and-rollback",
  "groupLabels": {"severity": "page-and-rollback"},
  "alerts": [
    {
      "status": "firing",
      "labels": {"alertname": "ListenerReconnectSpike", ...},
      "annotations": {"summary": "..."},
      "fingerprint": "abc123"
    }
  ]
}
```

The webhook:
1. Validates the Bearer token.
2. Deduplicates by `fingerprint` (Redis SET with 1h TTL) so a re-fire within the same incident doesn't trigger a second rollback.
3. Writes an audit-log row (`subcommand=auto-rollback operator=alertmanager`).
4. Calls the rollback library function from Track B-2.
5. Posts a `PageAndRollback` ntfy on success or failure (this is informational; the audit topic already echoed the rollback).

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWebhook_RejectsMissingAuth(t *testing.T) {
	h := newTestWebhook(t)
	req := httptest.NewRequest(http.MethodPost, "/webhook/auto-rollback", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebhook_RejectsWrongAuth(t *testing.T) {
	h := newTestWebhook(t)
	req := httptest.NewRequest(http.MethodPost, "/webhook/auto-rollback", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWebhook_AcceptsValidAlertAndCallsRollback(t *testing.T) {
	called := false
	rollback := func(ctx context.Context, reason string) error {
		called = true
		require.Contains(t, reason, "ListenerReconnectSpike")
		return nil
	}
	h := newTestWebhookWithRollback(t, rollback)

	payload := map[string]interface{}{
		"version": "4",
		"status":  "firing",
		"alerts": []map[string]interface{}{
			{
				"status":      "firing",
				"labels":      map[string]string{"alertname": "ListenerReconnectSpike", "severity": "page-and-rollback"},
				"fingerprint": "fp1",
			},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/webhook/auto-rollback", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestWebhook_DedupesByFingerprint(t *testing.T) {
	count := 0
	rollback := func(ctx context.Context, _ string) error {
		count++
		return nil
	}
	h := newTestWebhookWithRollback(t, rollback)

	payload, _ := json.Marshal(map[string]interface{}{
		"alerts": []map[string]interface{}{
			{"status": "firing", "fingerprint": "fp-dedupe-test", "labels": map[string]string{"alertname": "x"}},
		},
	})
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/webhook/auto-rollback", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
	}
	require.Equal(t, 1, count, "rollback called once despite 3 webhook deliveries")
}

func TestWebhook_IgnoresResolvedAlerts(t *testing.T) {
	called := false
	rollback := func(ctx context.Context, _ string) error {
		called = true
		return nil
	}
	h := newTestWebhookWithRollback(t, rollback)
	payload, _ := json.Marshal(map[string]interface{}{
		"alerts": []map[string]interface{}{
			{"status": "resolved", "fingerprint": "fpres", "labels": map[string]string{"alertname": "x"}},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/webhook/auto-rollback", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.False(t, called, "resolved alert must not trigger rollback")
}
```

- [ ] **Step 2: Implement**

`cmd/grimnir-deploy/rollback_webhook.go`:

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"github.com/friendsincode/grimnir_radio/internal/audit"
	"github.com/friendsincode/grimnir_radio/internal/notify"
)

type alertmanagerPayload struct {
	Version string `json:"version"`
	Status  string `json:"status"`
	Alerts  []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
		Fingerprint string            `json:"fingerprint"`
	} `json:"alerts"`
}

type RollbackWebhook struct {
	expectedToken string
	rollback      func(ctx context.Context, reason string) error
	audit         *audit.Writer
	notifier      *notify.Client
	rdb           *redis.Client
	dedupeTTL     time.Duration

	// In-memory fallback when Redis is unavailable (still want dedupe within
	// process lifetime).
	mu      sync.Mutex
	seenLRU map[string]time.Time
}

func NewRollbackWebhook(expectedToken string, rollback func(context.Context, string) error,
	audit *audit.Writer, notifier *notify.Client, rdb *redis.Client) *RollbackWebhook {
	return &RollbackWebhook{
		expectedToken: expectedToken,
		rollback:      rollback,
		audit:         audit,
		notifier:      notifier,
		rdb:           rdb,
		dedupeTTL:     1 * time.Hour,
		seenLRU:       make(map[string]time.Time),
	}
}

func (h *RollbackWebhook) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authOK(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var p alertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad payload", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	for _, a := range p.Alerts {
		if a.Status != "firing" {
			continue
		}
		if h.seen(ctx, a.Fingerprint) {
			log.Info().Str("fingerprint", a.Fingerprint).Msg("rollback webhook: dedup hit, skipping")
			continue
		}

		alertname := a.Labels["alertname"]
		reason := fmt.Sprintf("%s: %s", alertname, a.Annotations["summary"])

		action := h.audit.Start(ctx, "auto-rollback", map[string]interface{}{
			"alertname":   alertname,
			"fingerprint": a.Fingerprint,
			"reason":      reason,
		})

		if err := h.rollback(ctx, reason); err != nil {
			action.Fail(ctx, err)
			_ = h.notifier.PageAndRollback(ctx, notify.Message{
				Title: "[grimnir] Auto-rollback FAILED",
				Body:  fmt.Sprintf("alert=%s reason=%s error=%s — operator MUST intervene", alertname, reason, err),
			})
			http.Error(w, "rollback failed", http.StatusInternalServerError)
			return
		}

		action.Complete(ctx, "ok", reason)
		_ = h.notifier.PageAndRollback(ctx, notify.Message{
			Title: "[grimnir] Auto-rollback executed",
			Body:  fmt.Sprintf("alert=%s reason=%s — system rolled back; investigate.", alertname, reason),
		})
	}

	w.WriteHeader(http.StatusOK)
}

func (h *RollbackWebhook) authOK(r *http.Request) bool {
	a := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(a, prefix) {
		return false
	}
	got := a[len(prefix):]
	// Constant-time compare via subtle would be ideal; for brevity:
	return got == h.expectedToken && got != ""
}

func (h *RollbackWebhook) seen(ctx context.Context, fp string) bool {
	if fp == "" {
		return false // can't dedupe without a fingerprint
	}
	// Try Redis first.
	if h.rdb != nil {
		ok, err := h.rdb.SetNX(ctx, "grimnir:auto-rollback:seen:"+fp, "1", h.dedupeTTL).Result()
		if err == nil {
			return !ok // SetNX returns true when SET, false when already existed
		}
	}
	// Fallback: in-memory LRU.
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	if ts, ok := h.seenLRU[fp]; ok && now.Sub(ts) < h.dedupeTTL {
		return true
	}
	h.seenLRU[fp] = now
	// GC entries older than the TTL.
	for k, ts := range h.seenLRU {
		if now.Sub(ts) > h.dedupeTTL {
			delete(h.seenLRU, k)
		}
	}
	return false
}
```

Test helpers:

```go
func newTestWebhook(t *testing.T) http.Handler {
	return newTestWebhookWithRollback(t, func(context.Context, string) error { return nil })
}

func newTestWebhookWithRollback(t *testing.T, rb func(context.Context, string) error) http.Handler {
	// nil audit + notifier OK for these tests if implementation handles nil;
	// otherwise inject mocks. Sketch — implementer fills in.
	return NewRollbackWebhook("test-token", rb, nil, nil, nil)
}
```

(Implementer note: the `RollbackWebhook` constructor accepts nil audit/notifier today for testability; production callers wire real ones. Add nil-guards in `ServeHTTP` accordingly, or pass real fakes from the test.)

- [ ] **Step 3: Wire into grimnir-deploy main**

In `cmd/grimnir-deploy/main.go` (created by Track B-2), add a subcommand or a daemon flag that starts the webhook server:

```go
if cfg.WebhookEnabled {
    h := NewRollbackWebhook(
        cfg.WebhookToken,
        rollback.AutoRollback,  // Track B-2 exposes this function
        auditWriter,
        notifyClient,
        rdb,
    )
    mux := http.NewServeMux()
    mux.Handle("/webhook/auto-rollback", h)
    mux.Handle("/metrics", metrics.Handler(metrics.DeployRegistry))
    go func() {
        log.Info().Str("addr", cfg.WebhookAddr).Msg("rollback webhook listening")
        if err := http.ListenAndServe(cfg.WebhookAddr, mux); err != nil {
            log.Fatal().Err(err).Msg("webhook server")
        }
    }()
}
```

- [ ] **Step 4: Test + commit**

```bash
go test ./cmd/grimnir-deploy/...
git add cmd/grimnir-deploy/rollback_webhook.go cmd/grimnir-deploy/rollback_webhook_test.go cmd/grimnir-deploy/main.go
git commit -m "feat(deploy): auto-rollback webhook with auth + dedupe + audit"
```

### Task 8.2: Soak-window synthetic metric emitter

**Files:**
- Modify: `cmd/grimnir-deploy/main.go` (deploy command flow)

**Context:**
The tier-3 alert in Chunk 7 fires only when `grimnir_deploy_soak_active == 1`. That series doesn't exist until something emits it. The deploy binary itself emits it: at the end of a deploy, set `SoakActive = 1`, hold for 5 minutes, then set `SoakActive = 0`.

- [ ] **Step 1: Add the metric**

In `internal/metrics/ha.go`:

```go
DeploySoakActive = prometheus.NewGauge(prometheus.GaugeOpts{
    Name: "grimnir_deploy_soak_active",
    Help: "1 during a deploy's 5-minute soak window, 0 otherwise.",
})
```

Register in `DeployRegistry`:

```go
DeployRegistry.MustRegister(DeployHistoryFailedTotal, DeploySoakActive)
```

- [ ] **Step 2: In the deploy completion flow**

```go
func runSoakWindow(ctx context.Context, dur time.Duration) {
    metrics.DeploySoakActive.Set(1)
    defer metrics.DeploySoakActive.Set(0)
    select {
    case <-ctx.Done():
    case <-time.After(dur):
    }
}
```

Call from main after a deploy:

```go
runSoakWindow(ctx, 5*time.Minute)
```

- [ ] **Step 3: Add a test**

```go
func TestSoakWindow_SetsThenClears(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()
    done := make(chan struct{})
    go func() {
        runSoakWindow(ctx, 50*time.Millisecond)
        close(done)
    }()
    time.Sleep(10 * time.Millisecond)
    if testutil.ToFloat64(metrics.DeploySoakActive) != 1 {
        t.Error("soak should be active mid-window")
    }
    <-done
    if testutil.ToFloat64(metrics.DeploySoakActive) != 0 {
        t.Error("soak should be inactive after window")
    }
}
```

- [ ] **Step 4: Commit**

```bash
git add internal/metrics/ha.go cmd/grimnir-deploy/main.go cmd/grimnir-deploy/soak_test.go
git commit -m "feat(deploy): emit grimnir_deploy_soak_active during 5-minute soak"
```

### Task 8.3: End-to-end manual test against staging

**Context:**
The wiring is across Prometheus + Alertmanager + ntfy + grimnir-deploy. Unit tests cover each piece but not the seam. One manual e2e against a staging region before this hits production.

- [ ] **Step 1: Stand up the staging region**

Per Track A acceptance gates for whichever step this lands behind.

- [ ] **Step 2: Trigger a synthetic soak-window listener-reconnect spike**

```bash
# On staging, while a deploy is in its soak window:
grimnir-deploy deploy --image v2.0.0-alpha.5-test --region staging
# Wait until SoakActive=1 (~5s after deploy completes), then:
for i in $(seq 1 100); do
  curl -s -X POST http://staging.grimnir.example/_test/inject-reconnect
done
```

(The `_test/inject-reconnect` endpoint is gated by `GRIMNIR_TEST_MODE=1`; ships in this chunk if it doesn't already.)

- [ ] **Step 3: Verify**

- Prometheus shows `ListenerReconnectSpike` firing.
- Phone receives a ntfy on the rollback topic within 30s.
- audit_log on staging has rows for `auto-rollback started/completed`.
- The previous image is back in service (verify image tag via `docker ps`).

- [ ] **Step 4: Document**

`docs/runbooks/auto-rollback.md`:

Covers: what auto-rollback does, what to expect on the phone, how to disable (`grimnir-deploy deploy --no-auto-rollback`), how to re-enable, where to find the audit trail.

- [ ] **Step 5: Bump version + tag + push**

```go
// internal/version/version.go
var Version = "2.0.0-alpha.6"
```

```bash
git add internal/version/version.go docs/runbooks/auto-rollback.md
git commit -m "Bump to v2.0.0-alpha.6: auto-rollback hook wired"
git tag -a v2.0.0-alpha.6 -m "v2-dev: auto-rollback from soak window"
git push origin v2-dev && git push origin v2.0.0-alpha.6
```

---
## Chunk 9: Grafana dashboards as code

Three dashboards: per-region, cross-region overview, deploy-soak. Each is a JSON file committed in-tree. Imported into Grafana via the provisioning system.

### Task 9.1: Per-region dashboard

**Files:**
- Create: `dashboards/grimnir-region.json`

**Context:**
Hand-authoring Grafana JSON is painful. The honest workflow is: build the dashboard in Grafana's UI, export the JSON, commit it. Re-importing edits requires `Settings → JSON Model → Save`. Treat the JSON as semi-readable; reviewers diff the panel titles + queries, not the full structure.

Panels (one row each):

| Row | Panels |
|---|---|
| Audio path | EngineHealth per node (stat); PcmInputPackets rate per engine-source (timeseries); EdgeEncoderBytes per node (timeseries) |
| Listeners | ListenerReconnect rate 5m (timeseries); reconnect total (counter) |
| Database | PostgresReplicationLag (timeseries with 5s + 30s threshold lines) |
| VRRP | VrrpHolderCount per VIP (stat, color: 1=green, 0/2=red) |
| Infrastructure | RedisUnreachableSeconds (counter); CacheHitRateRatio (gauge) |
| Deploy | DeploySoakActive (state timeline); DeployHistoryFailedTotal (counter) |

- [ ] **Step 1: Stand up a local Grafana**

```bash
docker run --rm -p 3000:3000 \
  -e GF_AUTH_ANONYMOUS_ENABLED=true \
  -e GF_AUTH_ANONYMOUS_ORG_ROLE=Admin \
  grafana/grafana-oss:11.2.0
```

- [ ] **Step 2: Add Prometheus as a data source**

In Grafana UI: Connections → Add → Prometheus → URL `http://host.docker.internal:9090` (or wherever the local Prometheus is).

- [ ] **Step 3: Build the dashboard panel-by-panel**

Use the metric names from `internal/metrics/ha.go`. For each panel, use the query suggested in Section 8.1 of the design (rate-of-counter for counters, instant value for gauges).

- [ ] **Step 4: Export JSON and commit**

Dashboard settings → JSON Model → copy → paste into `dashboards/grimnir-region.json`.

- [ ] **Step 5: Validate**

```bash
jq . dashboards/grimnir-region.json > /dev/null
```

(Just verifies it's valid JSON.)

- [ ] **Step 6: Commit**

```bash
git add dashboards/grimnir-region.json
git commit -m "feat(dashboards): per-region grafana dashboard"
```

### Task 9.2: Cross-region overview

**Files:**
- Create: `dashboards/grimnir-cross-region.json`

**Context:**
Phase 1 has one region so this is mostly a placeholder, but ship it now so phase 2 doesn't get blocked on dashboard authoring.

Panels: side-by-side per-region EngineHealth, EdgeEncoderBytes, PostgresReplicationLag, VrrpHolderCount. Filter by `{region=~".*"}` so it auto-includes new regions.

- [ ] **Step 1-5: Same workflow as 9.1**

- [ ] **Step 6: Commit**

```bash
git add dashboards/grimnir-cross-region.json
git commit -m "feat(dashboards): cross-region overview"
```

### Task 9.3: Deploy soak dashboard

**Files:**
- Create: `dashboards/grimnir-deploy.json`

**Context:**
Single-purpose dashboard for watching a deploy's 5-minute soak window live. Pull up during every deploy.

Panels: DeploySoakActive (state timeline); ListenerReconnect rate 1m + baseline 1h-window (timeseries — visualizes the 2× threshold the alert uses); EdgeEncoderBytes per node 1m (timeseries); DeployHistoryFailedTotal (counter).

- [ ] **Step 1-5: Workflow as above**

- [ ] **Step 6: Commit**

```bash
git add dashboards/grimnir-deploy.json
git commit -m "feat(dashboards): deploy-soak watcher"
```

### Task 9.4: Dashboard provisioning + README

**Files:**
- Create: `dashboards/README.md`
- Create: `dashboards/provisioning/grimnir.yml` (Grafana provisioning config)

- [ ] **Step 1: Provisioning config**

```yaml
# dashboards/provisioning/grimnir.yml
apiVersion: 1

providers:
  - name: 'grimnir'
    orgId: 1
    folder: 'Grimnir'
    type: file
    disableDeletion: true
    updateIntervalSeconds: 30
    allowUiUpdates: true
    options:
      path: /etc/grafana/provisioning/dashboards/grimnir
```

(Grafana picks up JSON files placed in that path; the production deploy mounts `dashboards/` into `/etc/grafana/provisioning/dashboards/grimnir/`.)

- [ ] **Step 2: README**

```markdown
# Grimnir Grafana dashboards

Three dashboards live here:

- `grimnir-region.json` — per-region operational view
- `grimnir-cross-region.json` — fleet overview (phase 2+)
- `grimnir-deploy.json` — pull up during every deploy

## Workflow

1. Edit in Grafana UI.
2. Settings → JSON Model → copy.
3. Replace the file's contents.
4. PR with a screenshot of the changed panels.

## Provisioning

Production Grafana mounts this directory at
`/etc/grafana/provisioning/dashboards/grimnir/` via the docker-compose
override. Changes deploy on the next Grafana restart (or 30s tick via
`updateIntervalSeconds`).

## Adding a dashboard

1. Build in Grafana UI.
2. Export JSON.
3. Add the file here.
4. Add a row to this README.
```

- [ ] **Step 3: Commit**

```bash
git add dashboards/README.md dashboards/provisioning/grimnir.yml
git commit -m "docs(dashboards): provisioning config + workflow"
```

---

## Chunk 10: Final docs, runbook index, CLAUDE.md, version bump

### Task 10.1: Runbook index

**Files:**
- Create: `docs/runbooks/index.md`

**Context:**
Per Section 8.2: "Operator opens it at 3am, finds the symptom, runs the named subcommand." Table format: symptom column wide, subcommand narrow, short description. This plan seeds entries for the observability-side symptoms; Track B-6 fills in operational ones (drain, promote-replica, cold-start-region, etc.).

- [ ] **Step 1: Write the index**

```markdown
# Grimnir Runbook Index

Symptom → subcommand → procedure. Optimized for 3am scanning.

| Symptom | Subcommand or doc | Notes |
|---|---|---|
| ntfy page: PostgresReplicationLagCritical | [docs/runbooks/promote-replica.md](promote-replica.md) | Tier-2; replica is behind; investigate before promoting |
| ntfy page: VrrpSplitBrain | [docs/runbooks/vrrp-split-brain.md](vrrp-split-brain.md) | Two masters claim the same VIP; recover-partition subcommand exists for the safe-to-automate case |
| ntfy page: BothEnginesUnhealthy | `grimnir-deploy verify --region <r>` | Triage; engine restart procedure in [restart-engines.md](restart-engines.md) |
| ntfy page: RedisUnreachable | [docs/runbooks/redis-recovery.md](redis-recovery.md) | Leader election + emergency-pause depend on Redis; degrade gracefully |
| ntfy page: DeployFailed | `grimnir-deploy verify --region <r>` then read audit_log for the failure row | Audit row's `notes` field carries the failure reason |
| ntfy page-and-rollback: ListenerReconnectSpike | (auto-rollback already executed) [auto-rollback.md](auto-rollback.md) | Investigate WHY; don't re-deploy without root cause |
| ntfy page-and-rollback: EdgeEncoderByteFlowZero | (auto-rollback already executed) [auto-rollback.md](auto-rollback.md) | Same as above |
| ntfy notify (chat): PostgresReplicationLagWarn | Wait + watch; if it climbs, escalates to tier-2 | Daytime triage |
| ntfy notify (chat): PcmInputStarved | Edge encoder switches internally; check the OTHER input is healthy | Observational |
| ntfy notify (chat): CacheHitRateLow | Capacity-planning signal; consider cache size increase | Not paging |
| Audit notification you didn't trigger | Treat as security event | [security-incident.md](security-incident.md) |
| Suspected secret leak | Rotate the suspect secret immediately | [rotate-*.md](.) for each secret type |
| Operator handover | [operator-handover.md](operator-handover.md) | New on-call: read this and the runbook index |
| Quarterly backup drill | `grimnir-deploy backup-drill --region <r>` | RTO/RPO measurement |

## Subcommand cheat sheet

| Subcommand | When to use |
|---|---|
| `grimnir-deploy verify` | Read-only health probe; safe at any time |
| `grimnir-deploy deploy --image TAG` | Normal deploy; runs soak window + auto-rollback |
| `grimnir-deploy --rollback` | Manual rollback to previous version |
| `grimnir-deploy emergency-pause` | Stop all auto-deploys (e.g., during incident) |
| `grimnir-deploy emergency-resume` | Re-enable auto-deploys |
| `grimnir-deploy drain --node N` | Drain a node for maintenance |
| `grimnir-deploy promote-replica` | Promote Postgres replica to primary |
| `grimnir-deploy cold-start-region` | Bring up a fresh region from scratch |
| `grimnir-deploy restore --from BACKUP_ID` | Restore Postgres from pgbackrest |
| `grimnir-deploy backup-drill` | Quarterly RTO/RPO drill |
| `grimnir-deploy rotate-secret --name X` | Rotate a secret via the configured backend |

Most subcommands take `--dry-run`, `--region`, `--help`. All write audit rows.
```

- [ ] **Step 2: Stub the referenced runbooks that don't exist yet**

For each unwritten runbook (`promote-replica.md`, `vrrp-split-brain.md`, `redis-recovery.md`, `restart-engines.md`, `security-incident.md`, `operator-handover.md`): create a single-line stub:

```markdown
# <Name>

> Stub. Filled in by Track B-6 (runbook subcommands) implementation plan.
```

This avoids dead links from the index.

- [ ] **Step 3: Commit**

```bash
git add docs/runbooks/index.md docs/runbooks/*.md
git commit -m "docs(runbooks): symptom → subcommand index per Section 8.2"
```

### Task 10.2: Update CLAUDE.md

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add new directory entries to the architecture list**

After the existing `internal/notifications/` mention (or near it), add:

```markdown
- `internal/metrics/` - HA-specific Prometheus metrics with per-binary registries (see `docs/superpowers/plans/2026-06-06-observability-secrets-audit.md` for the design rationale)
- `internal/notify/` - Self-hosted ntfy.sh client with three-tier severity (Notify / Page / PageAndRollback)
- `internal/secrets/` - Pluggable secrets backend (`.env` baseline + Vault). Backend selected via `GRIMNIR_SECRETS_BACKEND=env|vault`
- `internal/audit/` - Existing event-bus audit (service.go) + new operator-action writer (writer.go) for grimnir-deploy subcommands
- `prometheus/` - Alerting rules + scrape + Alertmanager config; validate with `make prometheus-validate`
- `dashboards/` - Grafana dashboards as code; see `dashboards/README.md` for the edit workflow
- `ops/ntfy/`, `ops/keepalived/`, `ops/prometheus/` - Operational provisioning scripts for the HA stack hosts
```

- [ ] **Step 2: Add new environment variables to the env-vars section**

```markdown
- `GRIMNIR_SECRETS_BACKEND` - `env` (default) or `vault`. Selects the secrets backend.
- `GRIMNIR_SECRETS_ENV_FILE` - Path to the .env file (default `.env`). Only used when backend=env.
- `VAULT_ADDR`, `VAULT_ROLE_ID`, `VAULT_SECRET_ID` - Vault AppRole credentials. Required when backend=vault.
- `GRIMNIR_NTFY_URL` - Self-hosted ntfy.sh base URL (e.g., `https://ntfy.grimnir.example`)
- `GRIMNIR_NTFY_TOKEN_PAGE`, `GRIMNIR_NTFY_TOKEN_AUDIT`, `GRIMNIR_NTFY_TOKEN_ROLLBACK` - Per-topic publisher tokens
- `GRIMNIR_REGION` - Region short name; defaults to `default`. Drives ntfy topic naming.
```

- [ ] **Step 3: Add a note about `make prometheus-validate` to the Common Commands section**

```markdown
# Validate Prometheus alerting rules + scrape config
make prometheus-validate
```

- [ ] **Step 4: Commit**

```bash
git add CLAUDE.md
git commit -m "docs(CLAUDE.md): document HA Track B-4 packages + env vars"
```

### Task 10.3: Final version bump + push

**Files:**
- Modify: `internal/version/version.go`

- [ ] **Step 1: Bump to v2.0.0-alpha.7**

```go
var Version = "2.0.0-alpha.7"
```

- [ ] **Step 2: Commit + tag + push per CLAUDE.md**

```bash
make ci   # MUST pass — no exceptions per the user's explicit demand
git add internal/version/version.go && \
  git commit -m "Bump to v2.0.0-alpha.7: Track B-4 (observability + secrets + audit) complete" && \
  git tag -a v2.0.0-alpha.7 -m "v2-dev: Track B-4 complete" && \
  git push origin v2-dev && git push origin v2.0.0-alpha.7
```

---

## Done

When all 11 chunks are merged to v2-dev:

- Every binary exposes `/metrics` with HA metrics registered
- Self-hosted ntfy.sh is running on a separate VPS with three topics per region
- `internal/notify/` ships a typed client with three severity methods
- `audit_log` table exists; operator actions write rows + post ntfy notifications
- `internal/secrets/` ships with `.env` (default) and Vault backends
- Three rotation runbooks exist; the rest follow the same template
- Prometheus alerts are defined for every Section 8.1 metric with three severity tiers
- Alertmanager routes notify-tier to chat, page-tier to ntfy, page-and-rollback to ntfy + grimnir-deploy webhook
- Auto-rollback fires automatically on listener-reconnect spikes during the post-deploy soak window
- Three Grafana dashboards are committed; provisioning config is in place
- Runbook index links every observability symptom to a subcommand or doc

The Track A and Track B-2 plans can use these primitives from here on. Subsequent Track B-6 plans (drain, promote-replica, etc.) write audit rows via `internal/audit/writer.go` and surface in the runbook index without coordinating back with this plan.




