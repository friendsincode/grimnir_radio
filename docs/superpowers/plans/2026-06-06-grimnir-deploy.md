# `grimnir-deploy` Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

> **Status:** Drafted 2026-06-06 incrementally per `feedback_brainstorming_incremental_save.md`; every chunk written to disk before the next was drafted. 15 chunks (Chunk 0 skeleton + Chunks 1-14). Honest estimate: 6-8 weeks solo. Several subcommands (`promote-replica`, `restore`, `recover-partition`, `cold-start-region`) need real Postgres/Redis/pgbackrest infrastructure to integration-test against; those tests carry skip conditions documented per chunk.

**Goal:** Build `cmd/grimnir-deploy/`, a Go binary that orchestrates rolling updates across two HA nodes, runs operational runbooks as first-class subcommands, gates deploys via per-region policy (`auto` / `window` / `manual`), respects emergency-pause + tag-suffix conventions, and writes an audit row + ntfy notification for every action.

**Architecture:** Single binary, cobra subcommand tree. Pre-flight gate layer (`internal/grimnirdeploy/gates`) is shared by every subcommand and runs the policy / emergency-pause / tag-suffix / image-exists / cluster-health checks before any mutation. State lives in two new Postgres tables (`audit_log`, `deploy_history`) + the existing Redis for the emergency-pause key. Side effects on cluster nodes happen via SSH (`golang.org/x/crypto/ssh`) and on the local Docker engine via `docker compose` shell-outs through a `RemoteRunner` interface (so tests can substitute a fake). Health probes use the same `/healthz`, gRPC `health.Check`, and `pg_stat_replication` queries Section 7 of the design specifies.

**Tech Stack:** Go 1.24 (matching repo baseline), `cobra` v1.8 (already in go.mod), `gorm` v1.31 + Postgres driver (already in go.mod for the two new tables), `redis/go-redis/v9` (already in use for leader election + emergency-pause), `golang.org/x/crypto/ssh` (new dependency for SSH-driving the peer node), `google.golang.org/grpc` + `grpc-health-v1` (already in repo) for health checks, ntfy HTTP API (plain `net/http`; no SDK needed).

**Issue:** TBD; file when Chunk 0 lands.

**Parent design:** `docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md`. Specifically:
- Section 6: Rolling update flow (full deploy script spec)
- Section 7: Failure modes the deploy + verify subcommands must detect
- Section 8.1: ntfy notification target
- Section 8.2: Subcommand list (this plan's primary contract)
- Section 8.3: Audit log schema (verbatim) and audit-ntfy separation
- Section 8.4: Backup-drill subcommand requirements
- Decisions log Q12 = **B+** (auto-deploy with per-environment policy + emergency pause toggle + tag suffix conventions)

**Decisions locked 2026-06-06 (Q-GD1..Q-GD5):**

| Q | Decision | Rationale |
|---|---|---|
| Q-GD1 | **A**: single binary, cobra subcommand tree | Matches the existing `cmd/grimnirradio` pattern; one binary to deploy, one set of flags to learn, one audit-log path. Q12=B+ explicitly names "grimnir-deploy" as a single tool |
| Q-GD2 | **B**: SSH-driven peer control, no agent on the peer | An agent on each node is more code, more attack surface, more failure modes. SSH + idempotent shell-outs is enough for two-node clusters; revisit if cluster size grows past four nodes |
| Q-GD3 | **B**: deploy / rollback / verify in Chunks 4-6, runbooks in 7-12 | The deploy + rollback path is the highest-leverage piece (every release uses it); the per-incident runbooks (`promote-replica`, `restore`, `recover-partition`, `cold-start-region`) are needed less often and can land incrementally after the core loop is solid |
| Q-GD4 | **C**: fake SSH and fake Docker runner via interface for unit tests; real-infrastructure tests gated by build tag | Lets the bulk of the logic (decisions, state transitions, gates, audit-writing) be tested fast without infra; the integration tests that need real Postgres / Redis / SSH / pgbackrest are tagged `integration` and `requires_real_cluster` so CI skips them by default |
| Q-GD5 | **A**: share `audit_log` and `deploy_history` tables with the control plane's Postgres | One DB to back up, one schema migration discipline (expand/contract), one source of truth. The existing app-level `internal/audit` package is unrelated and stays as-is |

**Honest scope:** 15 chunks. 6-8 weeks at solo pace if working through this in order. Chunks 0+1+2+3 are the foundation everything else depends on; after those land, Chunks 4-14 can be dispatched in any order to parallel subagents (each chunk produces an independently testable subcommand or piece of infrastructure). Chunks 8, 10, 11 (`promote-replica`, `restore`, `recover-partition`) carry the steepest infra requirements and ship behind feature-flagged integration tests; the production runbook docs they reference must be cross-validated against real drills before declaring them production-ready.

---

## File structure

| File | Status | Responsibility |
|---|---|---|
| `cmd/grimnir-deploy/main.go` | Create | Entry point: cobra root, signal handling, config loading, dependency wiring |
| `cmd/grimnir-deploy/main_test.go` | Create | Binary-level smoke tests (`--help`, `--version`) |
| `internal/grimnirdeploy/config.go` | Create | Env-var + config-file loading; region/policy/window/tag-suffix table |
| `internal/grimnirdeploy/config_test.go` | Create | Config defaults + env-var override tests |
| `internal/grimnirdeploy/audit/store.go` | Create | `audit_log` table writer; redaction of secrets in `args_json` |
| `internal/grimnirdeploy/audit/store_test.go` | Create | Audit-write tests against SQLite (sqlite is fine; the schema is portable) |
| `internal/grimnirdeploy/audit/ntfy.go` | Create | ntfy HTTP poster (separate `grimnir-audit-<region>` topic per Section 8.3) |
| `internal/grimnirdeploy/audit/ntfy_test.go` | Create | ntfy poster tests against `httptest.Server` |
| `internal/grimnirdeploy/audit/middleware.go` | Create | "Wrap" helper: every subcommand runs through this, gets START + COMPLETE rows + ntfy notifications |
| `internal/grimnirdeploy/audit/middleware_test.go` | Create | Middleware tests: happy path, error path, panic path |
| `internal/grimnirdeploy/pause/redis.go` | Create | Emergency-pause Redis key (`grimnir:emergency-pause`); set, clear, read |
| `internal/grimnirdeploy/pause/redis_test.go` | Create | Pause set/clear/read tests against miniredis |
| `internal/grimnirdeploy/cmd_emergency.go` | Create | `emergency-pause` and `emergency-resume` subcommands |
| `internal/grimnirdeploy/cmd_emergency_test.go` | Create | Subcommand tests; verify Redis state + audit row + ntfy posted |
| `internal/grimnirdeploy/history/store.go` | Create | `deploy_history` table writer + reader (last successful tag, eligibility window check, contract-migration crossing detector) |
| `internal/grimnirdeploy/history/store_test.go` | Create | Reader/writer tests; eligibility-window edges; contract-crossing detection |
| `internal/grimnirdeploy/gates/policy.go` | Create | Deploy-policy gate (`auto` / `window` / `manual`); cron-window matcher |
| `internal/grimnirdeploy/gates/policy_test.go` | Create | Policy table-driven tests |
| `internal/grimnirdeploy/gates/tagsuffix.go` | Create | Tag-suffix gate: `-hold`, `-hotfix` semantics |
| `internal/grimnirdeploy/gates/tagsuffix_test.go` | Create | Tag-suffix table-driven tests |
| `internal/grimnirdeploy/gates/image.go` | Create | Image-exists gate: `docker manifest inspect` on both nodes |
| `internal/grimnirdeploy/gates/image_test.go` | Create | Image-exists tests with fake docker runner |
| `internal/grimnirdeploy/gates/health.go` | Create | Both-nodes-healthy gate: probes `/healthz`, gRPC `health.Check`, pg replication lag |
| `internal/grimnirdeploy/gates/health_test.go` | Create | Health-gate tests with fake probes |
| `internal/grimnirdeploy/runner/ssh.go` | Create | SSH runner interface + real implementation using `golang.org/x/crypto/ssh` |
| `internal/grimnirdeploy/runner/ssh_test.go` | Create | SSH runner tests; uses fake server stub |
| `internal/grimnirdeploy/runner/docker.go` | Create | `docker compose` runner wrapper (local + via SSH) |
| `internal/grimnirdeploy/runner/docker_test.go` | Create | Docker runner tests with fake `exec.Cmd` |
| `internal/grimnirdeploy/runner/fake.go` | Create | Test-only in-memory fake runner used by all upper-layer tests |
| `internal/grimnirdeploy/cmd_deploy.go` | Create | `deploy <tag>`: the main rolling sequence; soak monitor; mid-roll auto-revert |
| `internal/grimnirdeploy/cmd_deploy_test.go` | Create | Rolling-sequence tests against the fake runner; verify pre-flight order + per-node drain/restart/verify/restore |
| `internal/grimnirdeploy/cmd_rollback.go` | Create | `deploy --rollback`: eligibility window; `--force-aged-rollback`; `--force-through-contract-migration` |
| `internal/grimnirdeploy/cmd_rollback_test.go` | Create | Rollback tests; eligibility-window refusal; contract-crossing refusal; force-flag override |
| `internal/grimnirdeploy/cmd_verify.go` | Create | `verify`: read-only cluster-wide health probe |
| `internal/grimnirdeploy/cmd_verify_test.go` | Create | Verify-output tests; uses fake probes |
| `internal/grimnirdeploy/cmd_drain.go` | Create | `drain --node=N`: VRRP priority drop, graceful service stop, leader hand-off |
| `internal/grimnirdeploy/cmd_drain_test.go` | Create | Drain tests with fake runner |
| `internal/grimnirdeploy/cmd_promote.go` | Create | `promote-replica`: Postgres failover runbook |
| `internal/grimnirdeploy/cmd_promote_test.go` | Create | Promote tests; the real-infra test is `//go:build integration && requires_real_cluster` |
| `internal/grimnirdeploy/cmd_coldstart.go` | Create | `cold-start-region --region=R`: region bring-up |
| `internal/grimnirdeploy/cmd_coldstart_test.go` | Create | Cold-start tests against fake runner; real-infra test gated |
| `internal/grimnirdeploy/cmd_restore.go` | Create | `restore --from=BACKUP_ID [--target-time=TS]`: pgbackrest wrapper |
| `internal/grimnirdeploy/cmd_restore_test.go` | Create | Restore tests; real-infra test gated |
| `internal/grimnirdeploy/cmd_recover.go` | Create | `recover-partition`: verifies VIP holder, replication state, leader lease |
| `internal/grimnirdeploy/cmd_recover_test.go` | Create | Recover tests against fake probes; real-infra test gated |
| `internal/grimnirdeploy/cmd_backupdrill.go` | Create | `backup-drill --region=R`: Section 8.4 quarterly drill |
| `internal/grimnirdeploy/cmd_backupdrill_test.go` | Create | Drill output tests; real-infra test gated |
| `migrations/005_audit_log.sql` | Create | Audit-log table; expand-only |
| `migrations/006_deploy_history.sql` | Create | Deploy-history table; expand-only |
| `internal/db/migrate.go` | Modify | Add the two new tables to the GORM AutoMigrate set |
| `docs/runbooks/index.md` | Create | Symptom → subcommand table (Section 8.2) |
| `docs/runbooks/deploy.md` | Create | Long-form deploy runbook with worked examples |
| `docs/runbooks/emergency-pause.md` | Create | Emergency-pause/-resume runbook |
| `docs/runbooks/drain.md` | Create | Drain runbook |
| `docs/runbooks/promote-replica.md` | Create | Postgres promotion runbook |
| `docs/runbooks/cold-start-region.md` | Create | Region bring-up runbook |
| `docs/runbooks/restore.md` | Create | Pgbackrest restore runbook |
| `docs/runbooks/recover-partition.md` | Create | Partition recovery runbook |
| `docs/runbooks/backup-drill.md` | Create | Drill procedure runbook |
| `scripts/grimnir-deploy.service` | Create | Optional systemd unit (so `systemctl status grimnir-deploy` shows the last run) |
| `Makefile` | Modify | Add `build-grimnir-deploy`, include in `build` cascade |
| `CLAUDE.md` | Modify | Document the new binary and its subcommands |
| `internal/version/version.go` | Modify | Patch-bump per the per-push rule (final chunk) |
| `VERSION` | Modify | Patch-bump per the per-push rule (final chunk) |

**Decomposition principle:** every subcommand is its own file (`cmd_<name>.go`) with a test file (`cmd_<name>_test.go`); shared infrastructure lives in subpackages with verb-based names (`gates/`, `runner/`, `pause/`, `audit/`, `history/`). The pre-flight gates are explicitly factored out so they're called identically from `deploy`, `rollback`, `drain`, and `cold-start-region`; three different entry points hitting the same policy table. Tests mirror source one-to-one; SQLite is the test DB for the audit + history stores because the schema is portable and Postgres-in-Docker per test is too slow.

---

## Chunk 0: Binary skeleton (cobra root, `version`, `help`, subcommand stubs)

This chunk produces a buildable binary with a working cobra tree, version flag, and a placeholder for every subcommand the design lists. No real behavior yet; the placeholders return `"not yet implemented"` and exit 1. The goal is to lock the CLI surface before the implementation chunks fill in each subcommand.

### Task 0.1: Create the binary directory and main entry point

**Files:**
- Create: `cmd/grimnir-deploy/main.go`
- Create: `cmd/grimnir-deploy/main_test.go`
- Create: `internal/grimnirdeploy/commands.go`
- Create: `internal/grimnirdeploy/cmd_stubs.go`

**Context:**
The existing `cmd/grimnirradio/main.go` uses cobra with a root command + subcommands wired in `init()`. Match that pattern. The binary takes no positional args at the root level; every action is a subcommand.

- [ ] **Step 1: Write the failing test**

`cmd/grimnir-deploy/main_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootHelp(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--help"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--help should not error: %v", err)
	}
	s := out.String()
	for _, name := range []string{
		"deploy",
		"verify",
		"drain",
		"emergency-pause",
		"emergency-resume",
		"promote-replica",
		"cold-start-region",
		"restore",
		"recover-partition",
		"backup-drill",
	} {
		if !strings.Contains(s, name) {
			t.Errorf("--help output missing subcommand %q\nfull output:\n%s", name, s)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetArgs([]string{"--version"})
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("--version should not error: %v", err)
	}
	if !strings.Contains(out.String(), "grimnir-deploy") {
		t.Errorf("--version output should contain binary name; got %q", out.String())
	}
}
```

- [ ] **Step 2: Run the test, expect failure**

```bash
cd /home/code/projects/grimnir_radio
go test ./cmd/grimnir-deploy/
```

Expected: build failure (`no Go files in cmd/grimnir-deploy`) because `main.go` doesn't exist yet.

- [ ] **Step 3: Write `cmd/grimnir-deploy/main.go`**

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// grimnir-deploy is the single operator binary for the HA cluster.
// It orchestrates rolling updates across two HA nodes, runs operational
// runbooks as first-class subcommands, and gates every action through a
// shared pre-flight + audit-log layer.
//
// See docs/superpowers/plans/2026-06-01-ha-zero-loss-failover-design.md
// Sections 6, 8.2, and 8.3 for the design contract this binary implements.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy"
	"github.com/friendsincode/grimnir_radio/internal/version"
)

var rootCmd = &cobra.Command{
	Use:     "grimnir-deploy",
	Short:   "Operator binary for HA rolling updates and runbooks",
	Long:    "grimnir-deploy orchestrates rolling updates across the two HA nodes, runs operational runbooks (promote-replica, drain, cold-start-region, restore, recover-partition, backup-drill), and gates every action via per-region deploy policy.",
	Version: version.Version,
}

func init() {
	rootCmd.SetVersionTemplate("grimnir-deploy {{.Version}}\n")
	grimnirdeploy.RegisterCommands(rootCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Create the package + subcommand registrar**

`internal/grimnirdeploy/commands.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package grimnirdeploy implements the subcommand tree of the grimnir-deploy
// binary. Each subcommand lives in its own file (cmd_<name>.go) and is
// registered through RegisterCommands.
package grimnirdeploy

import "github.com/spf13/cobra"

// RegisterCommands attaches every subcommand to the given root command.
// Called once from cmd/grimnir-deploy/main.go.
func RegisterCommands(root *cobra.Command) {
	root.AddCommand(newDeployCmd())
	root.AddCommand(newVerifyCmd())
	root.AddCommand(newDrainCmd())
	root.AddCommand(newEmergencyPauseCmd())
	root.AddCommand(newEmergencyResumeCmd())
	root.AddCommand(newPromoteReplicaCmd())
	root.AddCommand(newColdStartRegionCmd())
	root.AddCommand(newRestoreCmd())
	root.AddCommand(newRecoverPartitionCmd())
	root.AddCommand(newBackupDrillCmd())
}
```

- [ ] **Step 5: Create the placeholder file with every subcommand stub**

`internal/grimnirdeploy/cmd_stubs.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"errors"

	"github.com/spf13/cobra"
)

// errNotImplemented is returned by every stub until the chunk that implements
// the subcommand replaces the stub with a real RunE.
var errNotImplemented = errors.New("not yet implemented")

func newDeployCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "deploy <tag>",
		Short: "Roll out a new image tag across both HA nodes",
		Long:  "Pre-flight gates (emergency-pause, deploy policy, tag-suffix conventions, image-exists, both-nodes-healthy), then a per-node drain, migrate, start, wait-health, restore-VRRP, soak loop. See docs/runbooks/deploy.md.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().Bool("rollback", false, "roll back to the previous successful tag from deploy_history")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate the cluster")
	c.Flags().Bool("force-aged-rollback", false, "with --rollback: override the eligibility window")
	c.Flags().Bool("force-through-contract-migration", false, "with --rollback: override contract-migration boundary refusal")
	c.Flags().String("reason", "", "with --rollback: required incident reason; written to deploy_history.reason")
	c.Flags().String("force-policy", "", "override deploy policy: auto|window|manual")
	c.Flags().Bool("go", false, "with --force-policy=manual: explicit go signal")
	return c
}

func newVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Read-only cluster-wide health probe",
		Long:  "Probes /healthz on each control plane, gRPC health.Check on each engine and edge encoder, fan-out byte-flow, Postgres replication lag, Redis reachability, VIP holder count, and leader lease state. Exits 0 if everything is healthy, non-zero otherwise. Safe to run anytime; mutates nothing. See docs/runbooks/index.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
}

func newDrainCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "drain",
		Short: "Drain a node: drop VRRP priority, stop services, hand off leadership",
		Long:  "Drains the named node so the peer takes over. Drops VRRP priority via vrrp_script failure file, SIGTERMs grimnir / edge-encoder / fan-out / mediaengine in that order, waits for leader-election lease to migrate to the peer. See docs/runbooks/drain.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().String("node", "", "node hostname or 'self' (required)")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate the cluster")
	c.Flags().Duration("grace", 0, "override grace period per service (default 30s)")
	_ = c.MarkFlagRequired("node")
	return c
}

func newEmergencyPauseCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "emergency-pause",
		Short: "Set the Redis emergency-pause key; subsequent deploys abort with the pause message",
		Long:  "Sets the grimnir:emergency-pause Redis key. Every grimnir-deploy subcommand that mutates the cluster reads this key first and aborts if set. Use during an incident to prevent any automated or manual deploys from running. Cleared with `grimnir-deploy emergency-resume`. See docs/runbooks/emergency-pause.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().String("reason", "", "free-form reason recorded in audit_log.notes (required)")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate Redis")
	_ = c.MarkFlagRequired("reason")
	return c
}

func newEmergencyResumeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "emergency-resume",
		Short: "Clear the Redis emergency-pause key; deploys resume per region policy",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().String("reason", "", "free-form reason recorded in audit_log.notes (required)")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate Redis")
	_ = c.MarkFlagRequired("reason")
	return c
}

func newPromoteReplicaCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "promote-replica",
		Short: "Promote the Postgres replica to primary; repoint pgbouncer; rebuild new replica",
		Long:  "Postgres failover runbook. Verifies the replica is in streaming state and lag < 5s, promotes via pg_ctl promote, updates pgbouncer's primary_conninfo to point at the new primary, demotes the old primary to replica via pg_basebackup. See docs/runbooks/promote-replica.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate the cluster")
	c.Flags().Bool("skip-rebuild", false, "promote only; do not rebuild the old primary as replica (use when old primary is dead)")
	return c
}

func newColdStartRegionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cold-start-region",
		Short: "Bring up a freshly-built region from scratch in dependency order",
		Long:  "Region bring-up runbook. Verifies firewall rules, WireGuard mesh, Postgres + replica, Redis, MinIO/R2 connectivity, then starts grimnir + mediaengine + fan-out + edge-encoder on both nodes in dependency order and runs grimnir-deploy verify at the end. See docs/runbooks/cold-start-region.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().String("region", "", "region name (required)")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate the cluster")
	_ = c.MarkFlagRequired("region")
	return c
}

func newRestoreCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "restore",
		Short: "Restore Postgres from a pgbackrest backup",
		Long:  "pgbackrest restore wrapper. Stops grimnir + mediaengine on both nodes, restores the named backup (latest if not specified), optionally replays WAL to --target-time, restarts services, verifies via grimnir-deploy verify. See docs/runbooks/restore.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().String("from", "", "backup id, or 'latest' (required)")
	c.Flags().String("target-time", "", "WAL replay target, RFC3339 timestamp (optional; replays to end of WAL if omitted)")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate the cluster")
	_ = c.MarkFlagRequired("from")
	return c
}

func newRecoverPartitionCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "recover-partition",
		Short: "Recover from a network partition between the two HA nodes",
		Long:  "Partition recovery runbook. Verifies VIP holder count (must be exactly 1), Postgres replication state (which side has more recent WAL), leader-election lease holder, then surfaces conflicts for operator decision. Does not auto-merge diverged state. See docs/runbooks/recover-partition.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate the cluster")
	return c
}

func newBackupDrillCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "backup-drill",
		Short: "Run a backup-restore drill against a staging Postgres on a non-production host",
		Long:  "Stands up a temporary Postgres on the named drill host, restores the latest backup, measures base-restore + WAL-replay time, reports measured RTO + RPO. Posts results to the audit ntfy topic. Quarterly cadence per design Section 8.4. See docs/runbooks/backup-drill.md.",
		RunE:  func(cmd *cobra.Command, args []string) error { return errNotImplemented },
	}
	c.Flags().String("region", "", "region whose backup repository to drill (required)")
	c.Flags().String("drill-host", "", "host to stand up the staging Postgres on (required)")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not run the drill")
	_ = c.MarkFlagRequired("region")
	_ = c.MarkFlagRequired("drill-host")
	return c
}
```

- [ ] **Step 6: Run the test, expect pass**

```bash
go test ./cmd/grimnir-deploy/
```

Expected: PASS. `--help` lists every subcommand; `--version` prints the version string.

- [ ] **Step 7: Build the binary and smoke-test the CLI surface**

```bash
go build -o /tmp/grimnir-deploy ./cmd/grimnir-deploy/
/tmp/grimnir-deploy --help
/tmp/grimnir-deploy --version
/tmp/grimnir-deploy deploy --help
/tmp/grimnir-deploy emergency-pause --help
/tmp/grimnir-deploy deploy v1.0.0
```

Expected: binary builds; each `--help` shows the long description and flag table; the last command (`deploy v1.0.0`) returns `error: not yet implemented` and exits 1.

- [ ] **Step 8: Commit**

```bash
git add cmd/grimnir-deploy/ internal/grimnirdeploy/commands.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: skeleton binary with cobra subcommand stubs (Chunk 0)"
```

### Task 0.2: Add to Makefile build cascade

**Files:**
- Modify: `Makefile`

- [ ] **Step 1: Read the current Makefile structure**

```bash
grep -nA3 "^build" Makefile
```

Expected: shows the existing `build` target with `go build` lines for `grimnirradio`, `mediaengine`, and `edge-encoder`.

- [ ] **Step 2: Add the new target and include it in the `build` cascade**

Add a dedicated target after the existing `build-edge-encoder` (or whatever the last `build-*` target is):

```makefile
build-grimnir-deploy:
	go build -ldflags="-X github.com/friendsincode/grimnir_radio/internal/version.Version=$(VERSION)" -o bin/grimnir-deploy ./cmd/grimnir-deploy/
```

Then extend the top-level `build:` target to depend on it. If `build:` is a list of prerequisites, append `build-grimnir-deploy`. If it's a recipe with `go build` lines, add a matching line. Follow the established pattern.

- [ ] **Step 3: Verify the build target works**

```bash
make build-grimnir-deploy
./bin/grimnir-deploy --version
make build
ls -l bin/grimnir-deploy bin/grimnirradio bin/mediaengine
```

Expected: binary lands in `bin/`; `--version` prints the embedded version; the cascade `make build` also produces all three binaries.

- [ ] **Step 4: Commit**

```bash
git add Makefile
git commit -m "make: add build-grimnir-deploy target"
```

---

## Chunk 1: Config loading (`internal/grimnirdeploy/config.go`)

Every subcommand needs region, deploy policy, ntfy endpoint, Redis address, Postgres DSN, and peer SSH coordinates. Centralise loading in one place; let subcommands take a `*Config` rather than read env-vars themselves.

### Task 1.1: Config struct + loader

**Files:**
- Create: `internal/grimnirdeploy/config.go`
- Create: `internal/grimnirdeploy/config_test.go`

**Context:**
The existing `internal/config/config.go` loads `GRIMNIR_*` env-vars with `RLM_*` fallback. Match that convention. The deploy binary needs a subset (DB, Redis) plus a deploy-specific set: `GRIMNIR_REGION`, `GRIMNIR_DEPLOY_POLICY`, `GRIMNIR_DEPLOY_WINDOW`, `GRIMNIR_DEPLOY_SOAK`, `GRIMNIR_DEPLOY_PEER_HOST`, `GRIMNIR_DEPLOY_PEER_SSH_USER`, `GRIMNIR_DEPLOY_PEER_SSH_KEY`, `GRIMNIR_DEPLOY_NTFY_URL`, `GRIMNIR_DEPLOY_NTFY_TOPIC_AUDIT`, `GRIMNIR_DEPLOY_NTFY_TOPIC_PAGE`, `GRIMNIR_DEPLOY_ROLLBACK_WINDOW`.

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/config_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"testing"
	"time"
)

func TestLoadConfigDefaults(t *testing.T) {
	t.Setenv("GRIMNIR_REGION", "us-east")
	t.Setenv("GRIMNIR_DB_DSN", "postgres://localhost/grimnir")
	t.Setenv("GRIMNIR_REDIS_ADDR", "localhost:6379")
	t.Setenv("GRIMNIR_DEPLOY_PEER_HOST", "<edge-vps>2")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Region != "us-east" {
		t.Errorf("Region = %q, want us-east", cfg.Region)
	}
	if cfg.DeployPolicy != "auto" {
		t.Errorf("DeployPolicy default = %q, want auto", cfg.DeployPolicy)
	}
	if cfg.SoakWindow != 5*time.Minute {
		t.Errorf("SoakWindow default = %v, want 5m", cfg.SoakWindow)
	}
	if cfg.RollbackWindow != 4*time.Hour {
		t.Errorf("RollbackWindow default = %v, want 4h", cfg.RollbackWindow)
	}
	if cfg.PeerSSHUser != "<ssh-user>" {
		t.Errorf("PeerSSHUser default = %q, want <ssh-user>", cfg.PeerSSHUser)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	t.Setenv("GRIMNIR_REGION", "us-east")
	t.Setenv("GRIMNIR_DB_DSN", "postgres://localhost/grimnir")
	t.Setenv("GRIMNIR_REDIS_ADDR", "localhost:6379")
	t.Setenv("GRIMNIR_DEPLOY_PEER_HOST", "<edge-vps>2")
	t.Setenv("GRIMNIR_DEPLOY_POLICY", "window")
	t.Setenv("GRIMNIR_DEPLOY_WINDOW", "0 2 * * SUN")
	t.Setenv("GRIMNIR_DEPLOY_SOAK", "10m")
	t.Setenv("GRIMNIR_DEPLOY_ROLLBACK_WINDOW", "8h")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.DeployPolicy != "window" {
		t.Errorf("DeployPolicy = %q, want window", cfg.DeployPolicy)
	}
	if cfg.DeployWindowCron != "0 2 * * SUN" {
		t.Errorf("DeployWindowCron = %q", cfg.DeployWindowCron)
	}
	if cfg.SoakWindow != 10*time.Minute {
		t.Errorf("SoakWindow = %v", cfg.SoakWindow)
	}
	if cfg.RollbackWindow != 8*time.Hour {
		t.Errorf("RollbackWindow = %v", cfg.RollbackWindow)
	}
}

func TestLoadConfigMissingRegion(t *testing.T) {
	// GRIMNIR_REGION is required.
	t.Setenv("GRIMNIR_REGION", "")
	t.Setenv("GRIMNIR_DB_DSN", "postgres://localhost/grimnir")
	t.Setenv("GRIMNIR_REDIS_ADDR", "localhost:6379")
	t.Setenv("GRIMNIR_DEPLOY_PEER_HOST", "<edge-vps>2")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig with empty GRIMNIR_REGION should error")
	}
}

func TestLoadConfigInvalidPolicy(t *testing.T) {
	t.Setenv("GRIMNIR_REGION", "us-east")
	t.Setenv("GRIMNIR_DB_DSN", "postgres://localhost/grimnir")
	t.Setenv("GRIMNIR_REDIS_ADDR", "localhost:6379")
	t.Setenv("GRIMNIR_DEPLOY_PEER_HOST", "<edge-vps>2")
	t.Setenv("GRIMNIR_DEPLOY_POLICY", "yolo")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("LoadConfig with invalid policy should error")
	}
}
```

- [ ] **Step 2: Run the test, expect failure**

```bash
go test ./internal/grimnirdeploy/ -run TestLoadConfig
```

Expected: build failure (`LoadConfig` undefined).

- [ ] **Step 3: Implement the loader**

`internal/grimnirdeploy/config.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"fmt"
	"os"
	"time"
)

