/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package integrity

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

type FindingType string

const (
	FindingStationMissingMount         FindingType = "station_missing_mount"
	FindingStationOwnerMembershipGap   FindingType = "station_owner_membership_gap"
	FindingOrphanStationUser           FindingType = "orphan_station_user"
	FindingOrphanShowInstance          FindingType = "orphan_show_instance"
	FindingShowInstanceStationMismatch FindingType = "show_instance_station_mismatch"
)

type Finding struct {
	ID         string
	Type       FindingType
	Severity   string
	Summary    string
	StationID  *string
	ResourceID string
	Repairable bool
	Details    map[string]any
}

type Report struct {
	GeneratedAt time.Time
	Total       int
	ByType      map[FindingType]int
	Findings    []Finding
}

type RepairInput struct {
	Type       FindingType
	StationID  string
	ResourceID string
}

type RepairResult struct {
	Changed bool
	Message string
	Details map[string]any
}

type Service struct {
	db     *gorm.DB
	logger zerolog.Logger
}

func NewService(db *gorm.DB, logger zerolog.Logger) *Service {
	return &Service{
		db:     db,
		logger: logger.With().Str("component", "integrity").Logger(),
	}
}

func (s *Service) Scan(ctx context.Context) (*Report, error) {
	findings := make([]Finding, 0, 32)

	added, err := s.scanStationsMissingMount(ctx)
	if err != nil {
		return nil, err
	}
	findings = append(findings, added...)

	added, err = s.scanStationOwnerMembershipGaps(ctx)
	if err != nil {
		return nil, err
	}
	findings = append(findings, added...)

	added, err = s.scanOrphanStationUsers(ctx)
	if err != nil {
		return nil, err
	}
	findings = append(findings, added...)

	added, err = s.scanOrphanShowInstances(ctx)
	if err != nil {
		return nil, err
	}
	findings = append(findings, added...)

	added, err = s.scanShowInstanceStationMismatch(ctx)
	if err != nil {
		return nil, err
	}
	findings = append(findings, added...)

	byType := make(map[FindingType]int)
	for _, f := range findings {
		byType[f.Type]++
	}

	report := &Report{
		GeneratedAt: time.Now().UTC(),
		Total:       len(findings),
		ByType:      byType,
		Findings:    findings,
	}

	if report.Total > 0 {
		s.logger.Warn().Int("total_findings", report.Total).Interface("by_type", byType).Msg("integrity scan completed with findings")
	} else {
		s.logger.Info().Msg("integrity scan completed with no findings")
	}

	return report, nil
}

func (s *Service) Repair(ctx context.Context, input RepairInput) (RepairResult, error) {
	switch input.Type {
	case FindingStationMissingMount:
		return s.repairStationMissingMount(ctx, input)
	case FindingStationOwnerMembershipGap:
		return s.repairStationOwnerMembershipGap(ctx, input)
	case FindingOrphanStationUser:
		return s.repairOrphanStationUser(ctx, input)
	case FindingOrphanShowInstance:
		return s.repairOrphanShowInstance(ctx, input)
	case FindingShowInstanceStationMismatch:
		return s.repairShowInstanceStationMismatch(ctx, input)
	default:
		return RepairResult{}, fmt.Errorf("unsupported finding type: %s", input.Type)
	}
}

