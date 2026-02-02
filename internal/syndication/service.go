/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package syndication

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"github.com/friendsincode/grimnir_radio/internal/models"
)

// Service handles syndication of network shows.
type Service struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewService creates a new syndication service.
func NewService(db *gorm.DB, logger zerolog.Logger) *Service {
	return &Service{
		db:     db,
		logger: logger.With().Str("component", "syndication").Logger(),
	}
}

// CreateNetwork creates a new network for grouping shows.
func (s *Service) CreateNetwork(ctx context.Context, name, description, ownerID string) (*models.Network, error) {
	network := models.NewNetwork(name)
	network.Description = description
	network.OwnerID = ownerID

	if err := s.db.WithContext(ctx).Create(network).Error; err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	s.logger.Info().Str("network", network.ID).Str("name", name).Msg("network created")
	return network, nil
}

// GetNetwork retrieves a network by ID.
func (s *Service) GetNetwork(ctx context.Context, id string) (*models.Network, error) {
	var network models.Network
	if err := s.db.WithContext(ctx).Preload("Shows").First(&network, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("network not found: %w", err)
	}
	return &network, nil
}

// ListNetworks returns all networks, optionally filtered by owner.
func (s *Service) ListNetworks(ctx context.Context, ownerID string) ([]models.Network, error) {
	var networks []models.Network
	query := s.db.WithContext(ctx)
	if ownerID != "" {
		query = query.Where("owner_id = ?", ownerID)
	}
	if err := query.Order("name ASC").Find(&networks).Error; err != nil {
		return nil, err
	}
	return networks, nil
}

// CreateNetworkShow creates a new syndicated show.
func (s *Service) CreateNetworkShow(ctx context.Context, show *models.NetworkShow) error {
	if show.ID == "" {
		show.ID = uuid.NewString()
	}

	if err := s.db.WithContext(ctx).Create(show).Error; err != nil {
		return fmt.Errorf("failed to create network show: %w", err)
	}

	s.logger.Info().Str("show", show.ID).Str("name", show.Name).Msg("network show created")
	return nil
}

// GetNetworkShow retrieves a network show by ID.
func (s *Service) GetNetworkShow(ctx context.Context, id string) (*models.NetworkShow, error) {
	var show models.NetworkShow
	if err := s.db.WithContext(ctx).
		Preload("SourceShow").
		Preload("Subscriptions").
		Preload("Subscriptions.Station").
		First(&show, "id = ?", id).Error; err != nil {
		return nil, fmt.Errorf("network show not found: %w", err)
	}
	return &show, nil
}

// ListNetworkShows returns all network shows, optionally filtered by network.
func (s *Service) ListNetworkShows(ctx context.Context, networkID string) ([]models.NetworkShow, error) {
	var shows []models.NetworkShow
	query := s.db.WithContext(ctx).Where("active = ?", true)
	if networkID != "" {
		query = query.Where("network_id = ?", networkID)
	}
	if err := query.Order("name ASC").Find(&shows).Error; err != nil {
		return nil, err
	}
	return shows, nil
}

