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
	f.SetResponsePrefix("psql -tAc 'SELECT status FROM", "streaming\n", "", 0, nil)
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
	f.SetResponsePrefix("psql -tAc 'SELECT status FROM", "streaming\n", "", 0, nil)
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
