//go:build linux

package playout

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/config"
	"github.com/rs/zerolog"
)

func TestPipelineLifecycle_CancellationReapsWorker(t *testing.T) {
	starts := map[string]func(*Pipeline, context.Context) error{
		"stdout": func(p *Pipeline, ctx context.Context) error {
			return p.StartWithOutput(ctx, "", func(r io.Reader) { _, _ = io.Copy(io.Discard, r) })
		},
		"dual": func(p *Pipeline, ctx context.Context) error {
			return p.StartWithDualOutput(ctx, "", nil, nil, nil)
		},
		"dual_with_input": func(p *Pipeline, ctx context.Context) error {
			_, err := p.StartWithDualOutputAndInput(ctx, "", nil, nil)
			return err
		},
	}
	for name, start := range starts {
		t.Run(name, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			worker := decoderFixture(t, fmt.Sprintf("echo $$ > %q\nexec /bin/sleep 30", pidFile))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			p := NewPipeline(&config.Config{GStreamerBin: worker}, "lifecycle", zerolog.Nop())
			if err := start(p, ctx); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = p.Stop() })

			// Wait until the worker is running, then verify it is the child we
			// own rather than a grandchild hidden behind a launch shell.
			var workerPID int
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				data, _ := os.ReadFile(pidFile)
				workerPID, _ = strconv.Atoi(strings.TrimSpace(string(data)))
				if workerPID > 0 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if workerPID == 0 {
				t.Fatal("worker did not start")
			}
			if workerPID != p.CurrentPID() {
				t.Fatalf("worker PID %d differs from owned child %d", workerPID, p.CurrentPID())
			}
			cancel() // Deliberately do not call Stop: cancellation must suffice.
			select {
			case <-p.Done():
			case <-time.After(3 * time.Second):
				t.Fatal("pipeline was not reaped after cancellation")
			}
			var status syscall.WaitStatus
			pid, err := syscall.Wait4(workerPID, &status, syscall.WNOHANG, nil)
			if pid != -1 || !errors.Is(err, syscall.ECHILD) {
				t.Fatalf("worker not reaped: wait4 pid=%d err=%v", pid, err)
			}
			if err := syscall.Kill(workerPID, 0); !errors.Is(err, syscall.ESRCH) {
				t.Fatalf("worker still exists after cancellation: %v", err)
			}
		})
	}
}
