/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package audit

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Recorder is the surface the Wrapper depends on. Concrete implementations
// combine the Store (DB writes) and a Poster (ntfy notifications); tests
// use an in-memory fake.
//
// Contract: PostNtfy is best-effort. WriteStart / WriteComplete / WriteFailed
// are the system-of-record. A failing PostNtfy MUST NOT block DB writes from
// happening; the Wrapper achieves this by ignoring PostNtfy errors entirely.
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
// the binary. The operator + source IP are typically built via
// ResolveOperator / ResolveSourceIP during binary startup.
func NewWrapper(rec Recorder, operator, sourceIP string) *Wrapper {
	return &Wrapper{rec: rec, operator: operator, sourceIP: sourceIP}
}

// Operator returns the bound operator string.
func (w *Wrapper) Operator() string { return w.operator }

// SourceIP returns the bound source IP.
func (w *Wrapper) SourceIP() string { return w.sourceIP }

// Wrap runs inner with audit START / COMPLETE / FAILED bookkeeping. Two ntfy
// notifications fire: one when inner starts, one when it finishes (or fails).
// Panics inside inner are captured into a FAILED row + an urgent ntfy and
// re-panicked.
//
// If the initial WriteStart fails (e.g. Postgres unreachable), inner is NOT
// executed and the error is returned with an "audit start" prefix so callers
// can distinguish audit-plumbing failures from real action errors. This is
// deliberate: every subcommand mutates the cluster, so we refuse to run if
// we cannot write the audit row first.
//
// PostNtfy errors are intentionally swallowed; ntfy is glance-able
// notification, not an authoritative log.
func (w *Wrapper) Wrap(ctx context.Context, subcommand string, args map[string]any, inner func(context.Context) error) (retErr error) {
	if w == nil || w.rec == nil {
		// No recorder wired (early-chunk integration tests). Still run inner.
		return inner(ctx)
	}
	id, err := w.rec.WriteStart(ctx, w.operator, w.sourceIP, subcommand, args)
	if err != nil {
		return fmt.Errorf("audit start: %w", err)
	}
	_ = w.rec.PostNtfy(ctx,
		fmt.Sprintf("%s started", subcommand),
		fmt.Sprintf("%s@%s ran %s; args=%v", w.operator, w.sourceIP, subcommand, args),
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
		_ = w.rec.PostNtfy(ctx,
			fmt.Sprintf("%s completed", subcommand),
			fmt.Sprintf("%s ran %s in %v", w.operator, subcommand, dur),
			PriorityDefault)
	}()

	return inner(ctx)
}

// WrapCobra installs the audit wrapper around a cobra command's RunE. Every
// subcommand registered via RegisterCommands is passed through this helper
// so audit bookkeeping is transparent: the subcommand author only writes the
// body, the wrapper handles START / COMPLETE / FAILED rows + ntfy.
//
// The args map sent to WriteStart is built from the cobra command's flags
// (name -> value); positional args are folded in under "_positional". Secret
// flag values are redacted by the Store before persistence.
//
// A nil Wrapper is a no-op: the original RunE runs unwrapped. This lets the
// binary be built without ntfy / Postgres configured (e.g. for unit tests).
func WrapCobra(w *Wrapper, cmd *cobra.Command) {
	if cmd == nil || cmd.RunE == nil {
		return
	}
	inner := cmd.RunE
	subcommand := cmd.Name()
	cmd.RunE = func(c *cobra.Command, positional []string) error {
		if w == nil {
			return inner(c, positional)
		}
		args := flagMap(c, positional)
		return w.Wrap(c.Context(), subcommand, args, func(_ context.Context) error {
			return inner(c, positional)
		})
	}
}

// flagMap captures every flag that the user actually set (Changed=true) plus
// the positional args. Skipping unset flags keeps the audit row from being
// noisy with defaults the operator did not pick.
func flagMap(cmd *cobra.Command, positional []string) map[string]any {
	m := make(map[string]any)
	if cmd != nil {
		cmd.Flags().Visit(func(f *pflag.Flag) {
			m[f.Name] = f.Value.String()
		})
	}
	if len(positional) > 0 {
		m["_positional"] = positional
	}
	return m
}

// ResolveOperator returns the audit "operator" string. Reads $USER and
// falls back to "unknown" if unset.
func ResolveOperator() string {
	u := os.Getenv("USER")
	if u == "" {
		return "unknown"
	}
	return u
}

// ResolveSourceIP returns the audit "source_ip" string. For SSH'd-in
// operators, $SSH_CLIENT is set to "<client-ip> <client-port> <server-port>";
// we take the first whitespace-separated field. For local TTY sessions, the
// variable is unset and we return "local".
func ResolveSourceIP() string {
	v := os.Getenv("SSH_CLIENT")
	if v == "" {
		return "local"
	}
	fields := strings.Fields(v)
	if len(fields) == 0 {
		return "local"
	}
	return fields[0]
}

// RecorderImpl combines a Store + Poster into a single Recorder. The ntfy
// topic is bound at construction time so each region can post to its own
// audit topic (grimnir-audit-<region>) without per-call plumbing.
type RecorderImpl struct {
	Store  *Store
	Poster Poster
	Topic  string
}

// NewRecorder constructs a real Recorder for production wiring. Pass a nil
// Poster to disable ntfy entirely (DB writes still happen).
func NewRecorder(store *Store, poster Poster, topic string) *RecorderImpl {
	return &RecorderImpl{Store: store, Poster: poster, Topic: topic}
}

// WriteStart delegates to the underlying Store.
func (r *RecorderImpl) WriteStart(ctx context.Context, op, ip, sub string, args map[string]any) (uuid.UUID, error) {
	return r.Store.WriteStart(ctx, op, ip, sub, args)
}

// WriteComplete delegates to the underlying Store.
func (r *RecorderImpl) WriteComplete(ctx context.Context, id uuid.UUID, outcome string, dur time.Duration, notes string) error {
	return r.Store.WriteComplete(ctx, id, outcome, dur, notes)
}

// WriteFailed delegates to the underlying Store.
func (r *RecorderImpl) WriteFailed(ctx context.Context, id uuid.UUID, outcome string, dur time.Duration) error {
	return r.Store.WriteFailed(ctx, id, outcome, dur)
}

// PostNtfy delegates to the underlying Poster. A nil Poster is a silent
// no-op so the binary can run without ntfy configured.
func (r *RecorderImpl) PostNtfy(ctx context.Context, title, msg string, priority Priority) error {
	if r.Poster == nil {
		return nil
	}
	return r.Poster.Post(ctx, r.Topic, title, msg, priority)
}
