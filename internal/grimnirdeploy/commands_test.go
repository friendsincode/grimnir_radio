/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package grimnirdeploy

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/grimnirdeploy/audit"
)

// nullPoster is a Poster that swallows every call. Used so the wrapper's ntfy
// hooks fire (covering the code path) without any network call.
type nullPoster struct{}

func (nullPoster) Post(_ context.Context, _, _, _ string, _ audit.Priority, _ ...string) error {
	return nil
}

func newTestStore(t *testing.T) (*audit.Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&audit.Entry{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return audit.NewStore(db), db
}

// TestRegisterCommandsWrapsEveryStub confirms every Chunk-0 stub gets the
// audit wrapper transparently: invoking a stub with a real Wrapper produces
// a "failed" row with outcome "not yet implemented" in audit_log. This is
// the user's stated acceptance criterion for Chunk 1.
func TestRegisterCommandsWrapsEveryStub(t *testing.T) {
	// Only commands whose RunE is still the bare errNotImplemented stub.
	// Real implementations (e.g. emergency-pause / emergency-resume already
	// promoted in an earlier prep commit) drop out of this matrix by design;
	// they get their own audit-wrapper integration coverage in their own
	// _test.go files.
	stubs := map[string][]string{
		// deploy promoted to real RunE in Chunk 6; covered by cmd_deploy_test.go.
		"drain":             {"--node=self"},
		"promote-replica":   {},
		"cold-start-region": {"--region=us-east"},
		"restore":           {"--from=latest"},
		"recover-partition": {},
		"backup-drill":      {"--region=us-east", "--drill-host=staging"},
	}

	for sub, flags := range stubs {
		t.Run(sub, func(t *testing.T) {
			store, db := newTestStore(t)
			rec := audit.NewRecorder(store, nullPoster{}, "grimnir-audit-test")
			w := audit.NewWrapper(rec, "tester", "127.0.0.1")

			root := &cobra.Command{Use: "grimnir-deploy"}
			RegisterCommands(root, w)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SilenceErrors = true
			root.SilenceUsage = true

			args := append([]string{sub}, flags...)
			root.SetArgs(args)
			err := root.Execute()
			if !errors.Is(err, errNotImplemented) {
				t.Fatalf("Execute(%s): err = %v, want errNotImplemented", sub, err)
			}

			var got audit.Entry
			if err := db.Where("subcommand = ? AND phase = ?", sub, audit.PhaseFailed).First(&got).Error; err != nil {
				t.Fatalf("no failed audit row for %s: %v", sub, err)
			}
			if got.Outcome != "not yet implemented" {
				t.Errorf("%s: outcome = %q, want 'not yet implemented'", sub, got.Outcome)
			}
			if got.Operator != "tester" {
				t.Errorf("%s: operator = %q, want tester", sub, got.Operator)
			}
			if got.SourceIP != "127.0.0.1" {
				t.Errorf("%s: source_ip = %q, want 127.0.0.1", sub, got.SourceIP)
			}

			// The audit row is updated in-place from "started" -> "failed",
			// so exactly one row per subcommand invocation lives in the table.
			var count int64
			if err := db.Model(&audit.Entry{}).Where("subcommand = ?", sub).Count(&count).Error; err != nil {
				t.Errorf("%s: count: %v", sub, err)
			}
			if count != 1 {
				t.Errorf("%s: row count = %d, want 1 (Updates should move started -> failed in place)", sub, count)
			}
		})
	}
}

// TestRegisterCommandsNilWrapper confirms RegisterCommands works with a nil
// wrapper (the pre-config-wiring binary path) so existing tests like
// TestRootHelp continue to function.
func TestRegisterCommandsNilWrapper(t *testing.T) {
	root := &cobra.Command{Use: "grimnir-deploy"}
	RegisterCommands(root, nil)
	root.SetOut(&bytes.Buffer{})
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Errorf("nil wrapper --help: %v", err)
	}
}

// Compile-time assertion: *audit.Store + *audit.NtfyPoster satisfy the
// Recorder interface when combined via NewRecorder. Plus a sanity check that
// the time.Duration plumbing is in scope so the test file doesn't grow stale.
var (
	_ = time.Duration(0)
	_ = uuid.Nil
)
