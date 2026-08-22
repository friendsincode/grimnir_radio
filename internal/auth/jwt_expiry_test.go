/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package auth

import (
	"testing"
	"time"
)

// TestParse_RejectsExpiredToken guards a security invariant (#86): a token past
// its ExpiresAt must be rejected. Nothing asserted this, so it leaned entirely
// on the jwt library's defaults with no regression guard.
func TestParse_RejectsExpiredToken(t *testing.T) {
	secret := []byte("s3cr3t")
	tok, err := Issue(secret, Claims{UserID: "u1", Roles: []string{"user"}}, -time.Hour)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := Parse(secret, tok); err == nil {
		t.Fatal("expected an expired token to be rejected, got nil error")
	}
}

// TestNormalizeClaimRoles covers the legacy-role mappings. Only the admin case
// had coverage; the manager and mod/moderator mappings drive authorization and
// were untested.
func TestNormalizeClaimRoles(t *testing.T) {
	cases := []struct {
		roles     []string
		stationID string
		want      string
	}{
		{[]string{"admin"}, "", "platform_admin"},      // unscoped admin -> platform admin
		{[]string{"admin"}, "st1", "admin"},            // scoped admin stays station admin
		{[]string{"manager"}, "", "platform_mod"},      // unscoped manager -> platform mod
		{[]string{"manager"}, "st1", "manager"},        // scoped manager stays
		{[]string{"mod"}, "", "platform_mod"},          // mod -> platform mod
		{[]string{"moderator"}, "st1", "platform_mod"}, // moderator -> platform mod regardless of scope
		{[]string{"DJ"}, "st1", "dj"},                  // unknown role lowercased, passed through
	}
	for _, c := range cases {
		got := normalizeClaimRoles(c.roles, c.stationID)
		if len(got) != 1 || got[0] != c.want {
			t.Errorf("normalizeClaimRoles(%v, %q) = %v, want [%s]", c.roles, c.stationID, got, c.want)
		}
	}
}