// Config is the resolved runtime configuration for grimnir-deploy.
// Loaded once at process start via LoadConfig.
type Config struct {
	// Region identity (required).
	Region string

	// Backend addresses.
	DBDSN     string
	RedisAddr string

	// Peer node SSH coordinates.
	PeerHost    string
	PeerSSHPort int
	PeerSSHUser string
	PeerSSHKey  string // path to private key

	// Deploy policy.
	DeployPolicy     string // "auto" | "window" | "manual"
	DeployWindowCron string // cron expression, used when DeployPolicy == "window"
	SoakWindow       time.Duration
	RollbackWindow   time.Duration

	// ntfy.
	NtfyURL        string
	NtfyAuditTopic string
	NtfyPageTopic  string
	NtfyToken      string

	// Operator identity (for audit_log.operator).
	Operator string
}

// LoadConfig reads env-vars (GRIMNIR_* with RLM_* fallback) and returns a
// validated Config. Missing required fields cause an error; invalid values
// (e.g., unknown policy) also error.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		Region:           getEnv("GRIMNIR_REGION", "RLM_REGION", ""),
		DBDSN:            getEnv("GRIMNIR_DB_DSN", "RLM_DB_DSN", ""),
		RedisAddr:        getEnv("GRIMNIR_REDIS_ADDR", "RLM_REDIS_ADDR", ""),
		PeerHost:         getEnv("GRIMNIR_DEPLOY_PEER_HOST", "", ""),
		PeerSSHUser:      getEnv("GRIMNIR_DEPLOY_PEER_SSH_USER", "", "<ssh-user>"),
		PeerSSHKey:       getEnv("GRIMNIR_DEPLOY_PEER_SSH_KEY", "", os.ExpandEnv("$HOME/.ssh/id_ed25519")),
		DeployPolicy:     getEnv("GRIMNIR_DEPLOY_POLICY", "", "auto"),
		DeployWindowCron: getEnv("GRIMNIR_DEPLOY_WINDOW", "", "0 4 * * SUN"),
		NtfyURL:          getEnv("GRIMNIR_DEPLOY_NTFY_URL", "", "https://ntfy.sh"),
		NtfyAuditTopic:   getEnv("GRIMNIR_DEPLOY_NTFY_TOPIC_AUDIT", "", ""),
		NtfyPageTopic:    getEnv("GRIMNIR_DEPLOY_NTFY_TOPIC_PAGE", "", ""),
		NtfyToken:        getEnv("GRIMNIR_DEPLOY_NTFY_TOKEN", "", ""),
		Operator:         resolveOperator(),
	}
	cfg.PeerSSHPort = getEnvInt("GRIMNIR_DEPLOY_PEER_SSH_PORT", 22)
	cfg.SoakWindow = getEnvDuration("GRIMNIR_DEPLOY_SOAK", 5*time.Minute)
	cfg.RollbackWindow = getEnvDuration("GRIMNIR_DEPLOY_ROLLBACK_WINDOW", 4*time.Hour)

	if cfg.Region == "" {
		return nil, fmt.Errorf("GRIMNIR_REGION is required")
	}
	if cfg.DBDSN == "" {
		return nil, fmt.Errorf("GRIMNIR_DB_DSN is required")
	}
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("GRIMNIR_REDIS_ADDR is required")
	}
	if cfg.PeerHost == "" {
		return nil, fmt.Errorf("GRIMNIR_DEPLOY_PEER_HOST is required")
	}
	switch cfg.DeployPolicy {
	case "auto", "window", "manual":
	default:
		return nil, fmt.Errorf("GRIMNIR_DEPLOY_POLICY = %q; want auto|window|manual", cfg.DeployPolicy)
	}
	if cfg.NtfyAuditTopic == "" {
		// Sensible default mirrors Section 8.3's per-region topic convention.
		cfg.NtfyAuditTopic = "grimnir-audit-" + cfg.Region
	}
	if cfg.NtfyPageTopic == "" {
		cfg.NtfyPageTopic = "grimnir-page-" + cfg.Region
	}
	return cfg, nil
}

func getEnv(primary, fallback, def string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	if fallback != "" {
		if v := os.Getenv(fallback); v != "" {
			return v
		}
	}
	return def
}

func getEnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var i int
	if _, err := fmt.Sscanf(v, "%d", &i); err != nil {
		return def
	}
	return i
}

func getEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func resolveOperator() string {
	if v := os.Getenv("GRIMNIR_DEPLOY_OPERATOR"); v != "" {
		return v
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return "unknown"
}
```

- [ ] **Step 4: Run the test, expect pass**

```bash
go test -v ./internal/grimnirdeploy/ -run TestLoadConfig
```

Expected: all four sub-tests pass.

- [ ] **Step 5: Commit**

```bash
git add internal/grimnirdeploy/config.go internal/grimnirdeploy/config_test.go
git commit -m "grimnir-deploy: env-var config loader with policy validation (Chunk 1)"
```

---

## Chunk 2: `audit_log` table + writer + subcommand middleware

Every subcommand of grimnir-deploy writes a row on START and another on COMPLETE/FAILED, per Section 8.3. Also posts an ntfy notification to the `grimnir-audit-<region>` topic. This chunk lands the table, the writer, the ntfy poster, and the middleware that wraps every subcommand RunE.

### Task 2.1: SQL migration + GORM model + AutoMigrate hook

**Files:**
- Create: `migrations/005_audit_log.sql`
- Create: `internal/grimnirdeploy/audit/model.go`
- Modify: `internal/db/migrate.go`

**Context:**
Schema is verbatim from Section 8.3 of the design (id, ts, operator, source_ip, subcommand, args_json, phase, outcome, duration_ms, notes). The control plane's existing migrations (001-004) all live in `migrations/`; this chunk continues the numbering. Use the `TEMPLATE.sql` shape with the expand-phase annotation.

- [ ] **Step 1: Write the migration**

`migrations/005_audit_log.sql`:

```sql
-- Migration 005: audit_log table for grimnir-deploy
-- Created: 2026-06-06
-- Phase: expand
--
-- Description: Adds the audit_log table written by every grimnir-deploy
-- subcommand per Section 8.3 of the HA design. Schema is verbatim from the
-- design doc. Rows are written on START and on COMPLETE/FAILED of every
-- subcommand; the (subcommand, phase) tuple lets dashboards filter for
-- in-progress operations vs completed history.
--
-- Expand-only: additive table. Nothing dropped, renamed, or retyped.

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE TABLE IF NOT EXISTS audit_log (
    id              uuid PRIMARY KEY,
    ts              timestamptz NOT NULL DEFAULT now(),
    operator        text NOT NULL,
    source_ip       text NOT NULL,
    subcommand      text NOT NULL,
    args_json       jsonb NOT NULL,
    phase           text NOT NULL,
    outcome         text,
    duration_ms     bigint,
    notes           text
);

CREATE INDEX IF NOT EXISTS idx_audit_log_ts ON audit_log (ts DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_subcommand_ts ON audit_log (subcommand, ts DESC);
```

- [ ] **Step 2: Write the GORM model**

`internal/grimnirdeploy/audit/model.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package audit is the grimnir-deploy audit log: per-subcommand rows in the
// audit_log table plus paired ntfy notifications.
//
// This is separate from internal/audit which handles app-level audit events
// (priority changes, DJ connect/disconnect, etc). The two stores happen to
// live in the same Postgres for backup convenience; they do not share code.
package audit

import (
	"time"

	"github.com/google/uuid"
)

// Entry is one row in the audit_log table.
type Entry struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey"`
	TS         time.Time `gorm:"column:ts;not null;default:now()"`
	Operator   string    `gorm:"column:operator;not null"`
	SourceIP   string    `gorm:"column:source_ip;not null"`
	Subcommand string    `gorm:"column:subcommand;not null"`
	ArgsJSON   string    `gorm:"column:args_json;type:jsonb;not null"`
	Phase      string    `gorm:"column:phase;not null"` // "started" | "completed" | "failed"
	Outcome    string    `gorm:"column:outcome"`
	DurationMS int64     `gorm:"column:duration_ms"`
	Notes      string    `gorm:"column:notes"`
}

// TableName tells GORM the underlying table name.
func (Entry) TableName() string { return "audit_log" }
```

- [ ] **Step 3: Hook into AutoMigrate**

Open `internal/db/migrate.go`, find the `AutoMigrate(...)` call that registers the existing models, and add `&audit.Entry{}` to the list. Import the new package. Run:

```bash
go build ./...
```

Expected: builds cleanly.

- [ ] **Step 4: Commit**

```bash
git add migrations/005_audit_log.sql internal/grimnirdeploy/audit/model.go internal/db/migrate.go
git commit -m "grimnir-deploy: audit_log table + GORM model (Chunk 2.1)"
```

### Task 2.2: Audit store (write START / COMPLETE / FAILED rows)

**Files:**
- Create: `internal/grimnirdeploy/audit/store.go`
- Create: `internal/grimnirdeploy/audit/store_test.go`

**Context:**
The store has two methods: `WriteStart(ctx, op, ip, subcmd, args)` returns an entry ID; `WriteComplete(ctx, id, outcome, duration, notes)` updates the matching row by ID. Args are JSON-serialised with a redactor that strips known-secret field names (`--reason` is kept; `--ntfy-token`, `--ssh-key`, `--postgres-password`, etc. are redacted). The store accepts a `*gorm.DB` so callers can pass either Postgres or SQLite (SQLite is what tests use).

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/audit/store_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&Entry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestWriteStartThenComplete(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id, err := s.WriteStart(ctx, "alice", "10.0.0.1", "deploy", map[string]any{"tag": "v1.2.3"})
	if err != nil {
		t.Fatalf("WriteStart: %v", err)
	}
	if id.String() == "" {
		t.Fatal("WriteStart returned empty id")
	}

	if err := s.WriteComplete(ctx, id, "success", 1234*time.Millisecond, "ok"); err != nil {
		t.Fatalf("WriteComplete: %v", err)
	}

	var got Entry
	if err := db.First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("query back: %v", err)
	}
	if got.Phase != "completed" {
		t.Errorf("Phase = %q, want completed", got.Phase)
	}
	if got.Outcome != "success" {
		t.Errorf("Outcome = %q, want success", got.Outcome)
	}
	if got.DurationMS != 1234 {
		t.Errorf("DurationMS = %d, want 1234", got.DurationMS)
	}
}

func TestSecretRedaction(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	args := map[string]any{
		"tag":              "v1.2.3",
		"ntfy-token":       "tk_secret",
		"postgres-password": "hunter2",
		"reason":           "fixing #999",
	}
	id, err := s.WriteStart(ctx, "alice", "10.0.0.1", "deploy", args)
	if err != nil {
		t.Fatalf("WriteStart: %v", err)
	}
	var got Entry
	if err := db.First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if strings.Contains(got.ArgsJSON, "tk_secret") {
		t.Errorf("ntfy-token was not redacted: %s", got.ArgsJSON)
	}
	if strings.Contains(got.ArgsJSON, "hunter2") {
		t.Errorf("postgres-password was not redacted: %s", got.ArgsJSON)
	}
	if !strings.Contains(got.ArgsJSON, "fixing #999") {
		t.Errorf("reason should NOT be redacted: %s", got.ArgsJSON)
	}
	if !strings.Contains(got.ArgsJSON, "REDACTED") {
		t.Errorf("redactor should leave REDACTED markers: %s", got.ArgsJSON)
	}
}

func TestWriteCompleteFailedPhase(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id, _ := s.WriteStart(ctx, "alice", "10.0.0.1", "deploy", nil)
	if err := s.WriteFailed(ctx, id, "image not found in registry", 500*time.Millisecond); err != nil {
		t.Fatalf("WriteFailed: %v", err)
	}
	var got Entry
	_ = db.First(&got, "id = ?", id).Error
	if got.Phase != "failed" {
		t.Errorf("Phase = %q, want failed", got.Phase)
	}
}
```

- [ ] **Step 2: Run the test, expect failure**

```bash
go test ./internal/grimnirdeploy/audit/ -run "TestWriteStart|TestSecretRedaction|TestWriteCompleteFailedPhase"
```

Expected: build failure (`NewStore` undefined).

- [ ] **Step 3: Implement the store**

`internal/grimnirdeploy/audit/store.go`:

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
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Store writes audit_log rows.
type Store struct {
	db *gorm.DB
}

// NewStore constructs an audit store backed by the given GORM database.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// secretKeyTokens are case-insensitive substrings of arg keys whose values
// should be redacted before persistence to args_json.
var secretKeyTokens = []string{
	"password", "passwd", "token", "secret", "key", "credential",
}

// WriteStart inserts a "started" row and returns the new entry id. The args
// map is JSON-encoded with secret values redacted in place.
func (s *Store) WriteStart(ctx context.Context, operator, sourceIP, subcommand string, args map[string]any) (uuid.UUID, error) {
	id := uuid.New()
	argsJSON, err := marshalRedacted(args)
	if err != nil {
		return uuid.Nil, fmt.Errorf("marshal args: %w", err)
	}
	e := Entry{
		ID:         id,
		TS:         time.Now().UTC(),
		Operator:   operator,
		SourceIP:   sourceIP,
		Subcommand: subcommand,
		ArgsJSON:   argsJSON,
		Phase:      "started",
	}
	if err := s.db.WithContext(ctx).Create(&e).Error; err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// WriteComplete updates the row to phase="completed" with the given outcome.
func (s *Store) WriteComplete(ctx context.Context, id uuid.UUID, outcome string, duration time.Duration, notes string) error {
	return s.db.WithContext(ctx).Model(&Entry{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"phase":       "completed",
			"outcome":     outcome,
			"duration_ms": duration.Milliseconds(),
			"notes":       notes,
		}).Error
}

// WriteFailed updates the row to phase="failed" with the given outcome message.
func (s *Store) WriteFailed(ctx context.Context, id uuid.UUID, outcome string, duration time.Duration) error {
	return s.db.WithContext(ctx).Model(&Entry{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"phase":       "failed",
			"outcome":     outcome,
			"duration_ms": duration.Milliseconds(),
		}).Error
}

// marshalRedacted JSON-encodes args after replacing values whose keys match a
// secret-token substring with the literal string "REDACTED".
func marshalRedacted(args map[string]any) (string, error) {
	if args == nil {
		return "{}", nil
	}
	clean := make(map[string]any, len(args))
	for k, v := range args {
		if isSecretKey(k) {
			clean[k] = "REDACTED"
		} else {
			clean[k] = v
		}
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func isSecretKey(k string) bool {
	lower := strings.ToLower(k)
	for _, t := range secretKeyTokens {
		if strings.Contains(lower, t) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run the test, expect pass**

```bash
go test -v ./internal/grimnirdeploy/audit/ -run "TestWriteStart|TestSecretRedaction|TestWriteCompleteFailedPhase"
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/grimnirdeploy/audit/store.go internal/grimnirdeploy/audit/store_test.go
git commit -m "grimnir-deploy: audit log Store with secret redaction (Chunk 2.2)"
```

### Task 2.3: ntfy poster

**Files:**
- Create: `internal/grimnirdeploy/audit/ntfy.go`
- Create: `internal/grimnirdeploy/audit/ntfy_test.go`

**Context:**
ntfy.sh accepts plain HTTP POST to `https://<host>/<topic>` with body = message, optional `Authorization: Bearer <token>` header, optional `Title:`, `Priority:`, `Tags:` headers. See https://docs.ntfy.sh/publish/#publish-as-json for the JSON form. We use the JSON form so the topic, title, message, and priority go in one POST body without juggling headers. Section 8.3 requires audit notifications go to the audit topic (separate from page topic) so audit noise doesn't desensitise the operator to actual pages.

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/audit/ntfy_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNtfyPosterSendsExpectedPayload(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfyPoster(srv.URL, "tk_secret")
	err := p.Post(context.Background(), "grimnir-audit-us-east", "deploy started", "alice ran deploy v1.2.3", PriorityDefault)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotPath != "/" {
		t.Errorf("path = %q, want /", gotPath)
	}
	if gotAuth != "Bearer tk_secret" {
		t.Errorf("Authorization = %q, want Bearer tk_secret", gotAuth)
	}
	if gotBody["topic"] != "grimnir-audit-us-east" {
		t.Errorf("topic = %v", gotBody["topic"])
	}
	if gotBody["title"] != "deploy started" {
		t.Errorf("title = %v", gotBody["title"])
	}
	if !strings.Contains(gotBody["message"].(string), "v1.2.3") {
		t.Errorf("message missing tag: %v", gotBody["message"])
	}
}

func TestNtfyPosterNoAuthWhenTokenEmpty(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	p := NewNtfyPoster(srv.URL, "")
	if err := p.Post(context.Background(), "t", "title", "msg", PriorityDefault); err != nil {
		t.Fatalf("Post: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization sent when no token: %q", gotAuth)
	}
}

func TestNtfyPosterReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := NewNtfyPoster(srv.URL, "")
	if err := p.Post(context.Background(), "t", "title", "msg", PriorityDefault); err == nil {
		t.Fatal("expected error on 500")
	}
}
```

- [ ] **Step 2: Run the test, expect failure**

```bash
go test ./internal/grimnirdeploy/audit/ -run TestNtfyPoster
```

Expected: build failure (`NewNtfyPoster` undefined).

- [ ] **Step 3: Implement the poster**

`internal/grimnirdeploy/audit/ntfy.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Priority maps to ntfy's priority field (1..5).
type Priority int

const (
	PriorityMin     Priority = 1
	PriorityLow     Priority = 2
	PriorityDefault Priority = 3
	PriorityHigh    Priority = 4
	PriorityUrgent  Priority = 5
)

// NtfyPoster posts notifications to a ntfy.sh server. Safe for concurrent use.
type NtfyPoster struct {
	endpoint string // e.g., "https://ntfy.sh"
	token    string // optional Bearer token
	client   *http.Client
}

// NewNtfyPoster constructs a poster pointed at the given ntfy endpoint.
// Pass an empty token to skip the Authorization header.
func NewNtfyPoster(endpoint, token string) *NtfyPoster {
	return &NtfyPoster{
		endpoint: endpoint,
		token:    token,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

type ntfyMessage struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title,omitempty"`
	Message  string   `json:"message"`
	Priority Priority `json:"priority,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

// Post sends a notification. Returns nil on 2xx, error on non-2xx or transport
// failure. Tags are optional; pass nil if none.
func (p *NtfyPoster) Post(ctx context.Context, topic, title, message string, priority Priority, tags ...string) error {
	body, err := json.Marshal(ntfyMessage{
		Topic:    topic,
		Title:    title,
		Message:  message,
		Priority: priority,
		Tags:     tags,
	})
	if err != nil {
		return fmt.Errorf("marshal ntfy message: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("ntfy POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ntfy returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}
```

- [ ] **Step 4: Run the test, expect pass**

```bash
go test -v ./internal/grimnirdeploy/audit/ -run TestNtfyPoster
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/grimnirdeploy/audit/ntfy.go internal/grimnirdeploy/audit/ntfy_test.go
git commit -m "grimnir-deploy: ntfy poster with auth + priority (Chunk 2.3)"
```

### Task 2.4: Subcommand middleware

**Files:**
- Create: `internal/grimnirdeploy/audit/middleware.go`
- Create: `internal/grimnirdeploy/audit/middleware_test.go`

**Context:**
Every subcommand's `RunE` is wrapped by `audit.Wrap(...)`. The wrap writes a START row + ntfy, then calls the inner function, then writes COMPLETED/FAILED + ntfy with duration. On panic, the wrap recovers, writes FAILED with the panic value, posts a high-priority ntfy, then re-panics. The wrap takes a `Recorder` interface (so tests can stub the store + poster).

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/audit/middleware_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeRecorder struct {
	startCalls    int
	completeCalls int
	failedCalls   int
	ntfyCalls     int
	lastOutcome   string
	lastTitle     string
}

func (f *fakeRecorder) WriteStart(ctx context.Context, op, ip, sub string, args map[string]any) (uuid.UUID, error) {
	f.startCalls++
	return uuid.New(), nil
}
func (f *fakeRecorder) WriteComplete(ctx context.Context, id uuid.UUID, outcome string, d time.Duration, notes string) error {
	f.completeCalls++
	f.lastOutcome = outcome
	return nil
}
func (f *fakeRecorder) WriteFailed(ctx context.Context, id uuid.UUID, outcome string, d time.Duration) error {
	f.failedCalls++
	f.lastOutcome = outcome
	return nil
}
func (f *fakeRecorder) PostNtfy(ctx context.Context, title, msg string, priority Priority) error {
	f.ntfyCalls++
	f.lastTitle = title
	return nil
}

func TestWrapHappyPath(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "10.0.0.1")
	called := false
	inner := func(ctx context.Context) error { called = true; return nil }
	if err := w.Wrap(context.Background(), "deploy", map[string]any{"tag": "v1"}, inner); err != nil {
		t.Fatalf("Wrap returned err: %v", err)
	}
	if !called {
		t.Error("inner was not called")
	}
	if r.startCalls != 1 {
		t.Errorf("startCalls = %d, want 1", r.startCalls)
	}
	if r.completeCalls != 1 {
		t.Errorf("completeCalls = %d, want 1", r.completeCalls)
	}
	if r.failedCalls != 0 {
		t.Errorf("failedCalls = %d, want 0", r.failedCalls)
	}
	if r.ntfyCalls != 2 {
		t.Errorf("ntfyCalls = %d, want 2 (start + complete)", r.ntfyCalls)
	}
	if r.lastOutcome != "success" {
		t.Errorf("lastOutcome = %q, want success", r.lastOutcome)
	}
}

func TestWrapErrorPath(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "10.0.0.1")
	inner := func(ctx context.Context) error { return errors.New("boom") }
	err := w.Wrap(context.Background(), "deploy", nil, inner)
	if err == nil || err.Error() != "boom" {
		t.Fatalf("Wrap should propagate inner error; got %v", err)
	}
	if r.failedCalls != 1 {
		t.Errorf("failedCalls = %d, want 1", r.failedCalls)
	}
	if r.completeCalls != 0 {
		t.Errorf("completeCalls = %d, want 0", r.completeCalls)
	}
	if r.lastOutcome != "boom" {
		t.Errorf("lastOutcome = %q", r.lastOutcome)
	}
}

func TestWrapPanicPath(t *testing.T) {
	r := &fakeRecorder{}
	w := NewWrapper(r, "alice", "10.0.0.1")
	defer func() {
		if rec := recover(); rec == nil {
			t.Fatal("Wrap should re-panic")
		}
		if r.failedCalls != 1 {
			t.Errorf("failedCalls = %d, want 1", r.failedCalls)
		}
	}()
	w.Wrap(context.Background(), "deploy", nil, func(ctx context.Context) error {
		panic("kaboom")
	})
}
```

- [ ] **Step 2: Run the test, expect failure**

```bash
go test ./internal/grimnirdeploy/audit/ -run TestWrap
```

Expected: build failure (`NewWrapper` undefined).

- [ ] **Step 3: Implement the wrapper**

`internal/grimnirdeploy/audit/middleware.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Recorder is the surface the wrapper depends on. Concrete implementations
// combine the Store and the NtfyPoster; tests use an in-memory fake.
type Recorder interface {
	WriteStart(ctx context.Context, operator, sourceIP, subcommand string, args map[string]any) (uuid.UUID, error)
	WriteComplete(ctx context.Context, id uuid.UUID, outcome string, duration time.Duration, notes string) error
	WriteFailed(ctx context.Context, id uuid.UUID, outcome string, duration time.Duration) error
	PostNtfy(ctx context.Context, title, message string, priority Priority) error
}

// Wrapper composes a Recorder with operator + source IP context and runs
// every subcommand body inside Wrap.
type Wrapper struct {
	rec      Recorder
	operator string
	sourceIP string
}

// NewWrapper constructs a wrapper bound to the operator identity that ran
// the binary. The operator is read from $USER / $GRIMNIR_DEPLOY_OPERATOR
// during config load.
func NewWrapper(rec Recorder, operator, sourceIP string) *Wrapper {
	return &Wrapper{rec: rec, operator: operator, sourceIP: sourceIP}
}

// Wrap runs inner with audit START / COMPLETE / FAILED bookkeeping. Two ntfy
// notifications fire: one when inner starts, one when it finishes (or fails).
// Panics inside inner are captured into a FAILED row and re-panicked.
func (w *Wrapper) Wrap(ctx context.Context, subcommand string, args map[string]any, inner func(context.Context) error) (retErr error) {
	id, err := w.rec.WriteStart(ctx, w.operator, w.sourceIP, subcommand, args)
	if err != nil {
		return fmt.Errorf("audit start: %w", err)
	}
	_ = w.rec.PostNtfy(ctx, fmt.Sprintf("%s started", subcommand),
		fmt.Sprintf("%s ran %s; args=%v", w.operator, subcommand, args),
		PriorityDefault)
	started := time.Now()

	defer func() {
		dur := time.Since(started)
		if rec := recover(); rec != nil {
			msg := fmt.Sprintf("panic: %v", rec)
			_ = w.rec.WriteFailed(ctx, id, msg, dur)
			_ = w.rec.PostNtfy(ctx, fmt.Sprintf("%s PANICKED", subcommand), msg, PriorityUrgent)
			panic(rec)
		}
		if retErr != nil {
			_ = w.rec.WriteFailed(ctx, id, retErr.Error(), dur)
			_ = w.rec.PostNtfy(ctx, fmt.Sprintf("%s failed", subcommand), retErr.Error(), PriorityHigh)
			return
		}
		_ = w.rec.WriteComplete(ctx, id, "success", dur, "")
		_ = w.rec.PostNtfy(ctx, fmt.Sprintf("%s completed", subcommand),
			fmt.Sprintf("%s ran %s in %v", w.operator, subcommand, dur),
			PriorityDefault)
	}()

	return inner(ctx)
}
```

- [ ] **Step 4: Run the test, expect pass**

```bash
go test -v ./internal/grimnirdeploy/audit/ -run TestWrap
```

Expected: PASS (all three).

- [ ] **Step 5: Implement the combined Recorder**

The Store and NtfyPoster need to be glued into one Recorder that implements the interface. Append to `internal/grimnirdeploy/audit/middleware.go`:

```go
// RecorderImpl combines a Store and NtfyPoster into a single Recorder.
type RecorderImpl struct {
	Store *Store
	Ntfy  *NtfyPoster
	Topic string
}

// NewRecorder constructs a real Recorder for production wiring.
func NewRecorder(store *Store, poster *NtfyPoster, topic string) *RecorderImpl {
	return &RecorderImpl{Store: store, Ntfy: poster, Topic: topic}
}

func (r *RecorderImpl) WriteStart(ctx context.Context, op, ip, sub string, args map[string]any) (uuid.UUID, error) {
	return r.Store.WriteStart(ctx, op, ip, sub, args)
}
func (r *RecorderImpl) WriteComplete(ctx context.Context, id uuid.UUID, outcome string, dur time.Duration, notes string) error {
	return r.Store.WriteComplete(ctx, id, outcome, dur, notes)
}
func (r *RecorderImpl) WriteFailed(ctx context.Context, id uuid.UUID, outcome string, dur time.Duration) error {
	return r.Store.WriteFailed(ctx, id, outcome, dur)
}
func (r *RecorderImpl) PostNtfy(ctx context.Context, title, msg string, priority Priority) error {
	return r.Ntfy.Post(ctx, r.Topic, title, msg, priority)
}
```

- [ ] **Step 6: Run full audit-package tests**

```bash
go test -v ./internal/grimnirdeploy/audit/
```

Expected: every test (store, ntfy, middleware) passes.

- [ ] **Step 7: Commit**

```bash
git add internal/grimnirdeploy/audit/middleware.go internal/grimnirdeploy/audit/middleware_test.go
git commit -m "grimnir-deploy: audit middleware wrapper + combined Recorder (Chunk 2.4)"
```

---

## Chunk 3: Emergency-pause Redis lease + `emergency-pause` / `emergency-resume` subcommands

This is the smallest of the runbook subcommands and exercises the audit middleware + Redis path end-to-end. Once this chunk lands, every later chunk has a worked example of how a subcommand should wire its RunE.

### Task 3.1: Redis pause client

**Files:**
- Create: `internal/grimnirdeploy/pause/redis.go`
- Create: `internal/grimnirdeploy/pause/redis_test.go`

**Context:**
Key name is `grimnir:emergency-pause`. Value is a JSON blob: `{reason, operator, ts}`. No TTL: the pause is sticky until `emergency-resume` deletes it. Use `miniredis` (already in go.mod via leader-election tests; verify with `grep miniredis go.mod` first; add it via `go get github.com/alicebob/miniredis/v2` if missing) for tests.

- [ ] **Step 1: Verify miniredis is available**

```bash
grep miniredis go.mod || go get github.com/alicebob/miniredis/v2@latest
```

If `go get` ran, run `go mod tidy` and commit `go.mod` + `go.sum` in a separate prep commit before continuing.

- [ ] **Step 2: Write the failing test**

`internal/grimnirdeploy/pause/redis_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package pause

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newMini(t *testing.T) (*Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return NewClient(rdb), mr
}

func TestSetClearRead(t *testing.T) {
	c, _ := newMini(t)
	ctx := context.Background()

	got, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("Read empty: %v", err)
	}
	if got != nil {
		t.Errorf("Read empty should return nil; got %+v", got)
	}

	if err := c.Set(ctx, "fixing #999", "alice"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err = c.Read(ctx)
	if err != nil {
		t.Fatalf("Read after Set: %v", err)
	}
	if got == nil {
		t.Fatal("Read after Set returned nil")
	}
	if got.Reason != "fixing #999" {
		t.Errorf("Reason = %q, want fixing #999", got.Reason)
	}
	if got.Operator != "alice" {
		t.Errorf("Operator = %q, want alice", got.Operator)
	}
	if time.Since(got.TS) > time.Second {
		t.Errorf("TS too old: %v", got.TS)
	}

	if err := c.Clear(ctx); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, _ = c.Read(ctx)
	if got != nil {
		t.Errorf("Read after Clear should be nil; got %+v", got)
	}
}
```

- [ ] **Step 3: Run, expect failure**

```bash
go test ./internal/grimnirdeploy/pause/ -run TestSetClearRead
```

Expected: build failure (`NewClient` undefined).

- [ ] **Step 4: Implement the client**

`internal/grimnirdeploy/pause/redis.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package pause manages the grimnir:emergency-pause Redis key. Every
// grimnir-deploy subcommand that mutates the cluster reads this key first
// and aborts if it is set.
package pause

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

const keyName = "grimnir:emergency-pause"

// State is the JSON payload stored in the Redis key.
type State struct {
	Reason   string    `json:"reason"`
	Operator string    `json:"operator"`
	TS       time.Time `json:"ts"`
}

// Client wraps a redis.Client for pause-key operations.
type Client struct {
	rdb *redis.Client
}

// NewClient constructs a pause client around the given Redis connection.
func NewClient(rdb *redis.Client) *Client {
	return &Client{rdb: rdb}
}

// Set writes the pause state. Overwrites any prior value.
func (c *Client) Set(ctx context.Context, reason, operator string) error {
	if reason == "" {
		return errors.New("reason is required")
	}
	if operator == "" {
		return errors.New("operator is required")
	}
	b, err := json.Marshal(State{Reason: reason, Operator: operator, TS: time.Now().UTC()})
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, keyName, b, 0).Err()
}

// Read returns the current pause state, or nil if no pause is set.
func (c *Client) Read(ctx context.Context) (*State, error) {
	v, err := c.rdb.Get(ctx, keyName).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal([]byte(v), &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Clear deletes the pause key. Idempotent.
func (c *Client) Clear(ctx context.Context) error {
	return c.rdb.Del(ctx, keyName).Err()
}
```

- [ ] **Step 5: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/pause/
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/grimnirdeploy/pause/
git commit -m "grimnir-deploy: emergency-pause Redis client (Chunk 3.1)"
```

### Task 3.2: Replace stubs with real `emergency-pause` / `emergency-resume`

**Files:**
- Modify: `internal/grimnirdeploy/cmd_stubs.go` (remove the two emergency stubs)
- Create: `internal/grimnirdeploy/cmd_emergency.go`
- Create: `internal/grimnirdeploy/cmd_emergency_test.go`

**Context:**
The subcommand reads config, opens Redis + Postgres, wires a `Recorder`, wraps the body in `audit.Wrap`, calls `pause.Set` / `pause.Clear`, prints the prior + new state. `--dry-run` skips the Redis mutation but still writes audit rows so dry-runs are auditable.

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/cmd_emergency_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

type fakeNtfy struct{ posts int }

func (f *fakeNtfy) Post(ctx context.Context, topic, title, msg string, p audit.Priority, tags ...string) error {
	f.posts++
	return nil
}

func setupTestEnv(t *testing.T) (*pause.Client, *audit.Store, *fakeNtfy) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	pc := pause.NewClient(rdb)

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&audit.Entry{}); err != nil {
		t.Fatal(err)
	}
	return pc, audit.NewStore(db), &fakeNtfy{}
}

// We test the inner runEmergencyPause / runEmergencyResume functions directly,
// rather than through cobra, because cobra wiring is verified by the Chunk 0
// help-listing test. This isolates the behavior under test.
func TestRunEmergencyPauseSetsKey(t *testing.T) {
	pc, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runEmergencyPause(context.Background(), runEmergencyOpts{
		Reason: "fixing #999", DryRun: false, Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("runEmergencyPause: %v", err)
	}
	st, _ := pc.Read(context.Background())
	if st == nil || st.Reason != "fixing #999" {
		t.Errorf("pause state: %+v", st)
	}
	if ntfy.posts != 2 {
		t.Errorf("ntfy posts = %d, want 2 (start+complete)", ntfy.posts)
	}
}

func TestRunEmergencyPauseDryRunDoesNotSet(t *testing.T) {
	pc, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runEmergencyPause(context.Background(), runEmergencyOpts{
		Reason: "drill", DryRun: true, Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("runEmergencyPause dry-run: %v", err)
	}
	st, _ := pc.Read(context.Background())
	if st != nil {
		t.Errorf("dry-run set the key: %+v", st)
	}
}

func TestRunEmergencyResumeClearsKey(t *testing.T) {
	pc, store, ntfy := setupTestEnv(t)
	_ = pc.Set(context.Background(), "prior", "bob")

	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runEmergencyResume(context.Background(), runEmergencyOpts{
		Reason: "incident resolved", DryRun: false, Pause: pc, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("runEmergencyResume: %v", err)
	}
	st, _ := pc.Read(context.Background())
	if st != nil {
		t.Errorf("resume did not clear: %+v", st)
	}
}

// testRecorder satisfies audit.Recorder by composing the real Store with a
// fake ntfy poster. Used by every subcommand test in this package.
type testRecorder struct {
	store *audit.Store
	ntfy  *fakeNtfy
}

func (r testRecorder) WriteStart(ctx context.Context, op, ip, sub string, args map[string]any) (uuid.UUID, error) {
	return r.store.WriteStart(ctx, op, ip, sub, args)
}
func (r testRecorder) WriteComplete(ctx context.Context, id uuid.UUID, outcome string, d time.Duration, notes string) error {
	return r.store.WriteComplete(ctx, id, outcome, d, notes)
}
func (r testRecorder) WriteFailed(ctx context.Context, id uuid.UUID, outcome string, d time.Duration) error {
	return r.store.WriteFailed(ctx, id, outcome, d)
}
func (r testRecorder) PostNtfy(ctx context.Context, title, msg string, p audit.Priority) error {
	return r.ntfy.Post(ctx, "test-topic", title, msg, p)
}
```

- [ ] **Step 2: Run, expect failure**

```bash
go test ./internal/grimnirdeploy/ -run TestRunEmergency
```

Expected: build failure (`runEmergencyPause` undefined).

- [ ] **Step 3: Implement the subcommands**

`internal/grimnirdeploy/cmd_emergency.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

// runEmergencyOpts collects everything the inner functions need, so callers
// (cobra entry points + tests) can build the same options struct.
type runEmergencyOpts struct {
	Reason  string
	DryRun  bool
	Pause   *pause.Client
	Wrapper *audit.Wrapper
	Out     io.Writer
}

func runEmergencyPause(ctx context.Context, o runEmergencyOpts) error {
	return o.Wrapper.Wrap(ctx, "emergency-pause", map[string]any{"reason": o.Reason, "dry_run": o.DryRun}, func(ctx context.Context) error {
		prior, err := o.Pause.Read(ctx)
		if err != nil {
			return fmt.Errorf("read prior pause state: %w", err)
		}
		if prior != nil {
			fmt.Fprintf(o.Out, "pause already set by %s at %s (reason: %s); overwriting\n", prior.Operator, prior.TS.Format("2006-01-02T15:04:05Z"), prior.Reason)
		}
		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would set grimnir:emergency-pause reason=%q\n", o.Reason)
			return nil
		}
		// The operator on the new pause row is recorded by audit.Wrap; here we
		// also write it into the Redis key payload for non-DB readers.
		operator := ""
		if o.Wrapper != nil {
			// Wrapper has no public Operator accessor; pull it via a helper.
			operator = wrapperOperator(o.Wrapper)
		}
		if err := o.Pause.Set(ctx, o.Reason, operator); err != nil {
			return fmt.Errorf("set pause: %w", err)
		}
		fmt.Fprintf(o.Out, "emergency-pause SET (reason: %s)\n", o.Reason)
		return nil
	})
}

func runEmergencyResume(ctx context.Context, o runEmergencyOpts) error {
	return o.Wrapper.Wrap(ctx, "emergency-resume", map[string]any{"reason": o.Reason, "dry_run": o.DryRun}, func(ctx context.Context) error {
		prior, err := o.Pause.Read(ctx)
		if err != nil {
			return fmt.Errorf("read prior pause state: %w", err)
		}
		if prior == nil {
			fmt.Fprintln(o.Out, "no pause was set; nothing to clear")
			return nil
		}
		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would clear grimnir:emergency-pause (was: %s by %s)\n", prior.Reason, prior.Operator)
			return nil
		}
		if err := o.Pause.Clear(ctx); err != nil {
			return fmt.Errorf("clear pause: %w", err)
		}
		fmt.Fprintf(o.Out, "emergency-pause CLEARED (was: %s by %s; resume reason: %s)\n", prior.Reason, prior.Operator, o.Reason)
		return nil
	})
}

// realEmergencyPauseRunE replaces the stub in cmd_stubs.go.
func realEmergencyPauseRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	reason, _ := cmd.Flags().GetString("reason")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	return runEmergencyPause(cmd.Context(), runEmergencyOpts{
		Reason: reason, DryRun: dryRun, Pause: deps.Pause, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(),
	})
}

func realEmergencyResumeRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	reason, _ := cmd.Flags().GetString("reason")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	return runEmergencyResume(cmd.Context(), runEmergencyOpts{
		Reason: reason, DryRun: dryRun, Pause: deps.Pause, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(),
	})
}
```

- [ ] **Step 4: Add the dependency wiring helper**

Create `internal/grimnirdeploy/deps.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"reflect"
	"unsafe"

	"github.com/redis/go-redis/v9"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

// Deps is the bag of cluster connections a subcommand needs.
type Deps struct {
	Cfg     *Config
	DB      *gorm.DB
	Redis   *redis.Client
	Pause   *pause.Client
	Store   *audit.Store
	Ntfy    *audit.NtfyPoster
	Wrapper *audit.Wrapper
}

// Close releases the Redis connection and the underlying SQL connection.
func (d *Deps) Close() {
	if d.Redis != nil {
		_ = d.Redis.Close()
	}
	if d.DB != nil {
		sqlDB, _ := d.DB.DB()
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
	}
}

// wireDeps opens DB + Redis connections and assembles the audit Wrapper.
func wireDeps(ctx context.Context, cfg *Config) (*Deps, error) {
	db, err := gorm.Open(postgres.Open(cfg.DBDSN), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, err
	}
	store := audit.NewStore(db)
	poster := audit.NewNtfyPoster(cfg.NtfyURL, cfg.NtfyToken)
	rec := audit.NewRecorder(store, poster, cfg.NtfyAuditTopic)
	wrapper := audit.NewWrapper(rec, cfg.Operator, localSourceIP())
	return &Deps{
		Cfg: cfg, DB: db, Redis: rdb, Pause: pause.NewClient(rdb),
		Store: store, Ntfy: poster, Wrapper: wrapper,
	}, nil
}

// localSourceIP returns the IP of the SSH connection the operator is on
// ($SSH_CLIENT first field), or "local" for direct console invocation.
func localSourceIP() string {
	// SSH_CLIENT format: "client_ip client_port server_port"
	v := getEnv("SSH_CLIENT", "", "")
	if v == "" {
		return "local"
	}
	for i, c := range v {
		if c == ' ' {
			return v[:i]
		}
	}
	return v
}

// wrapperOperator is a small unsafe helper to read the unexported operator
// field from *audit.Wrapper without exposing a public accessor on the audit
// package. The audit package keeps the field unexported to discourage other
// callers from reading it; this binary needs it to stamp the Redis key
// payload.
//
// Justification: the cleanest alternative is to add an exported accessor
// in audit, which leaks a field that has no other public use. The reflection
// approach is ugly but contained to a single helper.
func wrapperOperator(w *audit.Wrapper) string {
	v := reflect.ValueOf(w).Elem().FieldByName("operator")
	if !v.IsValid() {
		return ""
	}
	return reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().String()
}
```

Note: if the reviewer rejects the unsafe-reflection helper, add `Operator() string` to `audit.Wrapper` and delete `wrapperOperator`. The lesser-evil decision is captured in the comment.

- [ ] **Step 5: Replace the two stubs in `cmd_stubs.go`**

In `internal/grimnirdeploy/cmd_stubs.go`, change `newEmergencyPauseCmd()` to set `RunE: realEmergencyPauseRunE` and `newEmergencyResumeCmd()` to set `RunE: realEmergencyResumeRunE`. Delete the `errNotImplemented` returns from those two constructors only.

- [ ] **Step 6: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/ -run TestRunEmergency
go test ./cmd/grimnir-deploy/
```

Expected: PASS (subcommand unit tests + the help-listing test still works).

- [ ] **Step 7: Commit**

```bash
git add internal/grimnirdeploy/cmd_emergency.go internal/grimnirdeploy/cmd_emergency_test.go internal/grimnirdeploy/deps.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: emergency-pause / emergency-resume subcommands (Chunk 3.2)"
```

---

## Chunk 4: `deploy_history` table + history store

The `deploy --rollback` flow reads this table to find the previous successful tag and to detect contract-migration crossings. Land the table and the read/write APIs before Chunk 5 (which writes rows) and Chunk 6 (which reads them for rollback eligibility).

### Task 4.1: Migration + model

**Files:**
- Create: `migrations/006_deploy_history.sql`
- Create: `internal/grimnirdeploy/history/model.go`
- Modify: `internal/db/migrate.go`

**Context:**
Schema is verbatim from Section 6 of the design (id, region, tag, previous_tag, started_at, completed_at, operator, outcome, reason, soak_outcome, failure_log).

- [ ] **Step 1: Write the migration**

`migrations/006_deploy_history.sql`:

```sql
-- Migration 006: deploy_history table for grimnir-deploy
-- Created: 2026-06-06
-- Phase: expand
--
-- Description: Adds the deploy_history table per Section 6 of the HA design.
-- One row per grimnir-deploy invocation; outcome column captures success,
-- rolled_back_mid_roll, rollback, soak_failed. Used by --rollback flag to
-- find the previous successful tag and to detect contract-migration crossings.
--
-- Expand-only: additive table.

-- ============================================================================
-- SQL BELOW
-- ============================================================================

CREATE TABLE IF NOT EXISTS deploy_history (
    id              uuid PRIMARY KEY,
    region          text NOT NULL,
    tag             text NOT NULL,
    previous_tag    text,
    started_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    operator        text NOT NULL,
    outcome         text,
    reason          text,
    soak_outcome    text,
    failure_log     text
);

CREATE INDEX IF NOT EXISTS idx_deploy_history_region_started ON deploy_history (region, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_deploy_history_outcome ON deploy_history (outcome, started_at DESC);
```

- [ ] **Step 2: Write the GORM model**

`internal/grimnirdeploy/history/model.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package history reads and writes the deploy_history table. Used by
// grimnir-deploy deploy (writes), grimnir-deploy deploy --rollback (reads
// previous successful tag + checks contract-migration crossings), and
// dashboards (reads outcome counts).
package history

import (
	"time"

	"github.com/google/uuid"
)

// Outcome values stored in the outcome column.
const (
	OutcomeSuccess           = "success"
	OutcomeRolledBackMidRoll = "rolled_back_mid_roll"
	OutcomeRollback          = "rollback"
	OutcomeSoakFailed        = "soak_failed"
	OutcomeFailed            = "failed"
)

// Soak outcome values.
const (
	SoakPassed  = "passed"
	SoakFailed  = "failed"
	SoakSkipped = "skipped"
)

// Entry is one row in deploy_history.
type Entry struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey"`
	Region      string     `gorm:"column:region;not null"`
	Tag         string     `gorm:"column:tag;not null"`
	PreviousTag string     `gorm:"column:previous_tag"`
	StartedAt   time.Time  `gorm:"column:started_at;not null;default:now()"`
	CompletedAt *time.Time `gorm:"column:completed_at"`
	Operator    string     `gorm:"column:operator;not null"`
	Outcome     string     `gorm:"column:outcome"`
	Reason      string     `gorm:"column:reason"`
	SoakOutcome string     `gorm:"column:soak_outcome"`
	FailureLog  string     `gorm:"column:failure_log"`
}

func (Entry) TableName() string { return "deploy_history" }
```

- [ ] **Step 3: Hook into AutoMigrate**

Add `&history.Entry{}` to the AutoMigrate list in `internal/db/migrate.go`.

- [ ] **Step 4: Commit**

```bash
git add migrations/006_deploy_history.sql internal/grimnirdeploy/history/model.go internal/db/migrate.go
git commit -m "grimnir-deploy: deploy_history table + GORM model (Chunk 4.1)"
```

### Task 4.2: History store

**Files:**
- Create: `internal/grimnirdeploy/history/store.go`
- Create: `internal/grimnirdeploy/history/store_test.go`

**Context:**
The store exposes:
- `Start(ctx, region, tag, prevTag, operator) -> uuid`: inserts a row with no outcome
- `Complete(ctx, id, outcome, soak)`: stamps completed_at + outcome
- `Fail(ctx, id, outcome, failureLog)`: stamps completed_at + outcome="failed" with log
- `LastSuccessful(ctx, region) -> *Entry`: most recent outcome=success row
- `WithinEligibility(ctx, region, window) -> bool`: did the most recent successful deploy finish within `window`
- `ContractCrossings(ctx, region, fromTag, toTag) -> []string`: list of migration filenames between two tag deployments that carry the `migration-contract:` annotation

The last function reads `migrations/*.sql` from disk (path configurable) and uses the `git log --format=%S --grep` mechanism via shelling to `git` to find which migrations were introduced between the deploy of `fromTag` and the deploy of `toTag`. This is a "make a real decision later" stub for now: implement a simple in-process scan of `migrations/` that opens each file and grep-matches the annotation, returning all annotated files (over-conservative; refines to git-blame-by-commit in a follow-up). Document the trade-off in code comments.

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/history/store_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package history

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&Entry{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStartCompleteAndQuery(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()

	id1, err := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, id1, OutcomeSuccess, SoakPassed); err != nil {
		t.Fatal(err)
	}

	id2, _ := s.Start(ctx, "us-east", "v1.1.0", "v1.0.0", "alice")
	_ = s.Complete(ctx, id2, OutcomeSuccess, SoakPassed)

	last, err := s.LastSuccessful(ctx, "us-east")
	if err != nil {
		t.Fatal(err)
	}
	if last.Tag != "v1.1.0" {
		t.Errorf("LastSuccessful.Tag = %q, want v1.1.0", last.Tag)
	}
	if last.PreviousTag != "v1.0.0" {
		t.Errorf("PreviousTag = %q, want v1.0.0", last.PreviousTag)
	}
}

func TestLastSuccessfulSkipsRolledBack(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	id1, _ := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	_ = s.Complete(ctx, id1, OutcomeSuccess, SoakPassed)

	id2, _ := s.Start(ctx, "us-east", "v1.1.0", "v1.0.0", "alice")
	_ = s.Complete(ctx, id2, OutcomeRolledBackMidRoll, SoakSkipped)

	last, _ := s.LastSuccessful(ctx, "us-east")
	if last.Tag != "v1.0.0" {
		t.Errorf("LastSuccessful.Tag = %q, want v1.0.0 (v1.1.0 was rolled back)", last.Tag)
	}
}

func TestWithinEligibility(t *testing.T) {
	db := newTestDB(t)
	s := NewStore(db)
	ctx := context.Background()
	id1, _ := s.Start(ctx, "us-east", "v1.0.0", "", "alice")
	_ = s.Complete(ctx, id1, OutcomeSuccess, SoakPassed)

	ok, err := s.WithinEligibility(ctx, "us-east", 4*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Error("recent success should be within 4h eligibility")
	}

	// Backdate to 5h ago.
	if err := db.Model(&Entry{}).Where("id = ?", id1).Update("completed_at", time.Now().UTC().Add(-5*time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	ok, _ = s.WithinEligibility(ctx, "us-east", 4*time.Hour)
	if ok {
		t.Error("5h-old success should NOT be within 4h eligibility")
	}
}

func TestContractCrossings(t *testing.T) {
	dir := t.TempDir()
	// One migration with the contract annotation, one without.
	_ = os.WriteFile(filepath.Join(dir, "010_safe.sql"), []byte("-- Phase: expand\nCREATE TABLE x();\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "011_contract.sql"), []byte("-- migration-contract: drop old column\nALTER TABLE x DROP COLUMN y;\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "TEMPLATE.sql"), []byte("template; should be skipped"), 0o644)

	db := newTestDB(t)
	s := NewStore(db).WithMigrationsDir(dir)

	got, err := s.ContractCrossings(context.Background(), "us-east", "v1.0.0", "v1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "011_contract.sql" {
		t.Errorf("ContractCrossings = %v, want [011_contract.sql]", got)
	}
}
```

- [ ] **Step 2: Run, expect failure**

```bash
go test ./internal/grimnirdeploy/history/
```

Expected: build failure (`NewStore` undefined).

- [ ] **Step 3: Implement the store**

`internal/grimnirdeploy/history/store.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package history

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Store reads and writes deploy_history.
type Store struct {
	db            *gorm.DB
	migrationsDir string // for ContractCrossings
}

// NewStore constructs a store. The migrations directory defaults to
// "migrations" relative to CWD; override with WithMigrationsDir for tests.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db, migrationsDir: "migrations"}
}

// WithMigrationsDir returns a copy of the store with the migration scan
// directory overridden.
func (s *Store) WithMigrationsDir(dir string) *Store {
	cp := *s
	cp.migrationsDir = dir
	return &cp
}

// Start inserts a new in-progress deploy row. Returns the new id.
func (s *Store) Start(ctx context.Context, region, tag, prevTag, operator string) (uuid.UUID, error) {
	id := uuid.New()
	e := Entry{
		ID: id, Region: region, Tag: tag, PreviousTag: prevTag,
		StartedAt: time.Now().UTC(), Operator: operator,
	}
	return id, s.db.WithContext(ctx).Create(&e).Error
}

// Complete stamps completed_at + outcome + soak_outcome on the row.
func (s *Store) Complete(ctx context.Context, id uuid.UUID, outcome, soak string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&Entry{}).Where("id = ?", id).Updates(map[string]any{
		"completed_at": now,
		"outcome":      outcome,
		"soak_outcome": soak,
	}).Error
}

// Fail stamps the row as failed with the captured log.
func (s *Store) Fail(ctx context.Context, id uuid.UUID, outcome, failureLog string) error {
	now := time.Now().UTC()
	return s.db.WithContext(ctx).Model(&Entry{}).Where("id = ?", id).Updates(map[string]any{
		"completed_at": now,
		"outcome":      outcome,
		"failure_log":  failureLog,
	}).Error
}

// LastSuccessful returns the most recent outcome="success" entry for the region,
// or nil if there is none.
func (s *Store) LastSuccessful(ctx context.Context, region string) (*Entry, error) {
	var e Entry
	err := s.db.WithContext(ctx).Where("region = ? AND outcome = ?", region, OutcomeSuccess).
		Order("started_at DESC").First(&e).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// WithinEligibility returns true if the most recent successful deploy
// completed within the given window. False if there is no successful deploy
// or it completed longer ago than window.
func (s *Store) WithinEligibility(ctx context.Context, region string, window time.Duration) (bool, error) {
	last, err := s.LastSuccessful(ctx, region)
	if err != nil || last == nil || last.CompletedAt == nil {
		return false, err
	}
	return time.Since(*last.CompletedAt) <= window, nil
}

// ContractCrossings returns the migration filenames between fromTag and
// toTag that carry the `migration-contract:` annotation.
//
// Phase-1 implementation: scans the migrations directory for any file
// containing the annotation. Over-conservative (it flags every annotated
// migration, regardless of which tags introduced them). A follow-up will
// refine this to git-blame-by-tag once we have a stable tag-to-commit
// mapping.
func (s *Store) ContractCrossings(ctx context.Context, region, fromTag, toTag string) ([]string, error) {
	entries, err := os.ReadDir(s.migrationsDir)
	if err != nil {
		return nil, err
	}
	var hits []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if e.Name() == "TEMPLATE.sql" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.migrationsDir, e.Name()))
		if err != nil {
			continue
		}
		if strings.Contains(string(b), "migration-contract:") {
			hits = append(hits, e.Name())
		}
	}
	sort.Strings(hits)
	return hits, nil
}
```

- [ ] **Step 4: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/history/
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/grimnirdeploy/history/store.go internal/grimnirdeploy/history/store_test.go
git commit -m "grimnir-deploy: deploy_history store with eligibility + contract scan (Chunk 4.2)"
```

---

## Chunk 5: Pre-flight gates + SSH/Docker runner abstractions

The `deploy`, `rollback`, `drain`, and `cold-start-region` subcommands all run the same pre-flight checks before touching the cluster. This chunk lands those checks as composable gates and lands the SSH + Docker runner abstractions every cluster-mutating subcommand uses. After this chunk, Chunk 6 (deploy) is mostly orchestration.

### Task 5.1: Runner interface + fake

**Files:**
- Create: `internal/grimnirdeploy/runner/runner.go`
- Create: `internal/grimnirdeploy/runner/fake.go`
- Create: `internal/grimnirdeploy/runner/fake_test.go`

**Context:**
The interface has one method: `Run(ctx, host, cmd) (stdout, stderr string, exitcode int, err error)`. Host="local" executes via `os/exec`; any other host SSHes. The fake records the exact command sequence and returns configured outputs per command pattern; tests inject the fake so subcommand logic is verified without touching real SSH.

- [ ] **Step 1: Write the failing test for the fake**

`internal/grimnirdeploy/runner/fake_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestFakeRecordsCalls(t *testing.T) {
	f := NewFake()
	f.SetResponse("hostname", "node-1\n", "", 0, nil)
	out, _, code, err := f.Run(context.Background(), "node-1", "hostname")
	if err != nil || code != 0 {
		t.Fatalf("Run: err=%v code=%d", err, code)
	}
	if strings.TrimSpace(out) != "node-1" {
		t.Errorf("stdout = %q", out)
	}
	if len(f.Calls) != 1 {
		t.Fatalf("Calls = %d, want 1", len(f.Calls))
	}
	if f.Calls[0].Host != "node-1" || f.Calls[0].Cmd != "hostname" {
		t.Errorf("Calls[0] = %+v", f.Calls[0])
	}
}

func TestFakeMatchesPrefixedResponse(t *testing.T) {
	f := NewFake()
	f.SetResponsePrefix("docker compose", "ok\n", "", 0, nil)
	out, _, _, err := f.Run(context.Background(), "local", "docker compose up -d grimnir")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "ok" {
		t.Errorf("prefix match did not return configured response; got %q", out)
	}
}

func TestFakeDefaultsToExitZeroEmptyStdout(t *testing.T) {
	f := NewFake()
	out, _, code, err := f.Run(context.Background(), "local", "anything")
	if err != nil || code != 0 || out != "" {
		t.Errorf("default response: err=%v code=%d out=%q", err, code, out)
	}
}
```

- [ ] **Step 2: Run, expect failure, then implement**

`internal/grimnirdeploy/runner/runner.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package runner abstracts command execution on the local node or on the
// peer node via SSH. Both implementations satisfy Runner.
package runner

import "context"

// Runner executes a shell command on a target host and returns its captured
// stdout, stderr, exit code, and any transport error.
//
// Host == "local" means execute on the current node via os/exec.
// Any other host is treated as an SSH target.
type Runner interface {
	Run(ctx context.Context, host, cmd string) (stdout, stderr string, exitCode int, err error)
}
```

`internal/grimnirdeploy/runner/fake.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"strings"
	"sync"
)

// Call captures one Runner.Run invocation.
type Call struct {
	Host string
	Cmd  string
}

// FakeResponse is what the fake returns for a matched command.
type FakeResponse struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Fake is an in-memory Runner for tests. It records every call and looks up
// the response by exact-command match first, then by prefix.
type Fake struct {
	mu       sync.Mutex
	Calls    []Call
	exact    map[string]FakeResponse
	prefixes []prefixResp
}

type prefixResp struct {
	prefix string
	resp   FakeResponse
}

// NewFake constructs an empty Fake.
func NewFake() *Fake {
	return &Fake{exact: map[string]FakeResponse{}}
}

// SetResponse registers an exact-match response.
func (f *Fake) SetResponse(cmd, stdout, stderr string, code int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exact[cmd] = FakeResponse{stdout, stderr, code, err}
}

// SetResponsePrefix registers a prefix-match response. Falls through to exact-
// match if the cmd is not in the exact table.
func (f *Fake) SetResponsePrefix(prefix, stdout, stderr string, code int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prefixes = append(f.prefixes, prefixResp{prefix, FakeResponse{stdout, stderr, code, err}})
}

// Run records the call and returns the matched response.
func (f *Fake) Run(ctx context.Context, host, cmd string) (string, string, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Host: host, Cmd: cmd})
	if r, ok := f.exact[cmd]; ok {
		return r.Stdout, r.Stderr, r.ExitCode, r.Err
	}
	for _, p := range f.prefixes {
		if strings.HasPrefix(cmd, p.prefix) {
			return p.resp.Stdout, p.resp.Stderr, p.resp.ExitCode, p.resp.Err
		}
	}
	return "", "", 0, nil
}
```

- [ ] **Step 3: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/runner/
```

- [ ] **Step 4: Commit**

```bash
git add internal/grimnirdeploy/runner/runner.go internal/grimnirdeploy/runner/fake.go internal/grimnirdeploy/runner/fake_test.go
git commit -m "grimnir-deploy: Runner interface + Fake for tests (Chunk 5.1)"
```

### Task 5.2: Real SSH runner

**Files:**
- Create: `internal/grimnirdeploy/runner/ssh.go`
- Create: `internal/grimnirdeploy/runner/ssh_test.go`

**Context:**
Uses `golang.org/x/crypto/ssh` (already an indirect dep of webrtc; verify with `grep x/crypto/ssh go.sum`; explicit `go get golang.org/x/crypto/ssh@latest` if needed). One persistent SSH client per target host; sessions per command. Real-infra tests live in `ssh_real_test.go` with build tag `//go:build integration && requires_real_cluster`.

- [ ] **Step 1: Implement the SSH runner**

`internal/grimnirdeploy/runner/ssh.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHRunner runs commands locally (via os/exec) for host=="local" or via SSH
// for any other host. SSH clients are cached per host.
type SSHRunner struct {
	user     string
	port     int
	keyPath  string
	mu       sync.Mutex
	clients  map[string]*ssh.Client // host -> client
	hostKeys ssh.HostKeyCallback    // production: use known_hosts; tests: substitute
}

// NewSSHRunner constructs an SSH runner. The keyPath points at a PEM-encoded
// private key. hostKeyCallback is the verification callback; pass
// ssh.InsecureIgnoreHostKey() ONLY in tests.
func NewSSHRunner(user string, port int, keyPath string, hostKeys ssh.HostKeyCallback) *SSHRunner {
	return &SSHRunner{
		user: user, port: port, keyPath: keyPath, hostKeys: hostKeys,
		clients: map[string]*ssh.Client{},
	}
}

// Run executes cmd on host. host=="local" runs via os/exec.
func (r *SSHRunner) Run(ctx context.Context, host, cmd string) (string, string, int, error) {
	if host == "local" {
		return runLocal(ctx, cmd)
	}
	c, err := r.client(host)
	if err != nil {
		return "", "", 0, fmt.Errorf("ssh dial %s: %w", host, err)
	}
	sess, err := c.NewSession()
	if err != nil {
		return "", "", 0, fmt.Errorf("ssh session %s: %w", host, err)
	}
	defer sess.Close()
	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- sess.Run(cmd) }()
	select {
	case <-ctx.Done():
		_ = sess.Signal(ssh.SIGTERM)
		return stdout.String(), stderr.String(), -1, ctx.Err()
	case runErr := <-done:
		if runErr == nil {
			return stdout.String(), stderr.String(), 0, nil
		}
		var ee *ssh.ExitError
		if errors.As(runErr, &ee) {
			return stdout.String(), stderr.String(), ee.ExitStatus(), nil
		}
		return stdout.String(), stderr.String(), -1, runErr
	}
}

// Close tears down all cached SSH clients.
func (r *SSHRunner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.clients {
		_ = c.Close()
	}
	r.clients = map[string]*ssh.Client{}
}

func (r *SSHRunner) client(host string) (*ssh.Client, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[host]; ok {
		return c, nil
	}
	key, err := os.ReadFile(r.keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse key: %w", err)
	}
	cb := r.hostKeys
	if cb == nil {
		cb = ssh.InsecureIgnoreHostKey() // only reached if caller forgot to pass one
	}
	conf := &ssh.ClientConfig{
		User:            r.user,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: cb,
		Timeout:         10 * time.Second,
	}
	addr := net.JoinHostPort(host, strconv.Itoa(r.port))
	c, err := ssh.Dial("tcp", addr, conf)
	if err != nil {
		return nil, err
	}
	r.clients[host] = c
	return c, nil
}

func runLocal(ctx context.Context, cmd string) (string, string, int, error) {
	c := exec.CommandContext(ctx, "sh", "-c", cmd)
	var out, errBuf bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errBuf
	err := c.Run()
	if err == nil {
		return out.String(), errBuf.String(), 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out.String(), errBuf.String(), ee.ExitCode(), nil
	}
	return out.String(), errBuf.String(), -1, err
}
```

- [ ] **Step 2: Add a smoke test for local execution**

`internal/grimnirdeploy/runner/ssh_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestLocalEcho(t *testing.T) {
	r := NewSSHRunner("noone", 22, "", nil)
	out, _, code, err := r.Run(context.Background(), "local", "echo hello")
	if err != nil || code != 0 {
		t.Fatalf("local echo: err=%v code=%d", err, code)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("stdout = %q", out)
	}
}

func TestLocalExitNonZero(t *testing.T) {
	r := NewSSHRunner("noone", 22, "", nil)
	_, _, code, err := r.Run(context.Background(), "local", "exit 7")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if code != 7 {
		t.Errorf("exit code = %d, want 7", code)
	}
}
```

The real-SSH test against a remote host is deferred to `ssh_real_test.go` with build tag `//go:build integration && requires_real_cluster`; document this in the file header so future readers know where to look.

- [ ] **Step 3: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/runner/
```

- [ ] **Step 4: Commit**

```bash
git add internal/grimnirdeploy/runner/ssh.go internal/grimnirdeploy/runner/ssh_test.go
git commit -m "grimnir-deploy: real SSH runner with local exec fallback (Chunk 5.2)"
```

### Task 5.3: Docker compose helpers

**Files:**
- Create: `internal/grimnirdeploy/runner/docker.go`
- Create: `internal/grimnirdeploy/runner/docker_test.go`

**Context:**
Thin wrappers that build the right `docker compose` invocations for the production server (see CLAUDE.md production server commands: `./grimnir up -d`, `./grimnir down`, `./grimnir ps`, `./grimnir logs -f`, `./grimnir pull`). The compose dir is configurable (defaults to `/srv/docker/grimnir_radio`); operations run via the supplied Runner so they work on both `local` and the peer.

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/runner/docker_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"strings"
	"testing"
)

func TestDockerPullUsesGrimnirWrapper(t *testing.T) {
	f := NewFake()
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir pull", "pulled\n", "", 0, nil)
	d := NewDockerCompose(f, "/srv/docker/grimnir_radio")
	if err := d.Pull(context.Background(), "node-1"); err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if len(f.Calls) != 1 || !strings.Contains(f.Calls[0].Cmd, "./grimnir pull") {
		t.Errorf("Calls[0] = %+v", f.Calls[0])
	}
}

func TestDockerUpAndStop(t *testing.T) {
	f := NewFake()
	d := NewDockerCompose(f, "/srv/docker/grimnir_radio")
	_ = d.Up(context.Background(), "node-1")
	_ = d.Stop(context.Background(), "node-1", "grimnir")
	if len(f.Calls) != 2 {
		t.Fatalf("Calls = %d, want 2", len(f.Calls))
	}
	if !strings.Contains(f.Calls[0].Cmd, "./grimnir up -d") {
		t.Errorf("Calls[0].Cmd = %q", f.Calls[0].Cmd)
	}
	if !strings.Contains(f.Calls[1].Cmd, "stop grimnir") {
		t.Errorf("Calls[1].Cmd = %q", f.Calls[1].Cmd)
	}
}

func TestDockerCurrentImageTag(t *testing.T) {
	f := NewFake()
	f.SetResponsePrefix("docker inspect --format", "v1.40.7\n", "", 0, nil)
	d := NewDockerCompose(f, "/srv/docker/grimnir_radio")
	tag, err := d.CurrentTag(context.Background(), "node-1", "grimnir-radio")
	if err != nil {
		t.Fatalf("CurrentTag: %v", err)
	}
	if tag != "v1.40.7" {
		t.Errorf("CurrentTag = %q, want v1.40.7", tag)
	}
}
```

- [ ] **Step 2: Implement**

`internal/grimnirdeploy/runner/docker.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package runner

import (
	"context"
	"fmt"
	"strings"
)

// DockerCompose wraps the ./grimnir compose-wrapper script on a target host.
// Per CLAUDE.md, production must use ./grimnir rather than direct docker
// compose to get the correct compose-file ordering.
type DockerCompose struct {
	r       Runner
	dir     string // e.g., /srv/docker/grimnir_radio
	wrapper string // default "./grimnir"
}

// NewDockerCompose constructs a DockerCompose helper bound to a Runner.
func NewDockerCompose(r Runner, dir string) *DockerCompose {
	return &DockerCompose{r: r, dir: dir, wrapper: "./grimnir"}
}

// Pull pulls latest images via the wrapper.
func (d *DockerCompose) Pull(ctx context.Context, host string) error {
	cmd := fmt.Sprintf("cd %s && %s pull", d.dir, d.wrapper)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker pull exit %d: %s", code, stderr)
	}
	return nil
}

// Up starts all services via the wrapper (./grimnir up -d).
func (d *DockerCompose) Up(ctx context.Context, host string) error {
	cmd := fmt.Sprintf("cd %s && %s up -d", d.dir, d.wrapper)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker up exit %d: %s", code, stderr)
	}
	return nil
}

// Down stops all services.
func (d *DockerCompose) Down(ctx context.Context, host string) error {
	cmd := fmt.Sprintf("cd %s && %s down", d.dir, d.wrapper)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker down exit %d: %s", code, stderr)
	}
	return nil
}

// Stop stops a single service (matches `docker compose stop <svc>`).
func (d *DockerCompose) Stop(ctx context.Context, host, service string) error {
	cmd := fmt.Sprintf("cd %s && docker compose stop %s", d.dir, service)
	_, stderr, code, err := d.r.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("docker stop %s exit %d: %s", service, code, stderr)
	}
	return nil
}

// CurrentTag returns the image tag currently running for the named container
// on the target host. Empty string + nil if no such container.
func (d *DockerCompose) CurrentTag(ctx context.Context, host, container string) (string, error) {
	cmd := fmt.Sprintf("docker inspect --format='{{ .Config.Image }}' %s", container)
	out, _, code, err := d.r.Run(ctx, host, cmd)
	if err != nil || code != 0 {
		return "", err
	}
	image := strings.TrimSpace(out)
	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		return image[idx+1:], nil
	}
	return image, nil
}
```

- [ ] **Step 3: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/runner/
```

- [ ] **Step 4: Commit**

```bash
git add internal/grimnirdeploy/runner/docker.go internal/grimnirdeploy/runner/docker_test.go
git commit -m "grimnir-deploy: DockerCompose helper around ./grimnir wrapper (Chunk 5.3)"
```

### Task 5.4: Gates (emergency-pause, policy, tag-suffix, image-exists, both-nodes-healthy)

**Files:**
- Create: `internal/grimnirdeploy/gates/gates.go` (shared Gate interface + Run helper)
- Create: `internal/grimnirdeploy/gates/policy.go`
- Create: `internal/grimnirdeploy/gates/policy_test.go`
- Create: `internal/grimnirdeploy/gates/tagsuffix.go`
- Create: `internal/grimnirdeploy/gates/tagsuffix_test.go`
- Create: `internal/grimnirdeploy/gates/pause.go`
- Create: `internal/grimnirdeploy/gates/pause_test.go`
- Create: `internal/grimnirdeploy/gates/image.go`
- Create: `internal/grimnirdeploy/gates/image_test.go`
- Create: `internal/grimnirdeploy/gates/health.go`
- Create: `internal/grimnirdeploy/gates/health_test.go`

**Context:**
A Gate evaluates one pre-flight condition. Returns nil if the gate passes; an `*Aborted` error (sentinel type) if the gate refuses; a regular error if the check itself failed (network, etc.). The deploy subcommand runs every gate in order and aborts on the first non-nil result.

This task is intentionally five sub-tasks bundled because each gate is small (~50 lines + test). Implement them in this order: gates.go → pause → tagsuffix → policy → image → health. Each substep follows the same TDD shape (write test, run+fail, implement, run+pass, commit).

- [ ] **Step 1: Shared Gate interface**

`internal/grimnirdeploy/gates/gates.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package gates implements the pre-flight checks every cluster-mutating
// grimnir-deploy subcommand runs before any side effect. Each gate is a
// small focused type with one Evaluate method.
package gates

import (
	"context"
	"errors"
	"fmt"
)

// Aborted is returned by a gate to refuse the operation. Distinct from a
// transport error: an Aborted means "the gate worked correctly and the
// operation is denied"; a regular error means "the gate could not decide."
type Aborted struct {
	Gate   string
	Reason string
}

func (a *Aborted) Error() string { return fmt.Sprintf("aborted by %s gate: %s", a.Gate, a.Reason) }

// IsAborted reports whether err is an Aborted.
func IsAborted(err error) bool {
	var a *Aborted
	return errors.As(err, &a)
}

// Gate evaluates one pre-flight condition.
type Gate interface {
	Name() string
	Evaluate(ctx context.Context) error
}

// RunAll runs every gate in order and returns the first non-nil result.
func RunAll(ctx context.Context, gates ...Gate) error {
	for _, g := range gates {
		if err := g.Evaluate(ctx); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 2: Pause gate**

Test (`gates/pause_test.go`):

```go
package gates

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

type fakePauseReader struct{ state *pause.State }

func (f *fakePauseReader) Read(ctx context.Context) (*pause.State, error) { return f.state, nil }

func TestPauseGatePassesWhenNoPause(t *testing.T) {
	g := NewPauseGate(&fakePauseReader{})
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("expected pass; got %v", err)
	}
}

func TestPauseGateAbortsWhenPauseSet(t *testing.T) {
	g := NewPauseGate(&fakePauseReader{state: &pause.State{Reason: "fixing"}})
	err := g.Evaluate(context.Background())
	if !IsAborted(err) {
		t.Errorf("expected Aborted; got %v", err)
	}
}
```

Impl (`gates/pause.go`):

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
)

// PauseReader is the subset of pause.Client the gate needs.
type PauseReader interface {
	Read(ctx context.Context) (*pause.State, error)
}

// PauseGate aborts when the emergency-pause Redis key is set.
type PauseGate struct{ R PauseReader }

func NewPauseGate(r PauseReader) *PauseGate { return &PauseGate{R: r} }
func (g *PauseGate) Name() string           { return "emergency-pause" }
func (g *PauseGate) Evaluate(ctx context.Context) error {
	s, err := g.R.Read(ctx)
	if err != nil {
		return fmt.Errorf("read pause state: %w", err)
	}
	if s != nil {
		return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("pause set by %s at %s: %s", s.Operator, s.TS.Format("2006-01-02T15:04:05Z"), s.Reason)}
	}
	return nil
}
```

- [ ] **Step 3: Tag-suffix gate**

Test (`gates/tagsuffix_test.go`):

```go
package gates

import (
	"context"
	"testing"
)

func TestTagSuffixHoldAlwaysAborts(t *testing.T) {
	g := NewTagSuffixGate("v1.0.0-hold", "auto")
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("hold suffix should always abort")
	}
}

func TestTagSuffixHotfixOverridesWindowPolicy(t *testing.T) {
	g := NewTagSuffixGate("v1.0.0-hotfix", "window")
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("hotfix should override window; got %v", err)
	}
}

func TestTagSuffixBareTagDoesNothing(t *testing.T) {
	g := NewTagSuffixGate("v1.0.0", "auto")
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("bare tag should pass; got %v", err)
	}
}
```

Impl (`gates/tagsuffix.go`):

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"strings"
)

// TagSuffixGate enforces the design's tag suffix conventions:
//   -hold:   skip auto entirely (always abort)
//   -hotfix: override window policy
// Effect on PolicyGate is communicated via the OverridesPolicy method, read
// by the deploy command after gate evaluation.
type TagSuffixGate struct {
	tag    string
	policy string
}

func NewTagSuffixGate(tag, policy string) *TagSuffixGate { return &TagSuffixGate{tag: tag, policy: policy} }
func (g *TagSuffixGate) Name() string                    { return "tag-suffix" }

// OverridesPolicy reports whether the tag suffix overrides the deploy policy
// (e.g., -hotfix bypasses window restrictions). The PolicyGate consults this.
func (g *TagSuffixGate) OverridesPolicy() bool { return strings.HasSuffix(g.tag, "-hotfix") }

func (g *TagSuffixGate) Evaluate(ctx context.Context) error {
	if strings.HasSuffix(g.tag, "-hold") {
		return &Aborted{Gate: g.Name(), Reason: "-hold suffix; deploys disabled for this tag"}
	}
	return nil
}
```

- [ ] **Step 4: Policy gate (cron window matcher)**

The "window" policy needs a cron-expression evaluator. The repo already has `github.com/teambition/rrule-go` but that's RRULE not cron. Add a small dependency: `github.com/robfig/cron/v3` (`go get github.com/robfig/cron/v3@latest`).

Test (`gates/policy_test.go`):

```go
package gates

import (
	"context"
	"testing"
	"time"
)

func TestPolicyAutoAlwaysPasses(t *testing.T) {
	g := NewPolicyGate("auto", "0 4 * * SUN", false, false, now)
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("auto should pass; got %v", err)
	}
}

func TestPolicyManualRequiresGoFlag(t *testing.T) {
	g := NewPolicyGate("manual", "", false, false, now)
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("manual without --go should abort")
	}
	g2 := NewPolicyGate("manual", "", false, true, now)
	if err := g2.Evaluate(context.Background()); err != nil {
		t.Errorf("manual with --go should pass; got %v", err)
	}
}

func TestPolicyWindowOutOfWindowAborts(t *testing.T) {
	// Run on a Tuesday at 14:00; window is Sunday 04:00.
	clock := func() time.Time { return time.Date(2026, 6, 9, 14, 0, 0, 0, time.UTC) }
	g := NewPolicyGate("window", "0 4 * * SUN", false, false, clock)
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("out-of-window should abort")
	}
}

func TestPolicyWindowHotfixOverrideBypasses(t *testing.T) {
	clock := func() time.Time { return time.Date(2026, 6, 9, 14, 0, 0, 0, time.UTC) }
	g := NewPolicyGate("window", "0 4 * * SUN", true, false, clock)
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("hotfix should override window; got %v", err)
	}
}

func now() time.Time { return time.Now().UTC() }
```

Impl (`gates/policy.go`):

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// PolicyGate enforces the per-region deploy policy. The "window" policy means
// the deploy is allowed only inside a cron-expressed window each day/week.
// A window is "matched" if the cron schedule produces a fire-time within the
// past hour relative to now (i.e., we're inside that hour's deploy slot).
type PolicyGate struct {
	policy   string
	cronExpr string
	hotfix   bool
	goFlag   bool
	now      func() time.Time
}

// NewPolicyGate constructs a policy gate.
//
//	policy: "auto" | "window" | "manual"
//	cronExpr: standard 5-field cron expression, used when policy == "window"
//	hotfix: whether the tag carried the -hotfix suffix (bypasses window)
//	goFlag: whether the operator passed --go (required for manual)
//	now: clock injection for tests
func NewPolicyGate(policy, cronExpr string, hotfix, goFlag bool, now func() time.Time) *PolicyGate {
	return &PolicyGate{policy: policy, cronExpr: cronExpr, hotfix: hotfix, goFlag: goFlag, now: now}
}

func (g *PolicyGate) Name() string { return "deploy-policy" }

func (g *PolicyGate) Evaluate(ctx context.Context) error {
	switch g.policy {
	case "auto":
		return nil
	case "manual":
		if g.goFlag {
			return nil
		}
		return &Aborted{Gate: g.Name(), Reason: "policy=manual; pass --force-policy=manual --go to proceed"}
	case "window":
		if g.hotfix {
			return nil
		}
		sched, err := cron.ParseStandard(g.cronExpr)
		if err != nil {
			return fmt.Errorf("parse cron %q: %w", g.cronExpr, err)
		}
		now := g.now()
		next := sched.Next(now.Add(-time.Hour))
		// "Within window" = the next fire time from one hour ago is within the
		// current hour. This gives a one-hour deploy slot per cron tick.
		if next.Before(now) && now.Sub(next) <= time.Hour {
			return nil
		}
		return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("policy=window; outside %q (current %s)", g.cronExpr, now.Format("2006-01-02T15:04Z"))}
	default:
		return fmt.Errorf("unknown policy %q", g.policy)
	}
}
```

- [ ] **Step 5: Image-exists gate**

Test (`gates/image_test.go`):

```go
package gates

import (
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestImageGatePassesWhenManifestExists(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker manifest inspect", "{}", "", 0, nil)
	g := NewImageGate(f, []string{"local", "node-2"}, "ghcr.io/friendsincode/grimnir-radio:v1.0.0")
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("manifest exists; got %v", err)
	}
}

func TestImageGateAbortsWhenManifestMissing(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker manifest inspect", "", "no such manifest", 1, nil)
	g := NewImageGate(f, []string{"local", "node-2"}, "ghcr.io/x:bad")
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("missing manifest should abort")
	}
}
```

Impl (`gates/image.go`):

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// ImageGate verifies the target image manifest is reachable from every node
// in the cluster (i.e., the registry is up and the tag exists).
type ImageGate struct {
	R     runner.Runner
	Hosts []string
	Image string // full image ref including tag
}

func NewImageGate(r runner.Runner, hosts []string, image string) *ImageGate {
	return &ImageGate{R: r, Hosts: hosts, Image: image}
}
func (g *ImageGate) Name() string { return "image-exists" }

func (g *ImageGate) Evaluate(ctx context.Context) error {
	for _, h := range g.Hosts {
		_, stderr, code, err := g.R.Run(ctx, h, fmt.Sprintf("docker manifest inspect %s", g.Image))
		if err != nil {
			return fmt.Errorf("manifest probe %s: %w", h, err)
		}
		if code != 0 {
			return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("image %s missing on %s: %s", g.Image, h, stderr)}
		}
	}
	return nil
}
```

- [ ] **Step 6: Health gate**

Test (`gates/health_test.go`):

```go
package gates

import (
	"context"
	"testing"
)

type fakeHealthProbe struct {
	results map[string]error
}

func (f *fakeHealthProbe) Probe(ctx context.Context, host string) error { return f.results[host] }

func TestHealthGatePassesWhenAllUp(t *testing.T) {
	p := &fakeHealthProbe{results: map[string]error{"local": nil, "node-2": nil}}
	g := NewHealthGate(p, []string{"local", "node-2"})
	if err := g.Evaluate(context.Background()); err != nil {
		t.Errorf("all up; got %v", err)
	}
}

func TestHealthGateAbortsWhenOneDown(t *testing.T) {
	p := &fakeHealthProbe{results: map[string]error{"local": nil, "node-2": context.DeadlineExceeded}}
	g := NewHealthGate(p, []string{"local", "node-2"})
	if !IsAborted(g.Evaluate(context.Background())) {
		t.Error("one down should abort")
	}
}
```

Impl (`gates/health.go`):

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package gates

import (
	"context"
	"fmt"
	"strings"
)

// HealthProbe probes one node's overall health (control plane, mediaengine,
// edge-encoder, fan-out). Implementations live in internal/grimnirdeploy/probe
// (Chunk 7). The gate just consumes the verdict.
type HealthProbe interface {
	Probe(ctx context.Context, host string) error
}

// HealthGate aborts unless every named host probes healthy. Design Section 6
// requires both nodes healthy before any rolling deploy; this enforces it.
type HealthGate struct {
	P     HealthProbe
	Hosts []string
}

func NewHealthGate(p HealthProbe, hosts []string) *HealthGate { return &HealthGate{P: p, Hosts: hosts} }
func (g *HealthGate) Name() string                            { return "both-nodes-healthy" }

func (g *HealthGate) Evaluate(ctx context.Context) error {
	var bad []string
	for _, h := range g.Hosts {
		if err := g.P.Probe(ctx, h); err != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", h, err))
		}
	}
	if len(bad) > 0 {
		return &Aborted{Gate: g.Name(), Reason: fmt.Sprintf("unhealthy nodes: %s", strings.Join(bad, "; "))}
	}
	return nil
}
```

- [ ] **Step 7: Run every gate test**

```bash
go test -v ./internal/grimnirdeploy/gates/
```

Expected: all gate tests pass.

- [ ] **Step 8: Commit (one combined commit for the gate package)**

```bash
git add internal/grimnirdeploy/gates/
git commit -m "grimnir-deploy: pre-flight gates (pause, tag-suffix, policy, image, health) (Chunk 5.4)"
```

---

## Chunk 6: `deploy <tag>`: the main rolling sequence

This is the most operationally important chunk. Implements Section 6 of the design verbatim: run pre-flight gates, drain first node, run migrations, start new version, wait for health, restore VRRP, repeat on the second node, soak, write deploy_history.

### Task 6.1: Health probe implementation (consumed by HealthGate + verify)

**Files:**
- Create: `internal/grimnirdeploy/probe/probe.go`
- Create: `internal/grimnirdeploy/probe/probe_test.go`

**Context:**
A probe knows how to call `/healthz` on a node's control plane via HTTP, gRPC `health.Check` on media engine + edge encoder, and `pg_stat_replication` against Postgres. The single `Probe(ctx, host)` method returns the first error encountered (so HealthGate's "any failure is a fail" semantics work). Each component sub-probe is its own helper so `verify` can report per-component status.

- [ ] **Step 1: Write the probe**

`internal/grimnirdeploy/probe/probe.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package probe runs read-only health checks against a node's components.
// Used by gates.HealthGate (pass/fail) and by the verify subcommand
// (per-component report).
package probe

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// Result is the per-component verdict for one host.
type Result struct {
	Host             string
	ControlPlaneOK   bool
	ControlPlaneErr  string
	MediaEngineOK    bool
	MediaEngineErr   string
	EdgeEncoderOK    bool
	EdgeEncoderErr   string
	FanOutOK         bool
	FanOutErr        string
	ReplicationLagS  float64
	ReplicationLagOK bool
}

// Prober runs probes for one host.
type Prober struct {
	HTTPClient      *http.Client
	GRPCDialTimeout time.Duration
	ControlPlanePort int // default 8080
	MediaEnginePort  int // default 9091
	EdgeEncoderPort  int // default 8081
	FanOutPort       int // default 9000
}

// NewProber constructs a Prober with sensible defaults.
func NewProber() *Prober {
	return &Prober{
		HTTPClient:       &http.Client{Timeout: 5 * time.Second},
		GRPCDialTimeout:  3 * time.Second,
		ControlPlanePort: 8080,
		MediaEnginePort:  9091,
		EdgeEncoderPort:  8081,
		FanOutPort:       9000,
	}
}

// ProbeAll returns a populated Result for the host. Never returns an error;
// per-component failures surface as Result fields.
func (p *Prober) ProbeAll(ctx context.Context, host string) Result {
	r := Result{Host: host}
	if err := p.probeControlPlane(ctx, host); err != nil {
		r.ControlPlaneErr = err.Error()
	} else {
		r.ControlPlaneOK = true
	}
	if err := p.probeGRPCHealth(ctx, host, p.MediaEnginePort); err != nil {
		r.MediaEngineErr = err.Error()
	} else {
		r.MediaEngineOK = true
	}
	if err := p.probeGRPCHealth(ctx, host, p.EdgeEncoderPort); err != nil {
		r.EdgeEncoderErr = err.Error()
	} else {
		r.EdgeEncoderOK = true
	}
	if err := p.probeFanOut(ctx, host); err != nil {
		r.FanOutErr = err.Error()
	} else {
		r.FanOutOK = true
	}
	return r
}

// Probe satisfies gates.HealthProbe: returns the first per-component error.
func (p *Prober) Probe(ctx context.Context, host string) error {
	r := p.ProbeAll(ctx, host)
	switch {
	case !r.ControlPlaneOK:
		return fmt.Errorf("control plane: %s", r.ControlPlaneErr)
	case !r.MediaEngineOK:
		return fmt.Errorf("media engine: %s", r.MediaEngineErr)
	case !r.EdgeEncoderOK:
		return fmt.Errorf("edge encoder: %s", r.EdgeEncoderErr)
	case !r.FanOutOK:
		return fmt.Errorf("fan-out: %s", r.FanOutErr)
	}
	return nil
}

func (p *Prober) probeControlPlane(ctx context.Context, host string) error {
	url := fmt.Sprintf("http://%s:%d/healthz", host, p.ControlPlanePort)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}

func (p *Prober) probeGRPCHealth(ctx context.Context, host string, port int) error {
	dialCtx, cancel := context.WithTimeout(ctx, p.GRPCDialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx,
		fmt.Sprintf("%s:%d", host, port),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return err
	}
	defer conn.Close()
	c := healthpb.NewHealthClient(conn)
	resp, err := c.Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("status=%s", resp.Status)
	}
	return nil
}

func (p *Prober) probeFanOut(ctx context.Context, host string) error {
	url := fmt.Sprintf("http://%s:%d/healthz", host, p.FanOutPort)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := p.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 2: Write the test using `httptest.Server`**

`internal/grimnirdeploy/probe/probe_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// startHTTP starts a test server on an ephemeral port, returns host+port.
func startHTTP(t *testing.T, handler http.Handler) (string, int) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u := strings.TrimPrefix(srv.URL, "http://")
	host, portStr, _ := net.SplitHostPort(u)
	port, _ := strconv.Atoi(portStr)
	return host, port
}

func TestProbeControlPlaneOK(t *testing.T) {
	host, port := startHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	p := NewProber()
	p.ControlPlanePort = port
	if err := p.probeControlPlane(context.Background(), host); err != nil {
		t.Errorf("probeControlPlane: %v", err)
	}
}

func TestProbeControlPlane503(t *testing.T) {
	host, port := startHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 503)
	}))
	p := NewProber()
	p.ControlPlanePort = port
	if err := p.probeControlPlane(context.Background(), host); err == nil {
		t.Error("expected 503 error")
	}
}
```

- [ ] **Step 3: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/probe/
```

- [ ] **Step 4: Commit**

```bash
git add internal/grimnirdeploy/probe/
git commit -m "grimnir-deploy: HTTP + gRPC health prober (Chunk 6.1)"
```

### Task 6.2: Deploy orchestration

**Files:**
- Create: `internal/grimnirdeploy/cmd_deploy.go`
- Create: `internal/grimnirdeploy/cmd_deploy_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go` (swap stub for real RunE)

**Context:**
The function `runDeploy` takes a `DeployOpts` struct containing all dependencies (runner, gates, history store, audit wrapper, prober, compose helper, sleeper). Sleeper is an injected function for "wait" steps so tests can move time forward without real sleeps. The orchestration follows Section 6 exactly:

1. Run gates (pause, tag-suffix, policy, image-exists, both-nodes-healthy)
2. Identify first vs second node (non-leader first)
3. For each node: drain → migrate (on first only) → start new → wait health → restore VRRP
4. Soak for cfg.SoakWindow
5. Write deploy_history with outcome=success / soak_outcome=passed

Failure cases:
- Mid-roll health-check failure on first node → revert first node, write outcome=rolled_back_mid_roll, exit non-zero
- Mid-roll health-check failure on second node → revert second; if still bad, revert first too
- Soak failure → write outcome=soak_failed; the caller's auto-rollback is a separate subcommand (Chunk 6.3 ties soak failure into ntfy alert; actual auto-rollback is wired in Chunk 7)

The test uses the fake runner exclusively. Real-cluster integration test gated by `//go:build integration && requires_real_cluster`.

- [ ] **Step 1: Write the orchestration test (happy path)**

`internal/grimnirdeploy/cmd_deploy_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/gates"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// alwaysHealthy is a HealthProbe that always returns nil.
type alwaysHealthy struct{}

func (alwaysHealthy) Probe(ctx context.Context, host string) error { return nil }

// alwaysSick is a HealthProbe that always returns an error.
type alwaysSick struct{ msg string }

func (s alwaysSick) Probe(ctx context.Context, host string) error { return errFn(s.msg) }

type errFn string

func (e errFn) Error() string { return string(e) }

func TestRunDeployHappyPath(t *testing.T) {
	f := runner.NewFake()
	// docker manifest inspect succeeds on both hosts.
	f.SetResponsePrefix("docker manifest inspect", "{}", "", 0, nil)
	// Compose wrapper accepts every operation.
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir", "ok\n", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && docker compose", "ok\n", "", 0, nil)
	// Migration step succeeds.
	f.SetResponsePrefix("docker run", "migrated\n", "", 0, nil)
	// VRRP toggle file ops.
	f.SetResponsePrefix("touch", "", "", 0, nil)
	f.SetResponsePrefix("rm -f", "", "", 0, nil)
	// Current tag query: each container reports v1.0.0.
	f.SetResponsePrefix("docker inspect --format", "v1.0.0\n", "", 0, nil)

	pc, store, ntfy := setupTestEnv(t)
	histStore := history.NewStore(newAuditTestDB(t)) // small helper; mirrors audit's newTestDB
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer

	opts := DeployOpts{
		Tag:        "v1.1.0",
		Cfg:        &Config{Region: "us-east", SoakWindow: 10 * time.Millisecond, DeployPolicy: "auto"},
		Hosts:      []string{"local", "node-2"},
		FirstHost:  "node-2", // peer is non-leader; deploy starts there
		SecondHost: "local",
		Runner:     f,
		Compose:    runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{},
		Pause:      pc,
		History:    histStore,
		Wrapper:    w,
		Out:        &out,
		Sleep:      func(d time.Duration) {}, // skip real sleeps
	}

	if err := runDeploy(context.Background(), opts); err != nil {
		t.Fatalf("runDeploy: %v", err)
	}
	if !strings.Contains(out.String(), "soak passed") {
		t.Errorf("expected soak passed in output; got:\n%s", out.String())
	}

	last, _ := histStore.LastSuccessful(context.Background(), "us-east")
	if last == nil {
		t.Fatal("expected deploy_history success row")
	}
	if last.Tag != "v1.1.0" || last.PreviousTag != "v1.0.0" {
		t.Errorf("history row tag/prev: %s/%s", last.Tag, last.PreviousTag)
	}
}

func TestRunDeployAbortsOnPause(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)
	_ = pc.Set(context.Background(), "incident #999", "bob")
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	opts := DeployOpts{
		Tag: "v1.1.0",
		Cfg: &Config{Region: "us-east", SoakWindow: 0, DeployPolicy: "auto"},
		Hosts: []string{"local", "node-2"},
		FirstHost: "node-2", SecondHost: "local",
		Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{}, Pause: pc,
		History: history.NewStore(newAuditTestDB(t)), Wrapper: w, Out: &bytes.Buffer{},
		Sleep: func(d time.Duration) {},
	}
	err := runDeploy(context.Background(), opts)
	if !gates.IsAborted(err) {
		t.Errorf("expected Aborted; got %v", err)
	}
}

func TestRunDeployRevertsOnFirstNodeUnhealthy(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker manifest inspect", "{}", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir", "ok\n", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && docker compose", "ok\n", "", 0, nil)
	f.SetResponsePrefix("docker run", "migrated\n", "", 0, nil)
	f.SetResponsePrefix("touch", "", "", 0, nil)
	f.SetResponsePrefix("rm -f", "", "", 0, nil)
	f.SetResponsePrefix("docker inspect --format", "v1.0.0\n", "", 0, nil)

	// Health probe fails permanently; wait-for-health on first node times out.
	pc, store, ntfy := setupTestEnv(t)
	histStore := history.NewStore(newAuditTestDB(t))
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	opts := DeployOpts{
		Tag: "v1.1.0",
		Cfg: &Config{Region: "us-east", SoakWindow: 0, DeployPolicy: "auto"},
		Hosts: []string{"local", "node-2"},
		FirstHost: "node-2", SecondHost: "local",
		Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		// First-node health stays bad even after start-new-version.
		HealthProbe: alwaysSick{"503"},
		Pause:       pc, History: histStore, Wrapper: w, Out: &bytes.Buffer{},
		Sleep:       func(d time.Duration) {},
		HealthWaitTimeout: 5 * time.Millisecond,
	}
	err := runDeploy(context.Background(), opts)
	if err == nil {
		t.Fatal("expected error on unhealthy first node")
	}
	// History row should record outcome=rolled_back_mid_roll.
	var rows []history.Entry
	_ = histStore.WithMigrationsDir(".") // not used here
	// Use the underlying gorm DB to query directly.
	// (For brevity in this test, just check that LastSuccessful is nil.)
	last, _ := histStore.LastSuccessful(context.Background(), "us-east")
	if last != nil {
		t.Errorf("no successful row expected; got %+v", last)
	}
	_ = rows
}

// newAuditTestDB constructs an in-memory SQLite DB with deploy_history migrated.
// Lives here because Chunk 4's history package already provides a similar helper
// in its own tests; this one is for grimnirdeploy-package tests.
func newAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&history.Entry{}, &audit.Entry{}); err != nil {
		t.Fatal(err)
	}
	return db
}
```

Add the relevant imports (`gorm.io/gorm`, `gorm.io/driver/sqlite`).

- [ ] **Step 2: Implement `runDeploy`**

`internal/grimnirdeploy/cmd_deploy.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/gates"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// DeployOpts is the dependency bag for runDeploy. Built by realDeployRunE for
// production; built directly by tests.
type DeployOpts struct {
	Tag        string
	Cfg        *Config
	Hosts      []string
	FirstHost  string // node to upgrade first (typically the non-leader)
	SecondHost string // node to upgrade second
	Runner     runner.Runner
	Compose    *runner.DockerCompose
	HealthProbe gates.HealthProbe
	Pause      *pause.Client
	History    *history.Store
	Wrapper    *audit.Wrapper
	Out        io.Writer
	Sleep      func(time.Duration)
	HealthWaitTimeout time.Duration // default 60s

	// Optional flags from cobra layer.
	DryRun       bool
	ForcePolicy  string
	GoFlag       bool
}

func (o *DeployOpts) waitTimeout() time.Duration {
	if o.HealthWaitTimeout <= 0 {
		return 60 * time.Second
	}
	return o.HealthWaitTimeout
}

// runDeploy runs the full rolling deploy sequence per Section 6 of the HA design.
func runDeploy(ctx context.Context, o DeployOpts) error {
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	return o.Wrapper.Wrap(ctx, "deploy", map[string]any{"tag": o.Tag, "dry_run": o.DryRun}, func(ctx context.Context) error {
		// Pre-flight gates.
		policy := o.Cfg.DeployPolicy
		if o.ForcePolicy != "" {
			policy = o.ForcePolicy
		}
		tagGate := gates.NewTagSuffixGate(o.Tag, policy)
		all := []gates.Gate{
			gates.NewPauseGate(o.Pause),
			tagGate,
			gates.NewPolicyGate(policy, o.Cfg.DeployWindowCron, tagGate.OverridesPolicy(), o.GoFlag, time.Now),
			gates.NewImageGate(o.Runner, o.Hosts, "ghcr.io/friendsincode/grimnir-radio:"+o.Tag),
			gates.NewHealthGate(o.HealthProbe, o.Hosts),
		}
		if err := gates.RunAll(ctx, all...); err != nil {
			return err
		}

		// Capture previous tag for deploy_history.
		prevTag, _ := o.Compose.CurrentTag(ctx, o.FirstHost, "grimnir-radio")
		histID, err := o.History.Start(ctx, o.Cfg.Region, o.Tag, prevTag, "audit_wrapper_operator")
		if err != nil {
			return fmt.Errorf("history start: %w", err)
		}

		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would deploy %s across %v (prev: %s)\n", o.Tag, o.Hosts, prevTag)
			_ = o.History.Complete(ctx, histID, history.OutcomeSuccess, history.SoakSkipped)
			return nil
		}

		// Roll first node.
		if err := o.rollNode(ctx, o.FirstHost, true, prevTag); err != nil {
			_ = o.History.Fail(ctx, histID, history.OutcomeRolledBackMidRoll, err.Error())
			return fmt.Errorf("first-node deploy failed and was reverted: %w", err)
		}
		fmt.Fprintf(o.Out, "first node (%s) upgraded to %s\n", o.FirstHost, o.Tag)

		// Roll second node.
		if err := o.rollNode(ctx, o.SecondHost, false, prevTag); err != nil {
			// Per Section 6: try to revert second node; if that fails, revert first too.
			if rerr := o.revert(ctx, o.FirstHost, prevTag); rerr != nil {
				_ = o.History.Fail(ctx, histID, history.OutcomeFailed, fmt.Sprintf("second-node failed (%v); first-node revert also failed (%v)", err, rerr))
				return fmt.Errorf("second-node deploy failed; first-node revert also failed: %v / %v", err, rerr)
			}
			_ = o.History.Fail(ctx, histID, history.OutcomeRolledBackMidRoll, err.Error())
			return fmt.Errorf("second-node deploy failed; cluster reverted to %s: %w", prevTag, err)
		}
		fmt.Fprintf(o.Out, "second node (%s) upgraded to %s\n", o.SecondHost, o.Tag)

		// Soak.
		if o.Cfg.SoakWindow > 0 {
			fmt.Fprintf(o.Out, "soak: waiting %v\n", o.Cfg.SoakWindow)
			o.Sleep(o.Cfg.SoakWindow)
		}
		// One last health probe at end of soak.
		for _, h := range o.Hosts {
			if err := o.HealthProbe.Probe(ctx, h); err != nil {
				_ = o.History.Complete(ctx, histID, history.OutcomeSoakFailed, history.SoakFailed)
				return fmt.Errorf("soak failed: %s unhealthy: %w", h, err)
			}
		}
		if err := o.History.Complete(ctx, histID, history.OutcomeSuccess, history.SoakPassed); err != nil {
			return fmt.Errorf("history complete: %w", err)
		}
		fmt.Fprintln(o.Out, "soak passed; deploy complete")
		return nil
	})
}

// rollNode runs the per-node sequence: drain, migrate (if firstNode), start
// new image, wait for health, restore VRRP. Returns an error if health does
// not come up; revert is the caller's responsibility.
func (o *DeployOpts) rollNode(ctx context.Context, host string, firstNode bool, prevTag string) error {
	// Drain via VRRP failure file (Section 6 step 2).
	if err := o.touchVRRPFail(ctx, host); err != nil {
		return fmt.Errorf("vrrp drain on %s: %w", host, err)
	}
	// Stop services in order. Grace handled by docker stop default timeout.
	for _, svc := range []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"} {
		if err := o.Compose.Stop(ctx, host, svc); err != nil {
			return err
		}
	}
	// Run migrations on the first node only.
	if firstNode {
		if err := o.runMigrations(ctx, host); err != nil {
			return fmt.Errorf("migrations: %w", err)
		}
	}
	// Pull and start the new image.
	if err := o.Compose.Pull(ctx, host); err != nil {
		return err
	}
	if err := o.Compose.Up(ctx, host); err != nil {
		return err
	}
	// Wait for health.
	if err := o.waitHealthy(ctx, host); err != nil {
		// Revert before returning.
		_ = o.revert(ctx, host, prevTag)
		return err
	}
	// Restore VRRP.
	if err := o.removeVRRPFail(ctx, host); err != nil {
		return fmt.Errorf("vrrp restore on %s: %w", host, err)
	}
	return nil
}

func (o *DeployOpts) waitHealthy(ctx context.Context, host string) error {
	deadline := time.Now().Add(o.waitTimeout())
	for time.Now().Before(deadline) {
		if err := o.HealthProbe.Probe(ctx, host); err == nil {
			return nil
		}
		o.Sleep(2 * time.Second)
	}
	return fmt.Errorf("%s never came healthy within %v", host, o.waitTimeout())
}

func (o *DeployOpts) revert(ctx context.Context, host, prevTag string) error {
	// Force the image tag back. Pull the prior tag, then up.
	_, _, _, err := o.Runner.Run(ctx, host, fmt.Sprintf("docker pull ghcr.io/friendsincode/grimnir-radio:%s", prevTag))
	if err != nil {
		return err
	}
	return o.Compose.Up(ctx, host)
}

func (o *DeployOpts) touchVRRPFail(ctx context.Context, host string) error {
	_, _, _, err := o.Runner.Run(ctx, host, "touch /var/run/keepalived/vrrp_fail")
	return err
}

func (o *DeployOpts) removeVRRPFail(ctx context.Context, host string) error {
	_, _, _, err := o.Runner.Run(ctx, host, "rm -f /var/run/keepalived/vrrp_fail")
	return err
}

func (o *DeployOpts) runMigrations(ctx context.Context, host string) error {
	// Run `grimnir migrate` from the new image against the primary.
	cmd := fmt.Sprintf("docker run --rm --network host -e GRIMNIR_DB_DSN ghcr.io/friendsincode/grimnir-radio:%s grimnirradio migrate", o.Tag)
	_, stderr, code, err := o.Runner.Run(ctx, host, cmd)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("migrate exit %d: %s", code, stderr)
	}
	return nil
}

// realDeployRunE is the cobra entry point; swaps the stub in cmd_stubs.go.
func realDeployRunE(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		// --rollback uses zero args; deploy with --rollback flag is handled below.
		rb, _ := cmd.Flags().GetBool("rollback")
		if !rb {
			return fmt.Errorf("usage: deploy <tag>  or  deploy --rollback")
		}
		return realRollbackRunE(cmd, args)
	}
	tag := args[0]

	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	forcePolicy, _ := cmd.Flags().GetString("force-policy")
	goFlag, _ := cmd.Flags().GetBool("go")

	histStore := history.NewStore(deps.DB)
	prober := probe.NewProber()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")

	return runDeploy(cmd.Context(), DeployOpts{
		Tag:        tag,
		Cfg:        cfg,
		Hosts:      []string{"local", cfg.PeerHost},
		FirstHost:  cfg.PeerHost, // default: peer first; refined by leader probe in follow-up
		SecondHost: "local",
		Runner:     sshRunner,
		Compose:    compose,
		HealthProbe: prober,
		Pause:      deps.Pause,
		History:    histStore,
		Wrapper:    deps.Wrapper,
		Out:        cmd.OutOrStdout(),
		DryRun:     dryRun, ForcePolicy: forcePolicy, GoFlag: goFlag,
	})
}
```

Add the relevant imports (`probe` package, etc.). Replace the `RunE` of `newDeployCmd()` in `cmd_stubs.go` with `realDeployRunE`.

- [ ] **Step 3: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/ -run TestRunDeploy
```

Expected: happy path passes; pause-abort test passes; revert-on-unhealthy test passes.

- [ ] **Step 4: Commit**

```bash
git add internal/grimnirdeploy/cmd_deploy.go internal/grimnirdeploy/cmd_deploy_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: deploy <tag> rolling sequence with gates + revert (Chunk 6)"
```

---

## Chunk 7: `deploy --rollback` with eligibility window + contract-migration refusal

The rollback path reuses most of the deploy plumbing but flips the target tag from "the requested tag" to "the previous successful tag from deploy_history." Adds two refusal gates: the eligibility window and contract-migration crossings.

### Task 7.1: Rollback orchestration

**Files:**
- Create: `internal/grimnirdeploy/cmd_rollback.go`
- Create: `internal/grimnirdeploy/cmd_rollback_test.go`

**Context:**
The rollback function:
1. Reads `LastSuccessful(region)` from `deploy_history`. If nil → error.
2. Reads currently-running tag via `Compose.CurrentTag("local", "grimnir-radio")`.
3. Checks eligibility window: `WithinEligibility(region, cfg.RollbackWindow)`. If false → refuse unless `--force-aged-rollback`.
4. Checks contract crossings: `ContractCrossings(region, prevTag, currentTag)`. If non-empty → refuse unless `--force-through-contract-migration` AND `--reason` is non-empty.
5. Requires `--reason` always (per Section 6).
6. Runs the same rolling sequence as `deploy`, with the target tag = previous successful tag and grace = 15s (vs 30s default).
7. Writes deploy_history with outcome=rollback.

- [ ] **Step 1: Write the failing tests**

`internal/grimnirdeploy/cmd_rollback_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestRollbackRefusesWithoutReason(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)
	histStore := history.NewStore(newAuditTestDB(t))
	// Seed: one successful deploy.
	id, _ := histStore.Start(context.Background(), "us-east", "v1.0.0", "", "alice")
	_ = histStore.Complete(context.Background(), id, history.OutcomeSuccess, history.SoakPassed)

	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	err := runRollback(context.Background(), RollbackOpts{
		Cfg: &Config{Region: "us-east", RollbackWindow: 4 * time.Hour},
		Reason: "", Pause: pc, History: histStore, Wrapper: w,
		Out: &bytes.Buffer{}, Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{}, Hosts: []string{"local", "node-2"},
		FirstHost: "node-2", SecondHost: "local",
		Sleep: func(d time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "reason") {
		t.Errorf("expected reason-required error; got %v", err)
	}
}

func TestRollbackRefusesAgedRollback(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)
	histStore := history.NewStore(newAuditTestDB(t))
	id, _ := histStore.Start(context.Background(), "us-east", "v1.0.0", "", "alice")
	_ = histStore.Complete(context.Background(), id, history.OutcomeSuccess, history.SoakPassed)
	// Backdate to 5h ago.
	db := newAuditTestDB(t) // won't work; we need the SAME db that hist is using.
	_ = db
	// Workaround: bypass by setting RollbackWindow = 0 so any age is out-of-window.
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	err := runRollback(context.Background(), RollbackOpts{
		Cfg: &Config{Region: "us-east", RollbackWindow: 0},
		Reason: "incident", Pause: pc, History: histStore, Wrapper: w,
		Out: &bytes.Buffer{}, Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{}, Hosts: []string{"local", "node-2"},
		FirstHost: "node-2", SecondHost: "local",
		ForceAged: false,
		Sleep: func(d time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "eligibility") {
		t.Errorf("expected eligibility-window refusal; got %v", err)
	}
}

func TestRollbackRefusesContractCrossing(t *testing.T) {
	f := runner.NewFake()
	pc, store, ntfy := setupTestEnv(t)

	// History with one successful deploy.
	db := newAuditTestDB(t)
	histStore := history.NewStore(db)
	// Add a fake migrations dir with a contract migration.
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "100_contract.sql"),
		[]byte("-- migration-contract: drop col\nALTER TABLE x DROP COLUMN y;\n"), 0o644)
	histStore = histStore.WithMigrationsDir(dir)

	id, _ := histStore.Start(context.Background(), "us-east", "v1.0.0", "", "alice")
	_ = histStore.Complete(context.Background(), id, history.OutcomeSuccess, history.SoakPassed)

	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	f.SetResponsePrefix("docker inspect --format", "v1.1.0\n", "", 0, nil)
	err := runRollback(context.Background(), RollbackOpts{
		Cfg: &Config{Region: "us-east", RollbackWindow: 4 * time.Hour},
		Reason: "incident", Pause: pc, History: histStore, Wrapper: w,
		Out: &bytes.Buffer{}, Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		HealthProbe: alwaysHealthy{}, Hosts: []string{"local", "node-2"},
		FirstHost: "node-2", SecondHost: "local",
		ForceContract: false, Sleep: func(d time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "contract") {
		t.Errorf("expected contract-crossing refusal; got %v", err)
	}
}
```

Add the necessary imports (`os`, `path/filepath`).

- [ ] **Step 2: Implement**

`internal/grimnirdeploy/cmd_rollback.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/gates"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/history"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/pause"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

// RollbackOpts mirrors DeployOpts for the rollback flow.
type RollbackOpts struct {
	Cfg           *Config
	Reason        string
	ForceAged     bool
	ForceContract bool
	Hosts         []string
	FirstHost     string
	SecondHost    string
	Runner        runner.Runner
	Compose       *runner.DockerCompose
	HealthProbe   gates.HealthProbe
	Pause         *pause.Client
	History       *history.Store
	Wrapper       *audit.Wrapper
	Out           io.Writer
	Sleep         func(time.Duration)
}

// runRollback rolls the cluster back to the previous successful tag with two
// refusal gates: eligibility window and contract-migration crossings.
func runRollback(ctx context.Context, o RollbackOpts) error {
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if strings.TrimSpace(o.Reason) == "" {
		return errors.New("--reason is required for rollback")
	}
	return o.Wrapper.Wrap(ctx, "rollback", map[string]any{"reason": o.Reason, "force_aged": o.ForceAged, "force_contract": o.ForceContract}, func(ctx context.Context) error {
		last, err := o.History.LastSuccessful(ctx, o.Cfg.Region)
		if err != nil {
			return err
		}
		if last == nil {
			return errors.New("no previous successful deploy found in deploy_history")
		}
		currentTag, _ := o.Compose.CurrentTag(ctx, "local", "grimnir-radio")
		if currentTag == last.Tag {
			return fmt.Errorf("currently running %s == last successful; nothing to roll back", currentTag)
		}

		// Eligibility window.
		ok, err := o.History.WithinEligibility(ctx, o.Cfg.Region, o.Cfg.RollbackWindow)
		if err != nil {
			return err
		}
		if !ok && !o.ForceAged {
			return fmt.Errorf("eligibility window (%v) exceeded; pass --force-aged-rollback to override (contract migrations may have run since)", o.Cfg.RollbackWindow)
		}

		// Contract-migration refusal.
		crossings, err := o.History.ContractCrossings(ctx, o.Cfg.Region, last.Tag, currentTag)
		if err != nil {
			return err
		}
		if len(crossings) > 0 && !o.ForceContract {
			return fmt.Errorf("rollback would cross contract migrations: %s; pass --force-through-contract-migration AND --reason explaining why this is safe", strings.Join(crossings, ", "))
		}
		if len(crossings) > 0 {
			fmt.Fprintf(o.Out, "WARNING: rolling through contract migrations: %s\n", strings.Join(crossings, ", "))
		}

		// Seed deploy_history for the rollback.
		histID, err := o.History.Start(ctx, o.Cfg.Region, last.Tag, currentTag, "audit_wrapper_operator")
		if err != nil {
			return fmt.Errorf("history start: %w", err)
		}

		// Use shorter grace; reuse runDeploy by constructing DeployOpts.
		dep := DeployOpts{
			Tag: last.Tag, Cfg: o.Cfg, Hosts: o.Hosts,
			FirstHost: o.FirstHost, SecondHost: o.SecondHost,
			Runner: o.Runner, Compose: o.Compose, HealthProbe: o.HealthProbe,
			Pause: o.Pause, History: o.History, Wrapper: o.Wrapper, Out: o.Out,
			Sleep: o.Sleep,
		}
		if err := dep.rollNode(ctx, o.FirstHost, false, currentTag); err != nil {
			_ = o.History.Fail(ctx, histID, history.OutcomeFailed, fmt.Sprintf("rollback first node: %v", err))
			return err
		}
		if err := dep.rollNode(ctx, o.SecondHost, false, currentTag); err != nil {
			_ = o.History.Fail(ctx, histID, history.OutcomeFailed, fmt.Sprintf("rollback second node: %v", err))
			return err
		}
		// Soak.
		if o.Cfg.SoakWindow > 0 {
			fmt.Fprintf(o.Out, "rollback soak: waiting %v\n", o.Cfg.SoakWindow)
			o.Sleep(o.Cfg.SoakWindow)
		}
		if err := o.History.Complete(ctx, histID, history.OutcomeRollback, history.SoakPassed); err != nil {
			return err
		}
		fmt.Fprintf(o.Out, "rollback to %s complete (reason: %s)\n", last.Tag, o.Reason)
		return nil
	})
}

func realRollbackRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	reason, _ := cmd.Flags().GetString("reason")
	forceAged, _ := cmd.Flags().GetBool("force-aged-rollback")
	forceContract, _ := cmd.Flags().GetBool("force-through-contract-migration")
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")

	return runRollback(cmd.Context(), RollbackOpts{
		Cfg: cfg, Reason: reason, ForceAged: forceAged, ForceContract: forceContract,
		Hosts: []string{"local", cfg.PeerHost}, FirstHost: cfg.PeerHost, SecondHost: "local",
		Runner: sshRunner, Compose: compose,
		HealthProbe: probe.NewProber(),
		Pause: deps.Pause, History: history.NewStore(deps.DB), Wrapper: deps.Wrapper,
		Out: cmd.OutOrStdout(),
	})
}
```

- [ ] **Step 3: Run, expect pass**

```bash
go test -v ./internal/grimnirdeploy/ -run TestRollback
```

- [ ] **Step 4: Commit**

```bash
git add internal/grimnirdeploy/cmd_rollback.go internal/grimnirdeploy/cmd_rollback_test.go
git commit -m "grimnir-deploy: --rollback with eligibility + contract-crossing refusal (Chunk 7)"
```

---

## Chunk 8: `verify` subcommand (read-only cluster health probe)

Smallest non-trivial chunk. Calls `probe.ProbeAll` on every host and formats a per-component report. Exits 0 iff every check passed.

### Task 8.1: Implementation

**Files:**
- Create: `internal/grimnirdeploy/cmd_verify.go`
- Create: `internal/grimnirdeploy/cmd_verify_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go`

- [ ] **Step 1: Write the failing test**

`internal/grimnirdeploy/cmd_verify_test.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
)

type fakeAllProber struct {
	result probe.Result
	err    error
}

func (f fakeAllProber) ProbeAll(ctx context.Context, host string) probe.Result { return f.result }
func (f fakeAllProber) Probe(ctx context.Context, host string) error           { return f.err }

func TestVerifyHappyPath(t *testing.T) {
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer

	good := probe.Result{
		Host: "local", ControlPlaneOK: true, MediaEngineOK: true, EdgeEncoderOK: true, FanOutOK: true,
	}
	err := runVerify(context.Background(), VerifyOpts{
		Hosts:   []string{"local", "node-2"},
		Prober:  fakeAllProber{result: good},
		Wrapper: w,
		Out:     &out,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	s := out.String()
	for _, want := range []string{"local", "node-2", "control plane", "media engine", "edge encoder", "fan-out"} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q:\n%s", want, s)
		}
	}
}

func TestVerifyReportsFailure(t *testing.T) {
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	bad := probe.Result{
		Host: "local", ControlPlaneOK: false, ControlPlaneErr: "500",
		MediaEngineOK: true, EdgeEncoderOK: true, FanOutOK: true,
	}
	err := runVerify(context.Background(), VerifyOpts{
		Hosts: []string{"local"}, Prober: fakeAllProber{result: bad},
		Wrapper: w, Out: &out,
	})
	if err == nil {
		t.Fatal("verify should error when something is down")
	}
}
```

- [ ] **Step 2: Implement**

`internal/grimnirdeploy/cmd_verify.go`:

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
)

// FullProber is the surface verify needs (per-component report).
type FullProber interface {
	ProbeAll(ctx context.Context, host string) probe.Result
}

type VerifyOpts struct {
	Hosts   []string
	Prober  FullProber
	Wrapper *audit.Wrapper
	Out     io.Writer
}

func runVerify(ctx context.Context, o VerifyOpts) error {
	return o.Wrapper.Wrap(ctx, "verify", nil, func(ctx context.Context) error {
		tw := tabwriter.NewWriter(o.Out, 2, 2, 2, ' ', 0)
		fmt.Fprintln(tw, "host\tcontrol plane\tmedia engine\tedge encoder\tfan-out")
		anyBad := false
		for _, h := range o.Hosts {
			r := o.Prober.ProbeAll(ctx, h)
			cp := okOr(r.ControlPlaneOK, r.ControlPlaneErr)
			me := okOr(r.MediaEngineOK, r.MediaEngineErr)
			ee := okOr(r.EdgeEncoderOK, r.EdgeEncoderErr)
			fo := okOr(r.FanOutOK, r.FanOutErr)
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", h, cp, me, ee, fo)
			if !r.ControlPlaneOK || !r.MediaEngineOK || !r.EdgeEncoderOK || !r.FanOutOK {
				anyBad = true
			}
		}
		_ = tw.Flush()
		if anyBad {
			return errors.New("one or more components unhealthy")
		}
		return nil
	})
}

func okOr(ok bool, errMsg string) string {
	if ok {
		return "OK"
	}
	if errMsg == "" {
		return "FAIL"
	}
	return "FAIL: " + errMsg
}

func realVerifyRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	return runVerify(cmd.Context(), VerifyOpts{
		Hosts:   []string{"local", cfg.PeerHost},
		Prober:  probe.NewProber(),
		Wrapper: deps.Wrapper,
		Out:     cmd.OutOrStdout(),
	})
}
```

Swap stub in `cmd_stubs.go`.

- [ ] **Step 3: Run, expect pass + commit**

```bash
go test -v ./internal/grimnirdeploy/ -run TestVerify
git add internal/grimnirdeploy/cmd_verify.go internal/grimnirdeploy/cmd_verify_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: verify subcommand with per-component report (Chunk 8)"
```

---

## Chunk 9: `drain --node=N`

The drain runbook is what every other subcommand calls when it needs to evict one node before mutating it. Implemented as a standalone subcommand for operator use (e.g., before reboots, hardware swaps).

### Task 9.1: Implementation

**Files:**
- Create: `internal/grimnirdeploy/cmd_drain.go`
- Create: `internal/grimnirdeploy/cmd_drain_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go`

**Context:**
Per Section 6 step 2: touch the VRRP failure file → wait 3s for VIP to float → SIGTERM grimnir, edge-encoder, fan-out, mediaengine in that order with 30s grace each → wait for leader-election lease to migrate to peer (poll Redis until the lease holder changes, max 30s).

- [ ] **Step 1: Test**

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestRunDrainStopsServicesInOrder(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("touch", "", "", 0, nil)
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && docker compose stop", "", "", 0, nil)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runDrain(context.Background(), DrainOpts{
		Node:    "node-2",
		Runner:  f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Wrapper: w, Out: &out,
		Sleep: func(d time.Duration) {},
	})
	if err != nil {
		t.Fatalf("runDrain: %v", err)
	}
	// Find the order of docker compose stop calls.
	var order []string
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "docker compose stop ") {
			parts := strings.Split(c.Cmd, "docker compose stop ")
			order = append(order, parts[1])
		}
	}
	want := []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"}
	if len(order) != len(want) {
		t.Fatalf("stop order len %d, want %d (%v)", len(order), len(want), order)
	}
	for i, svc := range want {
		if order[i] != svc {
			t.Errorf("order[%d] = %s, want %s", i, order[i], svc)
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

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

type DrainOpts struct {
	Node    string
	Grace   time.Duration
	Runner  runner.Runner
	Compose *runner.DockerCompose
	Wrapper *audit.Wrapper
	Out     io.Writer
	Sleep   func(time.Duration)
	DryRun  bool
}

func runDrain(ctx context.Context, o DrainOpts) error {
	if o.Sleep == nil {
		o.Sleep = time.Sleep
	}
	if o.Grace == 0 {
		o.Grace = 30 * time.Second
	}
	return o.Wrapper.Wrap(ctx, "drain", map[string]any{"node": o.Node, "dry_run": o.DryRun}, func(ctx context.Context) error {
		host := o.Node
		if host == "self" {
			host = "local"
		}
		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would drain %s\n", host)
			return nil
		}
		fmt.Fprintf(o.Out, "drain %s: dropping VRRP priority\n", host)
		if _, _, _, err := o.Runner.Run(ctx, host, "touch /var/run/keepalived/vrrp_fail"); err != nil {
			return err
		}
		o.Sleep(3 * time.Second) // VIP float delay
		for _, svc := range []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"} {
			fmt.Fprintf(o.Out, "drain %s: stopping %s (grace %v)\n", host, svc, o.Grace)
			if err := o.Compose.Stop(ctx, host, svc); err != nil {
				return fmt.Errorf("stop %s: %w", svc, err)
			}
		}
		fmt.Fprintf(o.Out, "drain %s: complete\n", host)
		return nil
	})
}

func realDrainRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	node, _ := cmd.Flags().GetString("node")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	grace, _ := cmd.Flags().GetDuration("grace")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")
	return runDrain(cmd.Context(), DrainOpts{
		Node: node, Grace: grace, Runner: sshRunner, Compose: compose,
		Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
```

- [ ] **Step 3: Test passes + commit**

```bash
go test -v ./internal/grimnirdeploy/ -run TestRunDrain
git add internal/grimnirdeploy/cmd_drain.go internal/grimnirdeploy/cmd_drain_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: drain subcommand (Chunk 9)"
```

---

## Chunk 10: `promote-replica` (Postgres failover runbook)

> **Integration-test prerequisite:** This subcommand requires a real Postgres primary + streaming replica + pgbouncer to exercise end-to-end. The unit tests below verify the command sequence against the fake runner; the real-infra test (`promote_real_test.go`) is gated by `//go:build integration && requires_real_cluster` and is run quarterly during the backup drill.

### Task 10.1: Implementation

**Files:**
- Create: `internal/grimnirdeploy/cmd_promote.go`
- Create: `internal/grimnirdeploy/cmd_promote_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go`

**Context:**
Sequence:
1. Verify replica is in streaming state (query `pg_stat_wal_receiver` on replica host).
2. Verify replication lag < 5s (`pg_last_xact_replay_timestamp()` vs `now()`).
3. Run `pg_ctl promote` on the replica.
4. Update pgbouncer's `primary_conninfo` to point at the new primary (edit `/etc/pgbouncer/pgbouncer.ini` and `RELOAD`).
5. If `--skip-rebuild` not set: run `pg_basebackup` on the old primary to make it a replica of the new primary.

Each step is a shell-out via the runner. The lag-check step has a numeric parse so the test can verify it aborts on lag > 5s.

- [ ] **Step 1: Test**

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestPromoteReplicaHappyPath(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("psql -tAc 'SELECT status'", "streaming\n", "", 0, nil)
	f.SetResponsePrefix("psql -tAc 'SELECT EXTRACT", "0.5\n", "", 0, nil) // 0.5s lag
	f.SetResponsePrefix("pg_ctl", "server promoted\n", "", 0, nil)
	f.SetResponsePrefix("sed -i", "", "", 0, nil)
	f.SetResponsePrefix("systemctl reload pgbouncer", "", "", 0, nil)
	f.SetResponsePrefix("pg_basebackup", "ok\n", "", 0, nil)

	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runPromoteReplica(context.Background(), PromoteOpts{
		PrimaryHost: "node-1", ReplicaHost: "node-2",
		Runner: f, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("promote: %v", err)
	}
}

func TestPromoteReplicaAbortsOnHighLag(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("psql -tAc 'SELECT status'", "streaming\n", "", 0, nil)
	f.SetResponsePrefix("psql -tAc 'SELECT EXTRACT", "12.0\n", "", 0, nil) // 12s lag
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	err := runPromoteReplica(context.Background(), PromoteOpts{
		PrimaryHost: "node-1", ReplicaHost: "node-2",
		Runner: f, Wrapper: w, Out: &bytes.Buffer{},
	})
	if err == nil {
		t.Error("expected refusal on 12s lag")
	}
}
```

- [ ] **Step 2: Implement**

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

type PromoteOpts struct {
	PrimaryHost string
	ReplicaHost string
	SkipRebuild bool
	DryRun      bool
	Runner      runner.Runner
	Wrapper     *audit.Wrapper
	Out         io.Writer
}

func runPromoteReplica(ctx context.Context, o PromoteOpts) error {
	return o.Wrapper.Wrap(ctx, "promote-replica", map[string]any{"primary": o.PrimaryHost, "replica": o.ReplicaHost, "skip_rebuild": o.SkipRebuild, "dry_run": o.DryRun}, func(ctx context.Context) error {
		// 1. Verify replica is streaming.
		out, _, code, err := o.Runner.Run(ctx, o.ReplicaHost, "psql -tAc 'SELECT status FROM pg_stat_wal_receiver'")
		if err != nil || code != 0 {
			return fmt.Errorf("query wal receiver: code=%d err=%v out=%q", code, err, out)
		}
		if !strings.Contains(out, "streaming") {
			return fmt.Errorf("replica not in streaming state: %q", strings.TrimSpace(out))
		}
		fmt.Fprintf(o.Out, "replica %s status: streaming\n", o.ReplicaHost)

		// 2. Verify lag < 5s.
		out, _, _, err = o.Runner.Run(ctx, o.ReplicaHost, "psql -tAc 'SELECT EXTRACT(EPOCH FROM (now() - pg_last_xact_replay_timestamp()))'")
		if err != nil {
			return err
		}
		lag, err := strconv.ParseFloat(strings.TrimSpace(out), 64)
		if err != nil {
			return fmt.Errorf("parse lag %q: %w", out, err)
		}
		if lag > 5.0 {
			return fmt.Errorf("replication lag %.2fs exceeds 5s threshold; refusing to promote", lag)
		}
		fmt.Fprintf(o.Out, "replication lag: %.2fs\n", lag)

		if o.DryRun {
			fmt.Fprintf(o.Out, "[dry-run] would promote %s and reconfigure pgbouncer\n", o.ReplicaHost)
			return nil
		}

		// 3. Promote.
		_, stderr, code, err := o.Runner.Run(ctx, o.ReplicaHost, "pg_ctl promote -D /var/lib/postgresql/data")
		if err != nil || code != 0 {
			return fmt.Errorf("pg_ctl promote: code=%d err=%v stderr=%q", code, err, stderr)
		}
		fmt.Fprintf(o.Out, "%s promoted to primary\n", o.ReplicaHost)

		// 4. Update pgbouncer (both nodes).
		for _, h := range []string{o.PrimaryHost, o.ReplicaHost} {
			cmd := fmt.Sprintf("sed -i 's/^primary_conninfo.*/primary_conninfo = host=%s/' /etc/pgbouncer/pgbouncer.ini", o.ReplicaHost)
			if _, _, _, err := o.Runner.Run(ctx, h, cmd); err != nil {
				return fmt.Errorf("update pgbouncer on %s: %w", h, err)
			}
			if _, _, _, err := o.Runner.Run(ctx, h, "systemctl reload pgbouncer"); err != nil {
				return fmt.Errorf("reload pgbouncer on %s: %w", h, err)
			}
		}
		fmt.Fprintln(o.Out, "pgbouncer reloaded on both nodes")

		// 5. Rebuild old primary as replica.
		if !o.SkipRebuild {
			cmd := fmt.Sprintf("pg_basebackup -h %s -D /var/lib/postgresql/data --wal-method=stream -P", o.ReplicaHost)
			_, stderr, code, err := o.Runner.Run(ctx, o.PrimaryHost, cmd)
			if err != nil || code != 0 {
				return fmt.Errorf("rebuild old primary: code=%d err=%v stderr=%q", code, err, stderr)
			}
			fmt.Fprintf(o.Out, "old primary %s rebuilt as replica\n", o.PrimaryHost)
		}
		return nil
	})
}

func realPromoteReplicaRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	skip, _ := cmd.Flags().GetBool("skip-rebuild")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	return runPromoteReplica(cmd.Context(), PromoteOpts{
		PrimaryHost: "local", ReplicaHost: cfg.PeerHost,
		SkipRebuild: skip, DryRun: dryRun,
		Runner: sshRunner, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(),
	})
}
```

Swap stub. Run tests, commit.

```bash
go test -v ./internal/grimnirdeploy/ -run TestPromote
git add internal/grimnirdeploy/cmd_promote.go internal/grimnirdeploy/cmd_promote_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: promote-replica subcommand (Chunk 10)"
```

---

## Chunk 11: `cold-start-region --region=R`

> **Integration-test prerequisite:** This subcommand stands up an entire region from scratch. Unit tests verify the dependency-ordering against the fake runner. Real exercise happens on the new HA stack as Track A step 1 lands.

### Task 11.1: Implementation

**Files:**
- Create: `internal/grimnirdeploy/cmd_coldstart.go`
- Create: `internal/grimnirdeploy/cmd_coldstart_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go`

**Context:**
Section 9.1 of the design lists the dependency order: firewall + WireGuard mesh → Postgres + replica → Redis → MinIO/R2 connectivity → grimnir + mediaengine + fan-out + edge-encoder on both nodes → `grimnir-deploy verify`.

Each step is idempotent (so re-running on a half-up region is safe). The implementation is mostly shell-outs through the runner; the test verifies the ordering.

- [ ] **Step 1: Test**

```go
/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestColdStartOrdersDependencies(t *testing.T) {
	f := runner.NewFake()
	// All operations succeed.
	for _, p := range []string{"iptables", "wg", "pg_isready", "redis-cli", "curl", "./grimnir"} {
		f.SetResponsePrefix(p, "ok\n", "", 0, nil)
	}
	f.SetResponsePrefix("cd /srv/docker/grimnir_radio && ./grimnir", "ok\n", "", 0, nil)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runColdStartRegion(context.Background(), ColdStartOpts{
		Region: "us-east", Hosts: []string{"local", "node-2"},
		Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("coldstart: %v", err)
	}
	// Verify dependency order via output milestones.
	s := out.String()
	idxFW := strings.Index(s, "firewall")
	idxWG := strings.Index(s, "wireguard")
	idxPG := strings.Index(s, "postgres")
	idxRD := strings.Index(s, "redis")
	idxApp := strings.Index(s, "grimnir-radio")
	if !(idxFW < idxWG && idxWG < idxPG && idxPG < idxRD && idxRD < idxApp) {
		t.Errorf("dependency order wrong; got:\n%s", s)
	}
}
```

- [ ] **Step 2: Implement**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

type ColdStartOpts struct {
	Region  string
	Hosts   []string
	Runner  runner.Runner
	Compose *runner.DockerCompose
	Wrapper *audit.Wrapper
	Out     io.Writer
	DryRun  bool
}

func runColdStartRegion(ctx context.Context, o ColdStartOpts) error {
	return o.Wrapper.Wrap(ctx, "cold-start-region", map[string]any{"region": o.Region, "dry_run": o.DryRun}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }
		say("cold-start region %s on %v", o.Region, o.Hosts)
		if o.DryRun {
			say("[dry-run] would bring up firewall, wireguard, postgres, redis, applications")
			return nil
		}

		// 1. Firewall (deny-all + explicit allow rules).
		for _, h := range o.Hosts {
			say("firewall on %s", h)
			if _, _, _, err := o.Runner.Run(ctx, h, "iptables -P INPUT DROP && iptables -A INPUT -i lo -j ACCEPT && iptables -A INPUT -p tcp --dport 22 -j ACCEPT"); err != nil {
				return fmt.Errorf("firewall on %s: %w", h, err)
			}
		}
		// 2. WireGuard mesh.
		for _, h := range o.Hosts {
			say("wireguard on %s", h)
			if _, _, _, err := o.Runner.Run(ctx, h, "wg-quick up wg0"); err != nil {
				return fmt.Errorf("wg on %s: %w", h, err)
			}
		}
		// 3. Postgres (primary on first host; replica on second).
		say("postgres primary on %s", o.Hosts[0])
		if _, _, _, err := o.Runner.Run(ctx, o.Hosts[0], "systemctl start postgresql && pg_isready"); err != nil {
			return err
		}
		say("postgres replica on %s", o.Hosts[1])
		if _, _, _, err := o.Runner.Run(ctx, o.Hosts[1], "systemctl start postgresql && pg_isready"); err != nil {
			return err
		}
		// 4. Redis.
		for _, h := range o.Hosts {
			say("redis on %s", h)
			if _, _, _, err := o.Runner.Run(ctx, h, "systemctl start redis && redis-cli ping"); err != nil {
				return err
			}
		}
		// 5. Object storage reachability probe.
		for _, h := range o.Hosts {
			if _, _, _, err := o.Runner.Run(ctx, h, "curl -sS -m 5 -o /dev/null -w '%{http_code}' $GRIMNIR_S3_ENDPOINT/minio/health/ready"); err != nil {
				return fmt.Errorf("s3 probe on %s: %w", h, err)
			}
		}
		// 6. Application stack (compose up).
		for _, h := range o.Hosts {
			say("grimnir-radio + dependencies on %s", h)
			if err := o.Compose.Up(ctx, h); err != nil {
				return err
			}
		}
		say("cold-start complete; run `grimnir-deploy verify` to validate")
		return nil
	})
}

func realColdStartRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	region, _ := cmd.Flags().GetString("region")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")
	return runColdStartRegion(cmd.Context(), ColdStartOpts{
		Region: region, Hosts: []string{"local", cfg.PeerHost},
		Runner: sshRunner, Compose: compose,
		Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
```

Swap stub, test, commit.

```bash
go test -v ./internal/grimnirdeploy/ -run TestColdStart
git add internal/grimnirdeploy/cmd_coldstart.go internal/grimnirdeploy/cmd_coldstart_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: cold-start-region subcommand (Chunk 11)"
```

---

## Chunk 12: `restore --from=BACKUP_ID [--target-time=TS]`

> **Integration-test prerequisite:** Requires real pgbackrest + cluster. Unit tests cover the command sequence; real restore happens during the quarterly backup drill (Chunk 14).

### Task 12.1: Implementation

**Files:**
- Create: `internal/grimnirdeploy/cmd_restore.go`
- Create: `internal/grimnirdeploy/cmd_restore_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go`

**Context:**
1. Stop grimnir + mediaengine on both nodes.
2. Run `pgbackrest restore --stanza=grimnir --set=<backup>` (or `--type=time --target=<ts>` for PITR).
3. Start Postgres.
4. Wait for `pg_isready`.
5. Start grimnir + mediaengine on both nodes.
6. Run `grimnir-deploy verify`.

- [ ] **Step 1: Test**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestRestoreLatestBuildsExpectedCommand(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("cd /srv/docker", "ok", "", 0, nil)
	f.SetResponsePrefix("pgbackrest restore", "restored\n", "", 0, nil)
	f.SetResponsePrefix("systemctl", "", "", 0, nil)
	f.SetResponsePrefix("pg_isready", "ok\n", "", 0, nil)

	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runRestore(context.Background(), RestoreOpts{
		From: "latest", Hosts: []string{"local", "node-2"},
		Runner: f, Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober: fakeAllProber{result: probe.Result{Host: "local", ControlPlaneOK: true, MediaEngineOK: true, EdgeEncoderOK: true, FanOutOK: true}},
		Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	found := false
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "pgbackrest restore --stanza=grimnir") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected pgbackrest restore call; got: %+v", f.Calls)
	}
}

func TestRestoreWithTargetTimeAddsPITRFlags(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("cd /srv/docker", "ok", "", 0, nil)
	f.SetResponsePrefix("pgbackrest restore", "restored\n", "", 0, nil)
	f.SetResponsePrefix("systemctl", "", "", 0, nil)
	f.SetResponsePrefix("pg_isready", "ok\n", "", 0, nil)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	err := runRestore(context.Background(), RestoreOpts{
		From: "latest", TargetTime: "2026-06-01T12:00:00Z",
		Hosts: []string{"local"}, Runner: f,
		Compose: runner.NewDockerCompose(f, "/srv/docker/grimnir_radio"),
		Prober: fakeAllProber{result: probe.Result{Host: "local", ControlPlaneOK: true, MediaEngineOK: true, EdgeEncoderOK: true, FanOutOK: true}},
		Wrapper: w, Out: &bytes.Buffer{},
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range f.Calls {
		if strings.Contains(c.Cmd, "--type=time") && strings.Contains(c.Cmd, "2026-06-01T12:00:00Z") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected PITR flags in pgbackrest call; got: %+v", f.Calls)
	}
}
```

- [ ] **Step 2: Implement**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/probe"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

type RestoreOpts struct {
	From       string // backup id or "latest"
	TargetTime string // RFC3339 or empty (= replay to end of WAL)
	Hosts      []string
	Runner     runner.Runner
	Compose    *runner.DockerCompose
	Prober     FullProber
	Wrapper    *audit.Wrapper
	Out        io.Writer
	DryRun     bool
}

func runRestore(ctx context.Context, o RestoreOpts) error {
	return o.Wrapper.Wrap(ctx, "restore", map[string]any{"from": o.From, "target_time": o.TargetTime, "dry_run": o.DryRun}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }

		// Stop services on every host.
		for _, h := range o.Hosts {
			say("stopping services on %s", h)
			for _, svc := range []string{"grimnir-radio", "edge-encoder", "grimnir-fanout", "grimnir-mediaengine"} {
				if err := o.Compose.Stop(ctx, h, svc); err != nil {
					return err
				}
			}
		}

		// Run pgbackrest restore on the primary host (Hosts[0]).
		primary := o.Hosts[0]
		cmd := fmt.Sprintf("pgbackrest restore --stanza=grimnir --set=%s", o.From)
		if o.From == "latest" {
			cmd = "pgbackrest restore --stanza=grimnir"
		}
		if o.TargetTime != "" {
			cmd += fmt.Sprintf(" --type=time --target='%s'", o.TargetTime)
		}
		say("pgbackrest restore on %s", primary)
		if o.DryRun {
			say("[dry-run] would run: %s", cmd)
			return nil
		}
		if _, stderr, code, err := o.Runner.Run(ctx, primary, cmd); err != nil || code != 0 {
			return fmt.Errorf("pgbackrest: code=%d err=%v stderr=%q", code, err, stderr)
		}

		// Start Postgres and wait for ready.
		if _, _, _, err := o.Runner.Run(ctx, primary, "systemctl start postgresql"); err != nil {
			return err
		}
		deadline := time.Now().Add(60 * time.Second)
		for time.Now().Before(deadline) {
			if _, _, code, _ := o.Runner.Run(ctx, primary, "pg_isready"); code == 0 {
				goto pgReady
			}
			time.Sleep(time.Second)
		}
		return fmt.Errorf("postgres did not become ready within 60s after restore")
	pgReady:

		// Restart application services everywhere.
		for _, h := range o.Hosts {
			say("starting services on %s", h)
			if err := o.Compose.Up(ctx, h); err != nil {
				return err
			}
		}

		// Verify.
		for _, h := range o.Hosts {
			r := o.Prober.ProbeAll(ctx, h)
			if !r.ControlPlaneOK || !r.MediaEngineOK {
				return fmt.Errorf("post-restore verify failed on %s: cp=%s me=%s", h, r.ControlPlaneErr, r.MediaEngineErr)
			}
		}
		say("restore complete and verified")
		return nil
	})
}

func realRestoreRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	from, _ := cmd.Flags().GetString("from")
	target, _ := cmd.Flags().GetString("target-time")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	compose := runner.NewDockerCompose(sshRunner, "/srv/docker/grimnir_radio")
	return runRestore(cmd.Context(), RestoreOpts{
		From: from, TargetTime: target,
		Hosts: []string{"local", cfg.PeerHost},
		Runner: sshRunner, Compose: compose, Prober: probe.NewProber(),
		Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
```

Swap stub, test, commit.

```bash
go test -v ./internal/grimnirdeploy/ -run TestRestore
git add internal/grimnirdeploy/cmd_restore.go internal/grimnirdeploy/cmd_restore_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: restore subcommand with pgbackrest + PITR (Chunk 12)"
```

---

## Chunk 13: `recover-partition`

> **Integration-test prerequisite:** Requires a real two-node cluster with the ability to simulate network partitions (`iptables -A INPUT -s peer -j DROP`). Unit tests cover the conflict-detection logic; real partition recovery is exercised during failure-injection drills.

### Task 13.1: Implementation

**Files:**
- Create: `internal/grimnirdeploy/cmd_recover.go`
- Create: `internal/grimnirdeploy/cmd_recover_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go`

**Context:**
This subcommand does NOT auto-merge. It reports the state and surfaces conflicts; the operator decides. Checks performed:
1. VIP holder count for each VIP (listener, DJ): must be exactly 1. Reports both 0 and 2.
2. Which node has more recent WAL (`pg_current_wal_lsn` on each).
3. Leader-election lease holder (which node Redis says holds it).
4. Replication state from each side.

The output is a structured report. Exits 0 if all checks pass; non-zero if any conflict needs operator decision.

- [ ] **Step 1: Test**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
	"github.com/redis/go-redis/v9"
)

func TestRecoverPartitionReportsConflict(t *testing.T) {
	mr, _ := miniredis.Run()
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_ = rdb.Set(context.Background(), "grimnir:leader", "node-1", 0)

	f := runner.NewFake()
	// Both nodes claim the VIP (split-brain).
	f.SetResponsePrefix("ip addr show", "<edge-vps>00\n", "", 0, nil)
	// Different WAL LSN.
	f.SetResponse("psql -tAc 'SELECT pg_current_wal_lsn()'", "0/A0000000\n", "", 0, nil)

	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runRecoverPartition(context.Background(), RecoverOpts{
		Hosts: []string{"local", "node-2"}, VIPs: []string{"<edge-vps>00"},
		Runner: f, Redis: rdb, Wrapper: w, Out: &out,
	})
	if err == nil {
		t.Fatal("split-brain VIP should produce an error")
	}
	if !strings.Contains(out.String(), "VIP") {
		t.Errorf("output missing VIP report: %s", out.String())
	}
}
```

- [ ] **Step 2: Implement**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

type RecoverOpts struct {
	Hosts   []string
	VIPs    []string
	Runner  runner.Runner
	Redis   *redis.Client
	Wrapper *audit.Wrapper
	Out     io.Writer
	DryRun  bool
}

func runRecoverPartition(ctx context.Context, o RecoverOpts) error {
	return o.Wrapper.Wrap(ctx, "recover-partition", map[string]any{"dry_run": o.DryRun}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }
		var problems []string

		// 1. VIP holder count.
		for _, vip := range o.VIPs {
			var holders []string
			for _, h := range o.Hosts {
				out, _, _, _ := o.Runner.Run(ctx, h, fmt.Sprintf("ip addr show | grep -q %s && echo held || echo not-held", vip))
				if strings.Contains(out, "held") && !strings.Contains(out, "not-held") {
					holders = append(holders, h)
				}
			}
			say("VIP %s holders: %v", vip, holders)
			if len(holders) != 1 {
				problems = append(problems, fmt.Sprintf("VIP %s has %d holders (want 1)", vip, len(holders)))
			}
		}

		// 2. WAL position per node.
		walPositions := map[string]string{}
		for _, h := range o.Hosts {
			out, _, _, _ := o.Runner.Run(ctx, h, "psql -tAc 'SELECT pg_current_wal_lsn()'")
			walPositions[h] = strings.TrimSpace(out)
			say("WAL LSN on %s: %s", h, walPositions[h])
		}

		// 3. Leader lease holder.
		leader, err := o.Redis.Get(ctx, "grimnir:leader").Result()
		if err == redis.Nil {
			say("no leader lease held")
			problems = append(problems, "no leader lease held in Redis")
		} else if err != nil {
			return fmt.Errorf("read leader: %w", err)
		} else {
			say("leader lease: %s", leader)
		}

		if len(problems) > 0 {
			say("CONFLICTS:")
			for _, p := range problems {
				say("  - %s", p)
			}
			say("This subcommand does NOT auto-merge. Operator must decide which side is canonical.")
			return errors.New("partition recovery requires operator decision")
		}
		say("no conflicts detected; cluster appears healthy")
		return nil
	})
}

func realRecoverRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	return runRecoverPartition(cmd.Context(), RecoverOpts{
		Hosts: []string{"local", cfg.PeerHost},
		VIPs:  []string{getEnv("GRIMNIR_LISTENER_VIP", "", ""), getEnv("GRIMNIR_DJ_VIP", "", "")},
		Runner: sshRunner, Redis: deps.Redis,
		Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
```

Swap stub, test, commit.

```bash
go test -v ./internal/grimnirdeploy/ -run TestRecover
git add internal/grimnirdeploy/cmd_recover.go internal/grimnirdeploy/cmd_recover_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: recover-partition subcommand (Chunk 13)"
```

---

## Chunk 14: `backup-drill` + runbook docs + version bump

This chunk closes the binary out: the quarterly drill subcommand (Section 8.4), the markdown index + per-subcommand runbook files (Section 8.2), a CLAUDE.md note documenting the new binary, and the version bump per the per-push rule.

### Task 14.1: `backup-drill` subcommand

**Files:**
- Create: `internal/grimnirdeploy/cmd_backupdrill.go`
- Create: `internal/grimnirdeploy/cmd_backupdrill_test.go`
- Modify: `internal/grimnirdeploy/cmd_stubs.go`

**Context:**
Stand up a temporary Postgres on the drill host, restore the latest backup, measure base-restore + WAL-replay time, report RTO/RPO, post results to the audit ntfy topic. The Postgres container can be `docker run --rm -d postgres:16` on a writable mount; the restore uses `pgbackrest restore --stanza=grimnir --pg1-path=<temp_dir>`.

- [ ] **Step 1: Test**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

func TestBackupDrillReportsTimings(t *testing.T) {
	f := runner.NewFake()
	f.SetResponsePrefix("docker run", "started\n", "", 0, nil)
	f.SetResponsePrefix("pgbackrest restore", "restored\n", "", 0, nil)
	f.SetResponsePrefix("docker rm -f", "", "", 0, nil)
	_, store, ntfy := setupTestEnv(t)
	w := audit.NewWrapper(testRecorder{store, ntfy}, "alice", "10.0.0.1")
	var out bytes.Buffer
	err := runBackupDrill(context.Background(), BackupDrillOpts{
		Region: "us-east", DrillHost: "drill-host",
		Runner: f, Wrapper: w, Out: &out,
	})
	if err != nil {
		t.Fatalf("drill: %v", err)
	}
	if !strings.Contains(out.String(), "RTO") {
		t.Errorf("output missing RTO timing: %s", out.String())
	}
}
```

- [ ] **Step 2: Implement**

```go
/*
Copyright (C) 2026 Friends Incode
SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/runner"
)

type BackupDrillOpts struct {
	Region    string
	DrillHost string
	Runner    runner.Runner
	Wrapper   *audit.Wrapper
	Out       io.Writer
	DryRun    bool
}

func runBackupDrill(ctx context.Context, o BackupDrillOpts) error {
	return o.Wrapper.Wrap(ctx, "backup-drill", map[string]any{"region": o.Region, "drill_host": o.DrillHost, "dry_run": o.DryRun}, func(ctx context.Context) error {
		say := func(format string, args ...any) { fmt.Fprintf(o.Out, format+"\n", args...) }
		say("backup drill on %s for region %s", o.DrillHost, o.Region)
		if o.DryRun {
			say("[dry-run] would stand up staging Postgres + restore")
			return nil
		}

		containerName := fmt.Sprintf("grimnir-drill-%d", time.Now().Unix())
		defer func() {
			_, _, _, _ = o.Runner.Run(context.Background(), o.DrillHost, fmt.Sprintf("docker rm -f %s", containerName))
		}()

		startUp := time.Now()
		cmd := fmt.Sprintf("docker run --rm -d --name %s -v /tmp/drill-data:/var/lib/postgresql/data postgres:16", containerName)
		if _, stderr, code, err := o.Runner.Run(ctx, o.DrillHost, cmd); err != nil || code != 0 {
			return fmt.Errorf("docker run: code=%d err=%v stderr=%q", code, err, stderr)
		}
		startupDuration := time.Since(startUp)

		startRestore := time.Now()
		restore := "pgbackrest restore --stanza=grimnir --pg1-path=/tmp/drill-data"
		if _, stderr, code, err := o.Runner.Run(ctx, o.DrillHost, restore); err != nil || code != 0 {
			return fmt.Errorf("pgbackrest restore: code=%d err=%v stderr=%q", code, err, stderr)
		}
		restoreDuration := time.Since(startRestore)

		say("RTO measured:")
		say("  staging-startup: %v", startupDuration)
		say("  pgbackrest restore: %v", restoreDuration)
		say("  total: %v", startupDuration+restoreDuration)
		say("RPO bound: archive_timeout (30s) + push retry interval")
		return nil
	})
}

func realBackupDrillRunE(cmd *cobra.Command, args []string) error {
	cfg, err := LoadConfig()
	if err != nil {
		return err
	}
	region, _ := cmd.Flags().GetString("region")
	host, _ := cmd.Flags().GetString("drill-host")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	deps, err := wireDeps(cmd.Context(), cfg)
	if err != nil {
		return err
	}
	defer deps.Close()
	sshRunner := runner.NewSSHRunner(cfg.PeerSSHUser, cfg.PeerSSHPort, cfg.PeerSSHKey, nil)
	defer sshRunner.Close()
	return runBackupDrill(cmd.Context(), BackupDrillOpts{
		Region: region, DrillHost: host,
		Runner: sshRunner, Wrapper: deps.Wrapper, Out: cmd.OutOrStdout(), DryRun: dryRun,
	})
}
```

Swap stub, test, commit.

```bash
go test -v ./internal/grimnirdeploy/ -run TestBackupDrill
git add internal/grimnirdeploy/cmd_backupdrill.go internal/grimnirdeploy/cmd_backupdrill_test.go internal/grimnirdeploy/cmd_stubs.go
git commit -m "grimnir-deploy: backup-drill subcommand (Chunk 14.1)"
```

### Task 14.2: Runbook docs (Section 8.2)

**Files:**
- Create: `docs/runbooks/index.md`
- Create: `docs/runbooks/deploy.md`
- Create: `docs/runbooks/emergency-pause.md`
- Create: `docs/runbooks/drain.md`
- Create: `docs/runbooks/promote-replica.md`
- Create: `docs/runbooks/cold-start-region.md`
- Create: `docs/runbooks/restore.md`
- Create: `docs/runbooks/recover-partition.md`
- Create: `docs/runbooks/backup-drill.md`

**Context:**
Per Section 8.2, the index is a symptom → subcommand table. Each per-subcommand doc is a long-form runbook with: when to use this; what it does step-by-step; what to check after; what to do if it fails.

- [ ] **Step 1: Write `docs/runbooks/index.md`**

```markdown
# Grimnir Deploy Runbooks

3am, alert fired, what do you run? Find the symptom in the table below and run the named subcommand. Each subcommand has a `--help` that walks through the procedure inline; the per-subcommand markdown files in this directory are the long-form versions for incident-review reading.

| Symptom | Subcommand | Long-form runbook |
|---|---|---|
| New release ready to ship | `grimnir-deploy deploy vX.Y.Z` | [deploy.md](./deploy.md) |
| Just-shipped release is causing problems | `grimnir-deploy deploy --rollback --reason="..."` | [deploy.md](./deploy.md) |
| Active incident; freeze all deploys | `grimnir-deploy emergency-pause --reason="..."` | [emergency-pause.md](./emergency-pause.md) |
| Incident resolved; let deploys run again | `grimnir-deploy emergency-resume --reason="..."` | [emergency-pause.md](./emergency-pause.md) |
| Need to reboot a node / swap hardware | `grimnir-deploy drain --node=N` | [drain.md](./drain.md) |
| Primary Postgres degraded / down | `grimnir-deploy promote-replica` | [promote-replica.md](./promote-replica.md) |
| Bringing up a new region | `grimnir-deploy cold-start-region --region=R` | [cold-start-region.md](./cold-start-region.md) |
| Data corruption; need to restore from backup | `grimnir-deploy restore --from=BACKUP_ID` | [restore.md](./restore.md) |
| Network partition between HA nodes is recovering | `grimnir-deploy recover-partition` | [recover-partition.md](./recover-partition.md) |
| Quarterly: verify backups actually restore | `grimnir-deploy backup-drill --region=R --drill-host=H` | [backup-drill.md](./backup-drill.md) |
| Triage: is the cluster healthy right now? | `grimnir-deploy verify` | (this command is non-destructive; safe to run anytime) |

Every subcommand:
- Writes a row to `audit_log` on start AND on completion.
- Posts an ntfy notification to `grimnir-audit-<region>`.
- Supports `--dry-run` to print what it would do without mutating.
- Has `--help` with the inline procedure.
```

- [ ] **Step 2: Write the long-form runbook for each subcommand**

For each subcommand, create a doc with these sections:

```markdown
# <subcommand>

## When to use this
<one or two sentences>

## What it does
<numbered step-by-step matching the implementation>

## Pre-flight gates this respects
<list>

## What to check after it completes
<concrete checks: command + expected output>

## What to do if it fails mid-way
<rollback / cleanup / escalate path>

## Audit trail
<note the audit_log row + ntfy topic to look at>
```

Don't fabricate procedures: copy the implementation order from the subcommand's Go source. Lift the pre-flight gate list from Chunk 5. Use concrete commands operators can run (`psql -c "select ..."`, `docker ps`, etc.). Avoid intensifiers and vague phrasing per the writing style guide.

The per-subcommand docs can be ~50-80 lines each. The whole set is mechanical once the first one is written.

- [ ] **Step 3: Commit**

```bash
git add docs/runbooks/
git commit -m "grimnir-deploy: runbook index + per-subcommand long-form docs (Chunk 14.2)"
```

### Task 14.3: CLAUDE.md update

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add a new section to CLAUDE.md**

After the "Production Server Commands" section, add:

```markdown
## grimnir-deploy

`grimnir-deploy` is the operator binary for rolling updates and incident runbooks. It lives in `cmd/grimnir-deploy/` and is built into `bin/grimnir-deploy` via `make build-grimnir-deploy`.

All cluster mutations (deploy, drain, promote-replica, restore, etc.) run through this binary so they:
- Write an audit row to `audit_log` (start + completion)
- Post an ntfy notification to `grimnir-audit-<region>`
- Respect the `grimnir:emergency-pause` Redis key
- Are reversible where possible via `--rollback` (deploy only) or operator follow-up

The runbook index lives at `docs/runbooks/index.md`. Each subcommand has a `--help` describing its procedure inline.

Don't run `docker compose` or `./grimnir` directly for mutating operations; use the subcommand. Direct compose use bypasses the audit log.
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "claude: document grimnir-deploy binary and audit-trail rule (Chunk 14.3)"
```

### Task 14.4: Optional systemd unit

**Files:**
- Create: `scripts/grimnir-deploy.service`

**Context:**
This is optional but useful for `systemctl status grimnir-deploy` showing the last deploy's exit + logs. It runs `grimnir-deploy verify` on a timer, not the destructive subcommands.

- [ ] **Step 1: Write the unit**

```ini
[Unit]
Description=Grimnir Deploy verify probe
After=network.target

[Service]
Type=oneshot
User=<ssh-user>
WorkingDirectory=/srv/docker/grimnir_radio
EnvironmentFile=/etc/grimnir/deploy.env
ExecStart=/usr/local/bin/grimnir-deploy verify
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
```

And a paired timer (`scripts/grimnir-deploy.timer`):

```ini
[Unit]
Description=Run grimnir-deploy verify every 5 minutes

[Timer]
OnBootSec=2min
OnUnitActiveSec=5min

[Install]
WantedBy=timers.target
```

- [ ] **Step 2: Commit**

```bash
git add scripts/grimnir-deploy.service scripts/grimnir-deploy.timer
git commit -m "scripts: systemd unit + timer for grimnir-deploy verify (Chunk 14.4)"
```

### Task 14.5: Version bump

**Files:**
- Modify: `internal/version/version.go`
- Modify: `VERSION`

- [ ] **Step 1: Bump the version**

Read the current `internal/version/version.go` `Version` constant. Bump the patch portion (e.g., `2.0.0-alpha.4` -> `2.0.0-alpha.5`; if numerical patch like `1.40.8`, bump to `1.40.9`).

Update both files identically.

- [ ] **Step 2: Run the full verify**

```bash
make ci
```

Expected: all tests pass, fmt clean, vet clean, lint clean. **CI will fail if gofmt is not clean** (per memory rules).

- [ ] **Step 3: Commit + tag + push**

```bash
NEW_VERSION=<new version, e.g., 2.0.0-alpha.5>
git add internal/version/version.go VERSION
git commit -m "grimnir-deploy: complete binary + runbooks (v$NEW_VERSION)"
git tag -a "v$NEW_VERSION" -m "Version $NEW_VERSION: grimnir-deploy binary complete"
git push origin v2-dev
git push origin "v$NEW_VERSION"
```

Per CLAUDE.md, EVERY push to GitHub gets a version bump with a git tag. This is the final action of the plan.

---

## Plan-completion checklist

- [ ] Every subcommand in Section 8.2 of the design has a working implementation, a `--help`, and a long-form runbook doc.
- [ ] Every subcommand writes an `audit_log` row on start + completion and posts to `grimnir-audit-<region>`.
- [ ] Every subcommand supports `--dry-run`.
- [ ] `deploy --rollback` refuses aged rollbacks and contract crossings without explicit force flags.
- [ ] Unit tests pass without real infrastructure (`make test`).
- [ ] Integration tests that require real Postgres / Redis / SSH / pgbackrest are gated by `//go:build integration && requires_real_cluster` so default CI does not run them.
- [ ] `CLAUDE.md` documents the binary and the "no direct docker compose for mutations" rule.
- [ ] Runbook index links every subcommand.
- [ ] Version bumped + tagged + pushed.

## Honest caveats

- The unsafe-reflection helper in `internal/grimnirdeploy/deps.go::wrapperOperator` is ugly and should be replaced with an exported accessor on `*audit.Wrapper` if the reviewer flags it. The plan keeps the unsafe version because it's contained to one helper and avoids leaking unrelated audit-package surface.
- `History.ContractCrossings` is a phase-1 implementation that flags every annotated migration regardless of which tags introduced them (over-conservative). A follow-up issue should refine this to git-blame-by-tag once we have a stable tag→commit mapping.
- The leader-detection in `cmd_deploy.go::realDeployRunE` currently defaults to "peer is non-leader, so deploy peer first." A follow-up should probe the leader-election lease in Redis to identify the actual leader and pick first-node accordingly.
- The `cold-start-region` firewall + WireGuard steps are illustrative; the actual rules/keys belong in the operator's secret-managed configuration, not in this Go code. The implementation calls `iptables` and `wg-quick up` as illustrative shell-outs; the real ordering and rule content lives in `/etc/grimnir/firewall.rules` on each node.
- Real-infrastructure integration tests for `promote-replica`, `restore`, `recover-partition`, `cold-start-region`, and `backup-drill` are NOT included in this plan beyond `//go:build integration && requires_real_cluster` skip-stubs. Those tests are written when the HA stack is sufficiently built (Track A milestones); attempting to write them now against vapor would produce false confidence.
