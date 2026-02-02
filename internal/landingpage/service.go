/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package landingpage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/media"
	"github.com/friendsincode/grimnir_radio/internal/models"
)

var (
	// ErrNotFound is returned when a landing page is not found.
	ErrNotFound = errors.New("landing page not found")
	// ErrVersionNotFound is returned when a version is not found.
	ErrVersionNotFound = errors.New("version not found")
	// ErrAssetNotFound is returned when an asset is not found.
	ErrAssetNotFound = errors.New("asset not found")
	// ErrInvalidAssetType is returned for unsupported asset types.
	ErrInvalidAssetType = errors.New("invalid asset type")
)

// Service provides landing page management functionality.
type Service struct {
	db           *gorm.DB
	mediaService *media.Service
	mediaRoot    string
	logger       zerolog.Logger
}

// NewService creates a new landing page service.
func NewService(db *gorm.DB, mediaService *media.Service, mediaRoot string, logger zerolog.Logger) *Service {
	return &Service{
		db:           db,
		mediaService: mediaService,
		mediaRoot:    mediaRoot,
		logger:       logger.With().Str("component", "landingpage").Logger(),
	}
}

// GetOrCreate retrieves or creates a landing page for a station.
func (s *Service) GetOrCreate(ctx context.Context, stationID string) (*models.LandingPage, error) {
	var page models.LandingPage
	err := s.db.WithContext(ctx).Where("station_id = ?", stationID).First(&page).Error
	if err == nil {
		return &page, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("query landing page: %w", err)
	}

	// Create new landing page with default config
	page = models.LandingPage{
		ID:              uuid.NewString(),
		StationID:       &stationID,
		Theme:           "default",
		PublishedConfig: GetThemeDefaults("default"),
		DraftConfig:     nil,
	}

	if err := s.db.WithContext(ctx).Create(&page).Error; err != nil {
		return nil, fmt.Errorf("create landing page: %w", err)
	}

	s.logger.Info().Str("station_id", stationID).Msg("created landing page")
	return &page, nil
}

// GetOrCreatePlatform retrieves or creates the platform landing page (no station).
func (s *Service) GetOrCreatePlatform(ctx context.Context) (*models.LandingPage, error) {
	var page models.LandingPage
	err := s.db.WithContext(ctx).Where("station_id IS NULL").First(&page).Error
	if err == nil {
		return &page, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("query platform landing page: %w", err)
	}

	// Create new platform landing page with default config
	page = models.LandingPage{
		ID:              uuid.NewString(),
		StationID:       nil,
		Theme:           "default",
		PublishedConfig: GetPlatformThemeDefaults(),
		DraftConfig:     nil,
	}

	if err := s.db.WithContext(ctx).Create(&page).Error; err != nil {
		return nil, fmt.Errorf("create platform landing page: %w", err)
	}

	s.logger.Info().Msg("created platform landing page")
	return &page, nil
}

// GetPlatform retrieves the platform landing page.
func (s *Service) GetPlatform(ctx context.Context) (*models.LandingPage, error) {
	var page models.LandingPage
	err := s.db.WithContext(ctx).Where("station_id IS NULL").First(&page).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query platform landing page: %w", err)
	}
	return &page, nil
}

// GetPlatformDraft retrieves the draft configuration for the platform.
func (s *Service) GetPlatformDraft(ctx context.Context) (map[string]any, error) {
	page, err := s.GetOrCreatePlatform(ctx)
	if err != nil {
		return nil, err
	}

	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		return page.DraftConfig, nil
	}

	return page.PublishedConfig, nil
}

// GetPlatformPublished retrieves the published configuration for the platform.
func (s *Service) GetPlatformPublished(ctx context.Context) (map[string]any, error) {
	page, err := s.GetOrCreatePlatform(ctx)
	if err != nil {
		return nil, err
	}

	return page.PublishedConfig, nil
}

// SavePlatformDraft saves a draft configuration for the platform.
func (s *Service) SavePlatformDraft(ctx context.Context, config map[string]any) error {
	page, err := s.GetOrCreatePlatform(ctx)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Model(page).Update("draft_config", config).Error; err != nil {
		return fmt.Errorf("save platform draft: %w", err)
	}

	s.logger.Debug().Msg("saved platform landing page draft")
	return nil
}

