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
