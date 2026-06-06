/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package playout

import (
	"fmt"
	"hash/fnv"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// deterministicMediaPick returns a stable media row from a candidate set.
// Callers MUST pass `candidates` already ordered by a stable key
// (typically `ORDER BY id ASC` from GORM) so the offset is meaningful.
//
// Two control-plane instances against the same DB & schedule will pass the
// same `context` values & see the same candidate list, so they pick the same
// row. That's the HA lockstep property the executor-determinism audit needs
// (see docs/audits/2026-06-06-executor-determinism.md, findings C1/C2/C3).
//
// Uses FNV-64a — cheap, allocation-free for our small inputs, & the same
// hash on every platform. Not cryptographic; we don't need it to be.
//
// Returns nil if `candidates` is empty so callers can fall through to their
// existing "no media" branches without a separate length check.
func deterministicMediaPick(candidates []models.MediaItem, ctx ...any) *models.MediaItem {
	if len(candidates) == 0 {
		return nil
	}
	h := fnv.New64a()
	for _, c := range ctx {
		fmt.Fprintf(h, "%v|", c)
	}
	idx := int(h.Sum64() % uint64(len(candidates)))
	return &candidates[idx]
}