// PublishPlatform publishes the platform draft configuration.
func (s *Service) PublishPlatform(ctx context.Context, userID, summary string) error {
	page, err := s.GetOrCreatePlatform(ctx)
	if err != nil {
		return err
	}

	configToPublish := page.PublishedConfig
	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		configToPublish = page.DraftConfig
	}

	version, err := s.createVersion(ctx, page.ID, configToPublish, models.ChangeTypePublish, summary, &userID)
	if err != nil {
		return fmt.Errorf("create version: %w", err)
	}

	now := time.Now()
	updates := map[string]any{
		"published_config": configToPublish,
		"draft_config":     nil,
		"published_at":     now,
		"published_by":     userID,
	}

	if err := s.db.WithContext(ctx).Model(page).Updates(updates).Error; err != nil {
		return fmt.Errorf("publish platform: %w", err)
	}

	s.logger.Info().Int("version", version.VersionNumber).Msg("published platform landing page")
	return nil
}

// DiscardPlatformDraft discards the platform draft configuration.
func (s *Service) DiscardPlatformDraft(ctx context.Context) error {
	page, err := s.GetPlatform(ctx)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Model(page).Update("draft_config", nil).Error; err != nil {
		return fmt.Errorf("discard platform draft: %w", err)
	}

	s.logger.Info().Msg("discarded platform landing page draft")
	return nil
}

// Get retrieves a landing page by station ID.
func (s *Service) Get(ctx context.Context, stationID string) (*models.LandingPage, error) {
	var page models.LandingPage
	err := s.db.WithContext(ctx).Where("station_id = ?", stationID).First(&page).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query landing page: %w", err)
	}
	return &page, nil
}

// GetDraft retrieves the draft configuration for a station.
// Returns published config if no draft exists.
func (s *Service) GetDraft(ctx context.Context, stationID string) (map[string]any, error) {
	page, err := s.GetOrCreate(ctx, stationID)
	if err != nil {
		return nil, err
	}

	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		return page.DraftConfig, nil
	}

	return page.PublishedConfig, nil
}

// GetPublished retrieves the published configuration for a station.
func (s *Service) GetPublished(ctx context.Context, stationID string) (map[string]any, error) {
	page, err := s.GetOrCreate(ctx, stationID)
	if err != nil {
		return nil, err
	}

	return page.PublishedConfig, nil
}

// SaveDraft saves a draft configuration.
func (s *Service) SaveDraft(ctx context.Context, stationID string, config map[string]any) error {
	page, err := s.GetOrCreate(ctx, stationID)
	if err != nil {
		return err
	}

	// Update draft
	if err := s.db.WithContext(ctx).Model(page).Update("draft_config", config).Error; err != nil {
		return fmt.Errorf("save draft: %w", err)
	}

	s.logger.Debug().Str("station_id", stationID).Msg("saved landing page draft")
	return nil
}

// Publish publishes the draft configuration.
func (s *Service) Publish(ctx context.Context, stationID, userID, summary string) error {
	page, err := s.GetOrCreate(ctx, stationID)
	if err != nil {
		return err
	}

	// Use draft if available, otherwise keep current published
	configToPublish := page.PublishedConfig
	if page.DraftConfig != nil && len(page.DraftConfig) > 0 {
		configToPublish = page.DraftConfig
	}

	// Create version
	version, err := s.createVersion(ctx, page.ID, configToPublish, models.ChangeTypePublish, summary, &userID)
	if err != nil {
		return fmt.Errorf("create version: %w", err)
	}

	// Update landing page
	now := time.Now()
	updates := map[string]any{
		"published_config": configToPublish,
		"draft_config":     nil,
		"published_at":     now,
		"published_by":     userID,
	}

	if err := s.db.WithContext(ctx).Model(page).Updates(updates).Error; err != nil {
		return fmt.Errorf("publish: %w", err)
	}

	s.logger.Info().
		Str("station_id", stationID).
		Int("version", version.VersionNumber).
		Msg("published landing page")

	return nil
}

// DiscardDraft discards the draft configuration.
func (s *Service) DiscardDraft(ctx context.Context, stationID string) error {
	page, err := s.Get(ctx, stationID)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Model(page).Update("draft_config", nil).Error; err != nil {
		return fmt.Errorf("discard draft: %w", err)
	}

	s.logger.Info().Str("station_id", stationID).Msg("discarded landing page draft")
	return nil
}

// UpdateTheme updates the theme for a landing page.
func (s *Service) UpdateTheme(ctx context.Context, stationID, themeID string) error {
	theme := GetTheme(themeID)
	if theme == nil {
		return fmt.Errorf("theme not found: %s", themeID)
	}

	page, err := s.GetOrCreate(ctx, stationID)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Model(page).Update("theme", themeID).Error; err != nil {
		return fmt.Errorf("update theme: %w", err)
	}

	return nil
}

