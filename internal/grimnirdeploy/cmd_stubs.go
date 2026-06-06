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
		RunE:  realDeployRunE,
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
		RunE:  realVerifyRunE,
	}
}

func newDrainCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "drain",
		Short: "Drain a node: drop VRRP priority, stop services, hand off leadership",
		Long:  "Drains the named node so the peer takes over. Drops VRRP priority via vrrp_script failure file, SIGTERMs grimnir / edge-encoder / fan-out / mediaengine in that order, waits for leader-election lease to migrate to the peer. See docs/runbooks/drain.md.",
		RunE:  realDrainRunE,
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
		Long:  "Sets the grimnir-deploy:emergency-pause:<region> Redis key. Every grimnir-deploy subcommand that mutates the cluster reads this key first and aborts if set; the grimnirradio scheduler reads the same key before any auto-deploy gate. Use during an incident to prevent any automated or manual deploys from running. Cleared with `grimnir-deploy emergency-resume`. See docs/runbooks/emergency-pause.md.",
		RunE:  realEmergencyPauseRunE,
	}
	c.Flags().String("reason", "", "free-form reason recorded in audit_log.notes and the Redis payload (required)")
	c.Flags().String("region", "", "region to pause (defaults to $GRIMNIR_REGION or \"default\")")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate Redis")
	c.Flags().Duration("ttl", 0, "TTL on the pause key; 0 means sticky (manual emergency-resume required)")
	_ = c.MarkFlagRequired("reason")
	return c
}

func newEmergencyResumeCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "emergency-resume",
		Short: "Clear the Redis emergency-pause key; deploys resume per region policy",
		RunE:  realEmergencyResumeRunE,
	}
	c.Flags().String("reason", "", "free-form reason recorded in audit_log.notes (required)")
	c.Flags().String("region", "", "region to resume (defaults to $GRIMNIR_REGION or \"default\")")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate Redis")
	_ = c.MarkFlagRequired("reason")
	return c
}

func newPromoteReplicaCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "promote-replica",
		Short: "Promote the Postgres replica to primary; repoint pgbouncer; rebuild new replica",
		Long:  "Postgres failover runbook. Verifies the replica is in streaming state and lag < 5s, promotes via pg_ctl promote, updates pgbouncer's primary_conninfo to point at the new primary, demotes the old primary to replica via pg_basebackup. See docs/runbooks/promote-replica.md.",
		RunE:  realPromoteReplicaRunE,
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
		RunE:  realColdStartRunE,
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
		RunE:  realRestoreRunE,
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
		RunE:  realRecoverRunE,
	}
	c.Flags().Bool("dry-run", false, "print the planned actions; do not mutate the cluster")
	return c
}

func newBackupDrillCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "backup-drill",
		Short: "Run a backup-restore drill against a staging Postgres on a non-production host",
		Long:  "Stands up a temporary Postgres on the named drill host, restores the latest backup, measures base-restore + WAL-replay time, reports measured RTO + RPO. Posts results to the audit ntfy topic. Quarterly cadence per design Section 8.4. See docs/runbooks/backup-drill.md.",
		RunE:  realBackupDrillRunE,
	}
	c.Flags().String("region", "", "region whose backup repository to drill (required)")
	c.Flags().String("drill-host", "", "host to stand up the staging Postgres on (required)")
	c.Flags().Bool("dry-run", false, "print the planned actions; do not run the drill")
	_ = c.MarkFlagRequired("region")
	_ = c.MarkFlagRequired("drill-host")
	return c
}
