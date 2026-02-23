package models

import "testing"

func TestNormalizePlatformRole(t *testing.T) {
	tests := []struct {
		name string
		in   PlatformRole
		want PlatformRole
	}{
		{name: "admin legacy", in: PlatformRole("admin"), want: PlatformRoleAdmin},
		{name: "manager legacy", in: PlatformRole("manager"), want: PlatformRoleMod},
		{name: "platform admin canonical", in: PlatformRoleAdmin, want: PlatformRoleAdmin},
		{name: "platform mod canonical", in: PlatformRoleMod, want: PlatformRoleMod},
		{name: "user canonical", in: PlatformRoleUser, want: PlatformRoleUser},
	}

	for _, tt := range tests {
		if got := normalizePlatformRole(tt.in); got != tt.want {
			t.Fatalf("%s: normalizePlatformRole(%q)=%q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestUserIsPlatformAdminLegacyRole(t *testing.T) {
	u := &User{PlatformRole: PlatformRole("admin")}
	if !u.IsPlatformAdmin() {
		t.Fatalf("expected legacy admin role to be treated as platform admin")
	}
}