// UpdateCustomCSS updates the custom CSS for a landing page.
func (s *Service) UpdateCustomCSS(ctx context.Context, stationID, css string) error {
	page, err := s.GetOrCreate(ctx, stationID)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Model(page).Update("custom_css", css).Error; err != nil {
		return fmt.Errorf("update custom css: %w", err)
	}

	return nil
}

// UpdateCustomHead updates the custom head HTML for a landing page.
func (s *Service) UpdateCustomHead(ctx context.Context, stationID, html string) error {
	page, err := s.GetOrCreate(ctx, stationID)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Model(page).Update("custom_head", html).Error; err != nil {
		return fmt.Errorf("update custom head: %w", err)
	}

	return nil
}

// ListThemes returns all available themes.
func (s *Service) ListThemes() []Theme {
	return BuiltInThemes
}

// GetTheme returns a theme by ID.
func (s *Service) GetTheme(id string) *Theme {
	return GetTheme(id)
}

// createVersion creates a new version record.
func (s *Service) createVersion(ctx context.Context, pageID string, config map[string]any, changeType, summary string, userID *string) (*models.LandingPageVersion, error) {
	// Get the latest version number
	var maxVersion int
	s.db.WithContext(ctx).Model(&models.LandingPageVersion{}).
		Where("landing_page_id = ?", pageID).
		Select("COALESCE(MAX(version_number), 0)").
		Scan(&maxVersion)

	version := models.LandingPageVersion{
		ID:            uuid.NewString(),
		LandingPageID: pageID,
		VersionNumber: maxVersion + 1,
		Config:        config,
		ChangeType:    changeType,
		ChangeSummary: summary,
		CreatedBy:     userID,
	}

	if err := s.db.WithContext(ctx).Create(&version).Error; err != nil {
		return nil, err
	}

	return &version, nil
}

// ListVersions returns version history for a landing page.
func (s *Service) ListVersions(ctx context.Context, stationID string, limit, offset int) ([]models.LandingPageVersion, int64, error) {
	page, err := s.Get(ctx, stationID)
	if err != nil {
		return nil, 0, err
	}

	var versions []models.LandingPageVersion
	var total int64

	query := s.db.WithContext(ctx).
		Model(&models.LandingPageVersion{}).
		Where("landing_page_id = ?", page.ID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("count versions: %w", err)
	}

	if err := query.
		Order("version_number DESC").
		Limit(limit).
		Offset(offset).
		Find(&versions).Error; err != nil {
		return nil, 0, fmt.Errorf("list versions: %w", err)
	}

	return versions, total, nil
}

// GetVersion retrieves a specific version.
func (s *Service) GetVersion(ctx context.Context, versionID string) (*models.LandingPageVersion, error) {
	var version models.LandingPageVersion
	err := s.db.WithContext(ctx).Where("id = ?", versionID).First(&version).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}
	return &version, nil
}

// RestoreVersion restores a landing page to a specific version.
func (s *Service) RestoreVersion(ctx context.Context, stationID, versionID, userID string) error {
	page, err := s.Get(ctx, stationID)
	if err != nil {
		return err
	}

	version, err := s.GetVersion(ctx, versionID)
	if err != nil {
		return err
	}

	// Verify version belongs to this landing page
	if version.LandingPageID != page.ID {
		return ErrVersionNotFound
	}

	// Create a restore version
	summary := fmt.Sprintf("Restored from version %d", version.VersionNumber)
	if _, err := s.createVersion(ctx, page.ID, version.Config, models.ChangeTypeRestore, summary, &userID); err != nil {
		return fmt.Errorf("create restore version: %w", err)
	}

	// Update landing page with restored config
	now := time.Now()
	updates := map[string]any{
		"published_config": version.Config,
		"draft_config":     nil,
		"published_at":     now,
		"published_by":     userID,
	}

	if err := s.db.WithContext(ctx).Model(page).Updates(updates).Error; err != nil {
		return fmt.Errorf("restore version: %w", err)
	}

	s.logger.Info().
		Str("station_id", stationID).
		Int("restored_version", version.VersionNumber).
		Msg("restored landing page version")

	return nil
}

// Asset Management