func (s *Service) scanStationsMissingMount(ctx context.Context) ([]Finding, error) {
	type row struct {
		ID   string
		Name string
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Table("stations").
		Select("stations.id, stations.name").
		Joins("LEFT JOIN mounts ON mounts.station_id = stations.id").
		Where("mounts.id IS NULL").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	findings := make([]Finding, 0, len(rows))
	for _, r := range rows {
		stationID := r.ID
		findings = append(findings, Finding{
			ID:         findingID(FindingStationMissingMount, stationID, stationID),
			Type:       FindingStationMissingMount,
			Severity:   "high",
			Summary:    "Station has no mount configured",
			StationID:  &stationID,
			ResourceID: stationID,
			Repairable: true,
			Details: map[string]any{
				"station_name": r.Name,
			},
		})
	}
	return findings, nil
}

func (s *Service) scanStationOwnerMembershipGaps(ctx context.Context) ([]Finding, error) {
	type row struct {
		ID      string
		Name    string
		OwnerID string
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Table("stations").
		Select("stations.id, stations.name, stations.owner_id").
		Where("stations.owner_id <> ''").
		Where("NOT EXISTS (SELECT 1 FROM station_users su WHERE su.station_id = stations.id AND su.user_id = stations.owner_id)").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	findings := make([]Finding, 0, len(rows))
	for _, r := range rows {
		stationID := r.ID
		findings = append(findings, Finding{
			ID:         findingID(FindingStationOwnerMembershipGap, stationID, stationID),
			Type:       FindingStationOwnerMembershipGap,
			Severity:   "high",
			Summary:    "Station owner is not present in station_users",
			StationID:  &stationID,
			ResourceID: stationID,
			Repairable: true,
			Details: map[string]any{
				"station_name": r.Name,
				"owner_id":     r.OwnerID,
			},
		})
	}
	return findings, nil
}

func (s *Service) scanOrphanStationUsers(ctx context.Context) ([]Finding, error) {
	type row struct {
		ID             string
		StationID      string
		UserID         string
		MissingUser    bool
		MissingStation bool
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Table("station_users su").
		Select(`
			su.id, su.station_id, su.user_id,
			(u.id IS NULL) AS missing_user,
			(s.id IS NULL) AS missing_station
		`).
		Joins("LEFT JOIN users u ON u.id = su.user_id").
		Joins("LEFT JOIN stations s ON s.id = su.station_id").
		Where("u.id IS NULL OR s.id IS NULL").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	findings := make([]Finding, 0, len(rows))
	for _, r := range rows {
		stationID := r.StationID
		findings = append(findings, Finding{
			ID:         findingID(FindingOrphanStationUser, stationID, r.ID),
			Type:       FindingOrphanStationUser,
			Severity:   "medium",
			Summary:    "Station/user association references missing records",
			StationID:  &stationID,
			ResourceID: r.ID,
			Repairable: true,
			Details: map[string]any{
				"user_id":         r.UserID,
				"station_id":      r.StationID,
				"missing_user":    r.MissingUser,
				"missing_station": r.MissingStation,
			},
		})
	}
	return findings, nil
}

func (s *Service) scanOrphanShowInstances(ctx context.Context) ([]Finding, error) {
	type row struct {
		ID        string
		StationID string
		ShowID    string
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Table("show_instances si").
		Select("si.id, si.station_id, si.show_id").
		Joins("LEFT JOIN shows s ON s.id = si.show_id").
		Where("s.id IS NULL").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	findings := make([]Finding, 0, len(rows))
	for _, r := range rows {
		stationID := r.StationID
		findings = append(findings, Finding{
			ID:         findingID(FindingOrphanShowInstance, stationID, r.ID),
			Type:       FindingOrphanShowInstance,
			Severity:   "high",
			Summary:    "Show instance references a deleted/missing show",
			StationID:  &stationID,
			ResourceID: r.ID,
			Repairable: true,
			Details: map[string]any{
				"show_id": r.ShowID,
			},
		})
	}
	return findings, nil
}

func (s *Service) scanShowInstanceStationMismatch(ctx context.Context) ([]Finding, error) {
	type row struct {
		ID                string
		InstanceStationID string
		ShowStationID     string
		ShowID            string
	}
	var rows []row
	if err := s.db.WithContext(ctx).
		Table("show_instances si").
		Select("si.id, si.station_id AS instance_station_id, s.station_id AS show_station_id, si.show_id").
		Joins("JOIN shows s ON s.id = si.show_id").
		Where("si.station_id <> s.station_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}

	findings := make([]Finding, 0, len(rows))
	for _, r := range rows {
		stationID := r.InstanceStationID
		findings = append(findings, Finding{
			ID:         findingID(FindingShowInstanceStationMismatch, stationID, r.ID),
			Type:       FindingShowInstanceStationMismatch,
			Severity:   "high",
			Summary:    "Show instance station_id does not match parent show station_id",
			StationID:  &stationID,
			ResourceID: r.ID,
			Repairable: true,
			Details: map[string]any{
				"show_id":             r.ShowID,
				"instance_station_id": r.InstanceStationID,
				"expected_station_id": r.ShowStationID,
			},
		})
	}
	return findings, nil
}

func (s *Service) repairStationMissingMount(ctx context.Context, input RepairInput) (RepairResult, error) {
	var station models.Station
	if err := s.db.WithContext(ctx).First(&station, "id = ?", input.ResourceID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return RepairResult{Changed: false, Message: "station not found (already removed)"}, nil
		}
		return RepairResult{}, err
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Mount{}).Where("station_id = ?", station.ID).Count(&count).Error; err != nil {
		return RepairResult{}, err
	}
	if count > 0 {
		return RepairResult{Changed: false, Message: "station already has mount(s)"}, nil
	}

	mountName := models.GenerateMountName(station.Name)
	mount := models.Mount{
		ID:         uuid.NewString(),
		StationID:  station.ID,
		Name:       mountName,
		URL:        "/" + mountName,
		Format:     "mp3",
		Bitrate:    128,
		Channels:   2,
		SampleRate: 44100,
	}
	if err := s.db.WithContext(ctx).Create(&mount).Error; err != nil {
		return RepairResult{}, err
	}

	return RepairResult{
		Changed: true,
		Message: "created default mount",
		Details: map[string]any{"mount_id": mount.ID, "mount_name": mount.Name},
	}, nil
}

func (s *Service) repairStationOwnerMembershipGap(ctx context.Context, input RepairInput) (RepairResult, error) {
	var station models.Station
	if err := s.db.WithContext(ctx).First(&station, "id = ?", input.ResourceID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return RepairResult{Changed: false, Message: "station not found"}, nil
		}
		return RepairResult{}, err
	}
	if station.OwnerID == "" {
		return RepairResult{Changed: false, Message: "station has no owner_id"}, nil
	}

	var su models.StationUser
	err := s.db.WithContext(ctx).
		Where("station_id = ? AND user_id = ?", station.ID, station.OwnerID).
		First(&su).Error

	if err == nil {
		if su.Role == models.StationRoleOwner {
			return RepairResult{Changed: false, Message: "owner membership already present"}, nil
		}
		if err := s.db.WithContext(ctx).Model(&models.StationUser{}).
			Where("id = ?", su.ID).
			Update("role", models.StationRoleOwner).Error; err != nil {
			return RepairResult{}, err
		}
		return RepairResult{
			Changed: true,
			Message: "updated existing membership role to owner",
			Details: map[string]any{"station_user_id": su.ID},
		}, nil
	}
	if err != gorm.ErrRecordNotFound {
		return RepairResult{}, err
	}

	newSU := models.StationUser{
		ID:        uuid.NewString(),
		UserID:    station.OwnerID,
		StationID: station.ID,
		Role:      models.StationRoleOwner,
	}
	if err := s.db.WithContext(ctx).Create(&newSU).Error; err != nil {
		return RepairResult{}, err
	}

	return RepairResult{
		Changed: true,
		Message: "created missing owner membership",
		Details: map[string]any{"station_user_id": newSU.ID},
	}, nil
}

func (s *Service) repairOrphanStationUser(ctx context.Context, input RepairInput) (RepairResult, error) {
	var su models.StationUser
	if err := s.db.WithContext(ctx).First(&su, "id = ?", input.ResourceID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return RepairResult{Changed: false, Message: "station_user already removed"}, nil
		}
		return RepairResult{}, err
	}

	var userCount int64
	if err := s.db.WithContext(ctx).Model(&models.User{}).Where("id = ?", su.UserID).Count(&userCount).Error; err != nil {
		return RepairResult{}, err
	}
	var stationCount int64
	if err := s.db.WithContext(ctx).Model(&models.Station{}).Where("id = ?", su.StationID).Count(&stationCount).Error; err != nil {
		return RepairResult{}, err
	}
	if userCount > 0 && stationCount > 0 {
		return RepairResult{Changed: false, Message: "association is no longer orphaned"}, nil
	}

	if err := s.db.WithContext(ctx).Delete(&su).Error; err != nil {
		return RepairResult{}, err
	}
	return RepairResult{Changed: true, Message: "deleted orphan station_user association"}, nil
}

func (s *Service) repairOrphanShowInstance(ctx context.Context, input RepairInput) (RepairResult, error) {
	var inst models.ShowInstance
	if err := s.db.WithContext(ctx).First(&inst, "id = ?", input.ResourceID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return RepairResult{Changed: false, Message: "show instance already removed"}, nil
		}
		return RepairResult{}, err
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.Show{}).Where("id = ?", inst.ShowID).Count(&count).Error; err != nil {
		return RepairResult{}, err
	}
	if count > 0 {
		return RepairResult{Changed: false, Message: "parent show exists; finding already resolved"}, nil
	}

	if err := s.db.WithContext(ctx).Delete(&inst).Error; err != nil {
		return RepairResult{}, err
	}
	return RepairResult{Changed: true, Message: "deleted orphan show instance"}, nil
}

func (s *Service) repairShowInstanceStationMismatch(ctx context.Context, input RepairInput) (RepairResult, error) {
	type joined struct {
		InstanceID  string `gorm:"column:instance_id"`
		CurrentSID  string `gorm:"column:current_sid"`
		ExpectedSID string `gorm:"column:expected_sid"`
	}
	var row joined
	if err := s.db.WithContext(ctx).
		Table("show_instances si").
		Select("si.id AS instance_id, si.station_id AS current_sid, s.station_id AS expected_sid").
		Joins("JOIN shows s ON s.id = si.show_id").
		Where("si.id = ?", input.ResourceID).
		Limit(1).
		Scan(&row).Error; err != nil {
		return RepairResult{}, err
	}
	if row.InstanceID == "" {
		return RepairResult{Changed: false, Message: "show instance not found"}, nil
	}
	if row.CurrentSID == row.ExpectedSID {
		return RepairResult{Changed: false, Message: "show instance station_id already consistent"}, nil
	}

	if err := s.db.WithContext(ctx).Model(&models.ShowInstance{}).
		Where("id = ?", row.InstanceID).
		Update("station_id", row.ExpectedSID).Error; err != nil {
		return RepairResult{}, err
	}

	return RepairResult{
		Changed: true,
		Message: "updated show instance station_id to match show",
		Details: map[string]any{
			"old_station_id": row.CurrentSID,
			"new_station_id": row.ExpectedSID,
		},
	}, nil
}

func findingID(t FindingType, stationID, resourceID string) string {
	return fmt.Sprintf("%s|%s|%s", t, stationID, resourceID)
}
