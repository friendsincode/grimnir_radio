/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package clock

import (
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Planner compiles clocks into slot plans.
type Planner struct {
	db     *gorm.DB
	logger zerolog.Logger
}

// NewPlanner constructs a clock planner.
func NewPlanner(db *gorm.DB, logger zerolog.Logger) *Planner {
	return &Planner{db: db, logger: logger}
}

// Compile loads clock definitions and expands them over the requested horizon.
func (p *Planner) Compile(stationID string, start time.Time, horizon time.Duration) ([]SlotPlan, error) {
	start = start.UTC().Truncate(time.Minute)
	if horizon <= 0 {
		horizon = time.Hour
	}

	var station models.Station
	loc := time.UTC
	if err := p.db.Select("timezone").Where("id = ?", stationID).First(&station).Error; err == nil && station.Timezone != "" {
		loaded, loadErr := time.LoadLocation(station.Timezone)
		if loadErr == nil {
			loc = loaded
		} else {
			p.logger.Warn().Err(loadErr).Str("station_id", stationID).Str("timezone", station.Timezone).Msg("invalid station timezone, falling back to UTC")
		}
	}

	var clockHours []models.ClockHour
	// Order by window width ascending so narrower (more specific) clocks are
	// matched before broader ones (e.g. a 6-12 clock beats a 0-24 fallback).
	// Ties broken by start_hour then created_at for deterministic selection.
	err := p.db.Where("station_id = ?", stationID).
		Preload("Slots").
		Order("(end_hour - start_hour) ASC, start_hour ASC, created_at ASC").
		Find(&clockHours).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if len(clockHours) == 0 {
		return nil, nil
	}

	plans := buildPlansForStation(clockHours, start, horizon, loc)
	return plans, nil
}

func (p *Planner) CompileForClock(clockID string, start time.Time, horizon time.Duration) ([]SlotPlan, error) {
	start = start.UTC().Truncate(time.Minute)
	if horizon <= 0 {
		horizon = time.Hour
	}

	var clockHour models.ClockHour
	if err := p.db.Where("id = ?", clockID).Preload("Slots").First(&clockHour).Error; err != nil {
		return nil, err
	}
	if len(clockHour.Slots) == 0 {
		return nil, nil
	}

	plans := buildPlans(clockHour, start, horizon)
	return plans, nil
}

func buildPlans(clockHour models.ClockHour, start time.Time, horizon time.Duration) []SlotPlan {
	slots := make([]models.ClockSlot, len(clockHour.Slots))
	copy(slots, clockHour.Slots)
	sort.Slice(slots, func(i, j int) bool {
		return slots[i].Position < slots[j].Position
	})

	plans := make([]SlotPlan, 0, len(slots)*int(horizon/time.Hour+1))
	cursor := start.Truncate(time.Hour)
	end := start.Add(horizon)

	for cursor.Before(end) {
		for _, slot := range slots {
			planStart := cursor.Add(slot.Offset)
			if planStart.Before(start) {
				continue
			}

			duration := slotPayloadDuration(slot.Payload)
			if duration <= 0 {
				duration = time.Minute
			}

			plan := SlotPlan{
				SlotID:   slot.ID,
				StartsAt: planStart,
				EndsAt:   planStart.Add(duration),
				Duration: duration,
				SlotType: string(slot.Type),
				Payload:  normalizePayload(slot.Payload),
			}
			plans = append(plans, plan)
		}
		cursor = cursor.Add(time.Hour)
	}

	return plans
}

func buildPlansForStation(clockHours []models.ClockHour, start time.Time, horizon time.Duration, loc *time.Location) []SlotPlan {
	if len(clockHours) == 0 {
		return nil
	}
	cursor := start.Truncate(time.Hour)
	end := start.Add(horizon)
	plans := make([]SlotPlan, 0, len(clockHours)*int(horizon/time.Hour+1))

	for cursor.Before(end) {
		clockHour := selectClockHour(clockHours, cursor, loc)
		if clockHour != nil && len(clockHour.Slots) > 0 {
			hourPlans := buildPlans(*clockHour, cursor, time.Hour)
			for _, plan := range hourPlans {
				if plan.StartsAt.Before(start) || !plan.StartsAt.Before(end) {
					continue
				}
				plans = append(plans, plan)
			}
		}
		cursor = cursor.Add(time.Hour)
	}

	return plans
}

func selectClockHour(clockHours []models.ClockHour, instant time.Time, loc *time.Location) *models.ClockHour {
	local := instant.In(loc)
	hour := local.Hour()

	for i := range clockHours {
		if clockWindowApplies(clockHours[i], hour) {
			return &clockHours[i]
		}
	}
	return nil
}

func clockWindowApplies(clockHour models.ClockHour, hour int) bool {
	startHour, endHour := normalizeClockWindow(clockHour.StartHour, clockHour.EndHour)
	if startHour == endHour {
		return true
	}
	if startHour < endHour {
		return hour >= startHour && hour < endHour
	}
	return hour >= startHour || hour < endHour
}

func normalizeClockWindow(startHour, endHour int) (int, int) {
	if startHour < 0 || startHour > 23 {
		startHour = 0
	}
	if endHour < 1 || endHour > 24 {
		endHour = 24
	}
	return startHour, endHour
}

func slotPayloadDuration(payload map[string]any) time.Duration {
	if payload == nil {
		return 0
	}
	if raw, ok := payload["duration_ms"]; ok {
		if ms := asInt64(raw); ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	if raw, ok := payload["duration_seconds"]; ok {
		if sec := asInt64(raw); sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 0
}

func asInt64(v any) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case float32:
		return int64(val)
	case int:
		return int64(val)
	case int32:
		return int64(val)
	case int64:
		return val
	case json.Number:
		i, err := val.Int64()
		if err == nil {
			return i
		}
	}
	return 0
}

func normalizePayload(payload map[string]any) map[string]any {
	if payload == nil {
		return make(map[string]any)
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		out[k] = v
	}
	return out
}