// UploadAsset uploads an asset file.
// If stationID is nil, this is a platform-level asset.
func (s *Service) UploadAsset(ctx context.Context, stationID *string, assetType, fileName string, r io.Reader, userID *string) (*models.LandingPageAsset, error) {
	// Validate asset type
	validTypes := map[string]bool{
		models.AssetTypeLogo:       true,
		models.AssetTypeBackground: true,
		models.AssetTypeImage:      true,
		models.AssetTypeFavicon:    true,
		models.AssetTypeHero:       true,
	}
	if !validTypes[assetType] {
		return nil, ErrInvalidAssetType
	}

	// Determine mime type from extension
	ext := strings.ToLower(filepath.Ext(fileName))
	mimeTypes := map[string]string{
		".png":  "image/png",
		".jpg":  "image/jpeg",
		".jpeg": "image/jpeg",
		".gif":  "image/gif",
		".webp": "image/webp",
		".svg":  "image/svg+xml",
		".ico":  "image/x-icon",
	}
	mimeType, ok := mimeTypes[ext]
	if !ok {
		return nil, fmt.Errorf("unsupported file type: %s", ext)
	}

	// Generate asset ID and path
	assetID := uuid.NewString()
	pathPrefix := "platform"
	if stationID != nil && *stationID != "" {
		pathPrefix = *stationID
	}
	relativePath := filepath.Join("landing-assets", pathPrefix, assetID+ext)
	fullPath := filepath.Join(s.mediaRoot, relativePath)

	// Ensure directory exists
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create asset directory: %w", err)
	}

	// Create file
	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("create asset file: %w", err)
	}
	defer f.Close()

	// Copy content
	size, err := io.Copy(f, r)
	if err != nil {
		os.Remove(fullPath)
		return nil, fmt.Errorf("write asset: %w", err)
	}

	// Create asset record
	asset := models.LandingPageAsset{
		ID:         assetID,
		StationID:  stationID,
		AssetType:  assetType,
		FilePath:   relativePath,
		FileName:   fileName,
		MimeType:   mimeType,
		FileSize:   size,
		UploadedBy: userID,
	}

	if err := s.db.WithContext(ctx).Create(&asset).Error; err != nil {
		os.Remove(fullPath)
		return nil, fmt.Errorf("create asset record: %w", err)
	}

	logCtx := s.logger.Info().Str("asset_id", assetID).Str("type", assetType)
	if stationID != nil {
		logCtx.Str("station_id", *stationID)
	} else {
		logCtx.Str("scope", "platform")
	}
	logCtx.Msg("uploaded landing page asset")

	return &asset, nil
}

// ListAssets returns assets for a station.
func (s *Service) ListAssets(ctx context.Context, stationID string) ([]models.LandingPageAsset, error) {
	var assets []models.LandingPageAsset
	err := s.db.WithContext(ctx).
		Where("station_id = ?", stationID).
		Order("created_at DESC").
		Find(&assets).Error
	if err != nil {
		return nil, fmt.Errorf("list assets: %w", err)
	}
	return assets, nil
}

// ListPlatformAssets returns assets for the platform landing page.
func (s *Service) ListPlatformAssets(ctx context.Context) ([]models.LandingPageAsset, error) {
	var assets []models.LandingPageAsset
	err := s.db.WithContext(ctx).
		Where("station_id IS NULL").
		Order("created_at DESC").
		Find(&assets).Error
	if err != nil {
		return nil, fmt.Errorf("list platform assets: %w", err)
	}
	return assets, nil
}

// GetAsset retrieves an asset by ID.
func (s *Service) GetAsset(ctx context.Context, assetID string) (*models.LandingPageAsset, error) {
	var asset models.LandingPageAsset
	err := s.db.WithContext(ctx).Where("id = ?", assetID).First(&asset).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get asset: %w", err)
	}
	return &asset, nil
}

// GetAssetPath returns the full path to an asset file.
func (s *Service) GetAssetPath(asset *models.LandingPageAsset) string {
	return filepath.Join(s.mediaRoot, asset.FilePath)
}

// DeleteAsset deletes an asset.
func (s *Service) DeleteAsset(ctx context.Context, assetID string) error {
	asset, err := s.GetAsset(ctx, assetID)
	if err != nil {
		return err
	}

	// Delete file
	fullPath := filepath.Join(s.mediaRoot, asset.FilePath)
	if err := os.Remove(fullPath); err != nil && !os.IsNotExist(err) {
		s.logger.Warn().Err(err).Str("path", fullPath).Msg("failed to delete asset file")
	}

	// Delete record
	if err := s.db.WithContext(ctx).Delete(asset).Error; err != nil {
		return fmt.Errorf("delete asset: %w", err)
	}

	s.logger.Info().
		Str("asset_id", assetID).
		Msg("deleted landing page asset")

	return nil
}
