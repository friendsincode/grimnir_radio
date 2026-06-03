/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package main

import (
	"reflect"
	"testing"
)

func TestDiscoverAll(t *testing.T) {
	got, err := DiscoverAll("testdata/migrations_clean")
	if err != nil {
		t.Fatalf("DiscoverAll error: %v", err)
	}
	want := []string{
		"testdata/migrations_clean/100_add.sql",
		"testdata/migrations_clean/101_index.sql",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverAll = %v\nwant %v", got, want)
	}
}

func TestDiscoverAll_MissingDir(t *testing.T) {
	_, err := DiscoverAll("testdata/does_not_exist")
	if err == nil {
		t.Error("DiscoverAll on missing dir: want error, got nil")
	}
}

func TestDiscoverDiff(t *testing.T) {
	// Stub out the git runner.
	original := gitDiffNames
	defer func() { gitDiffNames = original }()

	gitDiffNames = func(baseRef string) ([]string, error) {
		if baseRef != "origin/main" {
			t.Errorf("git called with baseRef = %q, want %q", baseRef, "origin/main")
		}
		return []string{
			"migrations/099_drop.sql",
			"migrations/100_add.sql",
		}, nil
	}

	got, err := DiscoverDiff("origin/main")
	if err != nil {
		t.Fatalf("DiscoverDiff error: %v", err)
	}
	want := []string{
		"migrations/099_drop.sql",
		"migrations/100_add.sql",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DiscoverDiff = %v\nwant %v", got, want)
	}
}

func TestDiscoverDiff_EmptyDiff(t *testing.T) {
	original := gitDiffNames
	defer func() { gitDiffNames = original }()
	gitDiffNames = func(string) ([]string, error) { return nil, nil }

	got, err := DiscoverDiff("origin/main")
	if err != nil {
		t.Fatalf("DiscoverDiff error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("DiscoverDiff on empty diff = %v, want empty", got)
	}
}
