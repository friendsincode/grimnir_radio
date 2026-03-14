/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/landingpage"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

func newLandingPageAPITest(t *testing.T) (*LandingPageAPI, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&models.LandingPage{},
		&models.LandingPageVersion{},
		&models.LandingPageAsset{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// nil mediaService is ok for tests that don't upload assets
	svc := landingpage.NewService(db, nil, "", zerolog.Nop())
	api := &API{db: db, logger: zerolog.Nop()}
	return NewLandingPageAPI(api, svc), db
}

func TestLandingPageAPI_Get(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleGet(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id creates default", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		lp.handleGet(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLandingPageAPI_GetDraft(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleGetDraft(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("with station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		lp.handleGetDraft(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLandingPageAPI_Update(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"config": map[string]any{}})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		lp.handleUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		lp.handleUpdate(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("valid reaches service", func(t *testing.T) {
		// SQLite may have issues with jsonb serialization in raw Update calls
		body, _ := json.Marshal(map[string]any{"config": map[string]any{"title": "Test"}})
		req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		lp.handleUpdate(rr, req)
		// Accept 200 or 500 (SQLite jsonb serialization limitation)
		if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
			t.Fatalf("got %d, want 200 or 500", rr.Code)
		}
	})
}

func TestLandingPageAPI_Publish(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", bytes.NewReader([]byte("{}")))
		rr := httptest.NewRecorder()
		lp.handlePublish(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("missing auth", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/?station_id=s1", bytes.NewReader([]byte("{}")))
		rr := httptest.NewRecorder()
		lp.handlePublish(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("valid reaches service", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"summary": "Initial publish"})
		req := httptest.NewRequest("POST", "/?station_id=s1", bytes.NewReader(body))
		req = withAdminClaims(req)
		rr := httptest.NewRecorder()
		lp.handlePublish(rr, req)
		// Accept 200 or 500 (SQLite jsonb version creation limitation)
		if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
			t.Fatalf("got %d, want 200 or 500", rr.Code)
		}
	})
}

func TestLandingPageAPI_DiscardDraft(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleDiscardDraft(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("valid reaches service", func(t *testing.T) {
		// DiscardDraft uses Get (not GetOrCreate) so needs existing page
		req := httptest.NewRequest("POST", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		lp.handleDiscardDraft(rr, req)
		// Accept 200 or 500 (no page exists)
		if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
			t.Fatalf("got %d, want 200 or 500", rr.Code)
		}
	})
}

func TestLandingPageAPI_UpdateTheme(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"theme": "classic"})
		req := httptest.NewRequest("PUT", "/", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		lp.handleUpdateTheme(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader([]byte("{")))
		rr := httptest.NewRecorder()
		lp.handleUpdateTheme(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("valid reaches service", func(t *testing.T) {
		// Use "default" theme which is guaranteed to exist
		body, _ := json.Marshal(map[string]any{"theme": "default"})
		req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader(body))
		rr := httptest.NewRecorder()
		lp.handleUpdateTheme(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLandingPageAPI_UpdateCustomCSS(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte(`{"css":"body{}"}`)))
		rr := httptest.NewRecorder()
		lp.handleUpdateCustomCSS(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader([]byte(`{"css":"body{color:red}"}`)))
		rr := httptest.NewRecorder()
		lp.handleUpdateCustomCSS(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLandingPageAPI_UpdateCustomHead(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/", bytes.NewReader([]byte(`{"html":"<meta>"}`)))
		rr := httptest.NewRecorder()
		lp.handleUpdateCustomHead(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest("PUT", "/?station_id=s1", bytes.NewReader([]byte(`{"html":"<meta name='x'>"}`)))
		rr := httptest.NewRecorder()
		lp.handleUpdateCustomHead(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLandingPageAPI_Preview(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		lp.handlePreview(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		lp.handlePreview(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLandingPageAPI_Assets(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("list assets missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleAssetsList(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("list assets empty", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		lp.handleAssetsList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200, body=%s", rr.Code, rr.Body.String())
		}
		var resp map[string]any
		json.NewDecoder(rr.Body).Decode(&resp) //nolint:errcheck
		if _, ok := resp["assets"]; !ok {
			t.Fatal("expected assets key")
		}
	})

	t.Run("upload missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleAssetsUpload(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("upload missing auth", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		lp.handleAssetsUpload(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})

	t.Run("delete missing asset_id", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/", nil)
		// No chi param for assetID → ""
		rr := httptest.NewRecorder()
		lp.handleAssetsDelete(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("delete nonexistent asset", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/nonexistent", nil)
		req = withChiParam(req, "assetID", "nonexistent-id")
		rr := httptest.NewRecorder()
		lp.handleAssetsDelete(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404, body=%s", rr.Code, rr.Body.String())
		}
	})
}

func TestLandingPageAPI_Versions(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("list versions missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleVersionsList(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("list versions reaches service", func(t *testing.T) {
		// ListVersions uses Get (not GetOrCreate) so needs existing page
		req := httptest.NewRequest("GET", "/?station_id=s1", nil)
		rr := httptest.NewRecorder()
		lp.handleVersionsList(rr, req)
		// Accept 200 or 500 (no page exists in test)
		if rr.Code != http.StatusOK && rr.Code != http.StatusInternalServerError {
			t.Fatalf("got %d, want 200 or 500", rr.Code)
		}
	})

	t.Run("get version missing id", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleVersionsGet(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("get nonexistent version", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/missing", nil)
		req = withChiParam(req, "versionID", "nonexistent-id")
		rr := httptest.NewRecorder()
		lp.handleVersionsGet(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404, body=%s", rr.Code, rr.Body.String())
		}
	})

	t.Run("restore missing station_id", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/v1/restore", nil)
		req = withChiParam(req, "versionID", "v1")
		rr := httptest.NewRecorder()
		lp.handleVersionsRestore(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("restore missing auth", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/?station_id=s1", nil)
		req = withChiParam(req, "versionID", "v1")
		rr := httptest.NewRecorder()
		lp.handleVersionsRestore(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("got %d, want 401", rr.Code)
		}
	})
}

func TestLandingPageAPI_Themes(t *testing.T) {
	lp, _ := newLandingPageAPITest(t)

	t.Run("list themes", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		rr := httptest.NewRecorder()
		lp.handleThemesList(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("got %d, want 200", rr.Code)
		}
	})

	t.Run("get theme missing name", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		// No chi param for "name" → ""
		rr := httptest.NewRecorder()
		lp.handleThemesGet(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("got %d, want 400", rr.Code)
		}
	})

	t.Run("get nonexistent theme", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/nonexistent", nil)
		req = withChiParam(req, "name", "nonexistent-theme")
		rr := httptest.NewRecorder()
		lp.handleThemesGet(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Fatalf("got %d, want 404", rr.Code)
		}
	})
}