// UpdateNetworkShow updates a network show.
func (s *Service) UpdateNetworkShow(ctx context.Context, id string, updates map[string]any) error {
	if err := s.db.WithContext(ctx).Model(&models.NetworkShow{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return fmt.Errorf("failed to update network show: %w", err)
	}
	return nil
}

// DeleteNetworkShow deletes a network show.
func (s *Service) DeleteNetworkShow(ctx context.Context, id string) error {
	// Delete subscriptions first
	if err := s.db.WithContext(ctx).Where("network_show_id = ?", id).Delete(&models.NetworkSubscription{}).Error; err != nil {
		return fmt.Errorf("failed to delete subscriptions: %w", err)
	}

	if err := s.db.WithContext(ctx).Delete(&models.NetworkShow{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("failed to delete network show: %w", err)
	}

	s.logger.Info().Str("show", id).Msg("network show deleted")
	return nil
}

// Subscribe adds a station subscription to a network show.
func (s *Service) Subscribe(ctx context.Context, stationID, networkShowID string, localTime, localDays, timezone string) (*models.NetworkSubscription, error) {
	// Check if already subscribed
	var existing models.NetworkSubscription
	if err := s.db.WithContext(ctx).Where("station_id = ? AND network_show_id = ?", stationID, networkShowID).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("station already subscribed to this network show")
	}

	sub := models.NewNetworkSubscription(stationID, networkShowID)
	sub.LocalTime = localTime
	sub.LocalDays = localDays
	if timezone != "" {
		sub.Timezone = timezone
	}

	if err := s.db.WithContext(ctx).Create(sub).Error; err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	s.logger.Info().
		Str("station", stationID).
		Str("network_show", networkShowID).
		Msg("station subscribed to network show")

	return sub, nil
}

// Unsubscribe removes a station subscription.
func (s *Service) Unsubscribe(ctx context.Context, subscriptionID string) error {
	if err := s.db.WithContext(ctx).Delete(&models.NetworkSubscription{}, "id = ?", subscriptionID).Error; err != nil {
		return fmt.Errorf("failed to delete subscription: %w", err)
	}
	return nil
}

// GetStationSubscriptions returns all subscriptions for a station.
func (s *Service) GetStationSubscriptions(ctx context.Context, stationID string) ([]models.NetworkSubscription, error) {
	var subs []models.NetworkSubscription
	if err := s.db.WithContext(ctx).
		Where("station_id = ? AND active = ?", stationID, true).
		Preload("NetworkShow").
		Order("created_at DESC").
		Find(&subs).Error; err != nil {
		return nil, err
	}
	return subs, nil
}

// MaterializeSubscriptions creates show instances for subscribed network shows.
func (s *Service) MaterializeSubscriptions(ctx context.Context, stationID string, start, end time.Time) ([]models.ShowInstance, error) {
	subs, err := s.GetStationSubscriptions(ctx, stationID)
	if err != nil {
		return nil, err
	}

	var instances []models.ShowInstance

	for _, sub := range subs {
		if !sub.Active || sub.NetworkShow == nil {
			continue
		}

		// Parse local time and days
		if sub.LocalTime == "" || sub.LocalDays == "" {
			continue
		}

		days := parseDays(sub.LocalDays)
		localHour, localMin := parseTime(sub.LocalTime)

		// Generate instances for each day in range that matches
		current := start
		for current.Before(end) {
			weekday := int(current.Weekday())
			if containsDay(days, weekday) {
				// Create instance at local time
				instanceStart := time.Date(
					current.Year(), current.Month(), current.Day(),
					localHour, localMin, 0, 0,
					current.Location(),
				)

				// Apply delay
				if sub.NetworkShow.DelayMinutes > 0 {
					instanceStart = instanceStart.Add(time.Duration(sub.NetworkShow.DelayMinutes) * time.Minute)
				}

				instanceEnd := instanceStart.Add(time.Duration(sub.NetworkShow.Duration) * time.Minute)

				// Check for conflicts
				var conflictCount int64
				s.db.Model(&models.ShowInstance{}).
					Where("station_id = ?", stationID).
					Where("starts_at < ? AND ends_at > ?", instanceEnd, instanceStart).
					Where("status = ?", models.ShowInstanceScheduled).
					Count(&conflictCount)

				if conflictCount == 0 {
					instance := &models.ShowInstance{
						ID:            uuid.NewString(),
						StationID:     stationID,
						StartsAt:      instanceStart,
						EndsAt:        instanceEnd,
						Status:        models.ShowInstanceScheduled,
						ExceptionNote: fmt.Sprintf("Syndicated: %s", sub.NetworkShow.Name),
					}
					instances = append(instances, *instance)
				}
			}
			current = current.AddDate(0, 0, 1)
		}
	}

	// Save instances if auto_schedule is enabled
	for i, inst := range instances {
		if err := s.db.WithContext(ctx).Create(&inst).Error; err != nil {
			s.logger.Warn().Err(err).Str("instance", inst.ID).Msg("failed to create syndicated instance")
			continue
		}
		instances[i] = inst
	}

	return instances, nil
}

// parseDays parses comma-separated day codes (MO,TU,WE,TH,FR,SA,SU).
func parseDays(days string) []int {
	dayMap := map[string]int{
		"SU": 0, "MO": 1, "TU": 2, "WE": 3, "TH": 4, "FR": 5, "SA": 6,
	}

	var result []int
	for _, d := range strings.Split(days, ",") {
		if day, ok := dayMap[strings.TrimSpace(strings.ToUpper(d))]; ok {
			result = append(result, day)
		}
	}
	return result
}

// parseTime parses HH:MM:SS or HH:MM format.
func parseTime(t string) (hour, minute int) {
	parts := strings.Split(t, ":")
	if len(parts) >= 2 {
		fmt.Sscanf(parts[0], "%d", &hour)
		fmt.Sscanf(parts[1], "%d", &minute)
	}
	return
}

// containsDay checks if a day is in the list.
func containsDay(days []int, day int) bool {
	for _, d := range days {
		if d == day {
			return true
		}
	}
	return false
}
