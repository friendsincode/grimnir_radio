/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/dbtest"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func strptr(s string) *string { return &s }

func TestUploadAsset_StationLifecycle(t *testing.T) {
	svc, _ := newLPService(t)

	asset, err := svc.UploadAsset(bg(), strptr(dbtest.UUID("st1")), models.AssetTypeLogo, "logo.png", strings.NewReader("PNGDATA"), strptr(dbtest.UUID("user-1")))
	if err != nil {
		t.Fatalf("UploadAsset: %v", err)
	}
	if asset.MimeType != "image/png" || asset.FileSize != int64(len("PNGDATA")) {
		t.Fatalf("asset metadata wrong: mime=%q size=%d", asset.MimeType, asset.FileSize)
	}

	// The file exists on disk at the resolved path.
	path := svc.GetAssetPath(asset)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("asset file not written: %v", err)
	}

	// Retrieval by ID and by type.
	if got, err := svc.GetAsset(bg(), asset.ID); err != nil || got.ID != asset.ID {
		t.Fatalf("GetAsset: got %+v err %v", got, err)
	}
	if got, err := svc.GetAssetByType(bg(), strptr(dbtest.UUID("st1")), models.AssetTypeLogo); err != nil || got.ID != asset.ID {
		t.Fatalf("GetAssetByType: got %+v err %v", got, err)
	}

	// Listing includes it.
	list, err := svc.ListAssets(bg(), dbtest.UUID("st1"))
	if err != nil || len(list) != 1 {
		t.Fatalf("ListAssets: len=%d err=%v", len(list), err)
	}

	// Delete removes both the record and the file.
	if err := svc.DeleteAsset(bg(), asset.ID); err != nil {
		t.Fatalf("DeleteAsset: %v", err)
	}
	if _, err := svc.GetAsset(bg(), asset.ID); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("GetAsset after delete err = %v, want ErrAssetNotFound", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("asset file still present after delete: %v", err)
	}
}

func TestUploadAsset_PlatformScope(t *testing.T) {
	svc, _ := newLPService(t)

	if _, err := svc.UploadAsset(bg(), nil, models.AssetTypeFavicon, "fav.ico", strings.NewReader("i"), nil); err != nil {
		t.Fatalf("platform UploadAsset: %v", err)
	}
	list, err := svc.ListPlatformAssets(bg())
	if err != nil || len(list) != 1 {
		t.Fatalf("ListPlatformAssets: len=%d err=%v", len(list), err)
	}
	if got, err := svc.GetAssetByType(bg(), nil, models.AssetTypeFavicon); err != nil || got.MimeType != "image/x-icon" {
		t.Fatalf("platform GetAssetByType: got %+v err %v", got, err)
	}
}

func TestUploadAsset_Rejections(t *testing.T) {
	svc, _ := newLPService(t)

	if _, err := svc.UploadAsset(bg(), strptr(dbtest.UUID("st1")), "not-a-type", "x.png", strings.NewReader("d"), nil); !errors.Is(err, ErrInvalidAssetType) {
		t.Fatalf("invalid type err = %v, want ErrInvalidAssetType", err)
	}
	if _, err := svc.UploadAsset(bg(), strptr(dbtest.UUID("st1")), models.AssetTypeLogo, "x.bmp", strings.NewReader("d"), nil); err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestGetAssetByType_NotFound(t *testing.T) {
	svc, _ := newLPService(t)
	if _, err := svc.GetAssetByType(bg(), strptr(dbtest.UUID("st1")), models.AssetTypeHero); !errors.Is(err, ErrAssetNotFound) {
		t.Fatalf("missing asset err = %v, want ErrAssetNotFound", err)
	}
}
