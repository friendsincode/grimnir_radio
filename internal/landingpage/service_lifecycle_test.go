/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"errors"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func TestSaveDraft_Publish_Versions_Lifecycle(t *testing.T) {
	svc, db := newLPService(t)

	// Draft is stored but not yet published.
	draft := map[string]any{"hero": map[string]any{"title": "Coming soon"}}
	if err := svc.SaveDraft(bg(), dbtest.UUID("st1"), draft); err != nil {
		t.Fatalf("SaveDraft: %v", err)
	}
	page, err := svc.Get(bg(), dbtest.UUID("st1"))
	if err != nil {
		t.Fatalf("Get after draft: %v", err)
	}
	if page.DraftConfig["hero"] == nil {
		t.Fatal("draft config not persisted")
	}

	// Publish promotes the draft, clears it, and records a version.
	if err := svc.Publish(bg(), dbtest.UUID("st1"), dbtest.UUID("user-1"), "launch"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	page, _ = svc.Get(bg(), dbtest.UUID("st1"))
	if page.PublishedConfig["hero"] == nil {
		t.Fatal("published config not set from draft")
	}
	if len(page.DraftConfig) != 0 {
		t.Fatalf("draft should be cleared after publish, got %v", page.DraftConfig)
	}

	versions, total, err := svc.ListVersions(bg(), dbtest.UUID("st1"), 10, 0)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if total != 1 || len(versions) != 1 {
		t.Fatalf("expected 1 version, got total=%d len=%d", total, len(versions))
	}
	v := versions[0]
	if v.VersionNumber != 1 || v.ChangeType != models.ChangeTypePublish {
		t.Fatalf("unexpected version: number=%d type=%q", v.VersionNumber, v.ChangeType)
	}

	// GetVersion round-trips by ID.
	got, err := svc.GetVersion(bg(), v.ID)
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if got.ID != v.ID {
		t.Fatalf("GetVersion returned %q, want %q", got.ID, v.ID)
	}

	// Publish a second config, then restore the first version.
	if err := svc.SaveDraft(bg(), dbtest.UUID("st1"), map[string]any{"hero": map[string]any{"title": "v2"}}); err != nil {
		t.Fatalf("SaveDraft 2: %v", err)
	}
	if err := svc.Publish(bg(), dbtest.UUID("st1"), dbtest.UUID("user-1"), "second"); err != nil {
		t.Fatalf("Publish 2: %v", err)
	}
	if err := svc.RestoreVersion(bg(), dbtest.UUID("st1"), v.ID, dbtest.UUID("user-1")); err != nil {
		t.Fatalf("RestoreVersion: %v", err)
	}
	page, _ = svc.Get(bg(), dbtest.UUID("st1"))
	hero, _ := page.PublishedConfig["hero"].(map[string]any)
	if hero["title"] != "Coming soon" {
		t.Fatalf("restore did not bring back v1 config, got %v", page.PublishedConfig["hero"])
	}
	// Restore adds a version tagged as a restore.
	_, total, _ = svc.ListVersions(bg(), dbtest.UUID("st1"), 10, 0)
	if total != 3 {
		t.Fatalf("expected 3 versions after publish+publish+restore, got %d", total)
	}

	// DiscardDraft clears a pending draft.
	if err := svc.SaveDraft(bg(), dbtest.UUID("st1"), map[string]any{"x": 1}); err != nil {
		t.Fatalf("SaveDraft 3: %v", err)
	}
	if err := svc.DiscardDraft(bg(), dbtest.UUID("st1")); err != nil {
		t.Fatalf("DiscardDraft: %v", err)
	}
	page, _ = svc.Get(bg(), dbtest.UUID("st1"))
	if len(page.DraftConfig) != 0 {
		t.Fatalf("draft not discarded, got %v", page.DraftConfig)
	}

	_ = db
}

func TestPlatform_SaveDraft_Publish_Discard(t *testing.T) {
	svc, _ := newLPService(t)

	if err := svc.SavePlatformDraft(bg(), map[string]any{"nav": map[string]any{"brand": "Grimnir"}}); err != nil {
		t.Fatalf("SavePlatformDraft: %v", err)
	}
	draft, err := svc.GetPlatformDraft(bg())
	if err != nil {
		t.Fatalf("GetPlatformDraft: %v", err)
	}
	if draft["nav"] == nil {
		t.Fatal("platform draft not persisted")
	}

	if err := svc.PublishPlatform(bg(), dbtest.UUID("admin"), "go live"); err != nil {
		t.Fatalf("PublishPlatform: %v", err)
	}
	pub, err := svc.GetPlatformPublished(bg())
	if err != nil {
		t.Fatalf("GetPlatformPublished: %v", err)
	}
	if pub["nav"] == nil {
		t.Fatal("platform published config not set from draft")
	}

	// A fresh draft, then discard it.
	if err := svc.SavePlatformDraft(bg(), map[string]any{"x": 1}); err != nil {
		t.Fatalf("SavePlatformDraft 2: %v", err)
	}
	if err := svc.DiscardPlatformDraft(bg()); err != nil {
		t.Fatalf("DiscardPlatformDraft: %v", err)
	}
	page, err := svc.GetPlatform(bg())
	if err != nil {
		t.Fatalf("GetPlatform: %v", err)
	}
	if len(page.DraftConfig) != 0 {
		t.Fatalf("platform draft not discarded, got %v", page.DraftConfig)
	}
}

func TestGetVersion_NotFound(t *testing.T) {
	svc, _ := newLPService(t)
	if _, err := svc.GetVersion(bg(), dbtest.UUID("does-not-exist")); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("GetVersion(unknown) err = %v, want ErrVersionNotFound", err)
	}
}

func TestRestoreVersion_WrongPage_Rejected(t *testing.T) {
	svc, _ := newLPService(t)

	// One version on st1.
	_ = svc.SaveDraft(bg(), dbtest.UUID("st1"), map[string]any{"a": 1})
	_ = svc.Publish(bg(), dbtest.UUID("st1"), dbtest.UUID("u"), "s")
	versions, _, _ := svc.ListVersions(bg(), dbtest.UUID("st1"), 10, 0)
	otherVersionID := versions[0].ID

	// st2 exists but that version belongs to st1 → mismatch rejected.
	_, _ = svc.GetOrCreate(bg(), dbtest.UUID("st2"))
	if err := svc.RestoreVersion(bg(), dbtest.UUID("st2"), otherVersionID, dbtest.UUID("u")); !errors.Is(err, ErrVersionNotFound) {
		t.Fatalf("cross-page restore err = %v, want ErrVersionNotFound", err)
	}
}
