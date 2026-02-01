/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

// Package cache provides a Redis-based caching layer for frequently accessed data.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// Default TTL values for different cache types
const (
	DefaultStationListTTL    = 5 * time.Minute
	DefaultMountTTL          = 1 * time.Hour
	DefaultSmartBlockTTL     = 1 * time.Hour
	DefaultMediaItemTTL      = 1 * time.Hour
	DefaultClockTTL          = 1 * time.Hour
	DefaultStationMountsTTL  = 30 * time.Minute
)

// Key prefixes for Redis cache
const (
	KeyStationList         = "grimnir:cache:stations"
	KeyStationMounts       = "grimnir:cache:station_mounts:" // + station_id
	KeyDefaultMount        = "grimnir:cache:default_mount:"  // + station_id
	KeyMount               = "grimnir:cache:mount:"          // + mount_id
	KeySmartBlock          = "grimnir:cache:smartblock:"     // + smartblock_id
	KeyMediaItem           = "grimnir:cache:media:"          // + media_id
	KeyClock               = "grimnir:cache:clock:"          // + clock_id
	KeyClockHours          = "grimnir:cache:clock_hours:"    // + station_id
)

// Config contains cache configuration.
type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int

	// TTL overrides
	StationListTTL   time.Duration
	MountTTL         time.Duration
	SmartBlockTTL    time.Duration
	MediaItemTTL     time.Duration
	ClockTTL         time.Duration
	StationMountsTTL time.Duration

	// Fallback behavior
	DisableOnError bool // If true, disable caching on Redis errors
}

// DefaultConfig returns default cache configuration.
func DefaultConfig() Config {
	return Config{
		RedisAddr:        "localhost:6379",
		StationListTTL:   DefaultStationListTTL,
		MountTTL:         DefaultMountTTL,
		SmartBlockTTL:    DefaultSmartBlockTTL,
		MediaItemTTL:     DefaultMediaItemTTL,
		ClockTTL:         DefaultClockTTL,
		StationMountsTTL: DefaultStationMountsTTL,
		DisableOnError:   true,
	}
}

// Cache provides Redis-backed caching with graceful fallback.
type Cache struct {
	client *redis.Client
	logger zerolog.Logger
	config Config

	mu       sync.RWMutex
	disabled bool // Circuit breaker state
}

// New creates a new cache instance.
func New(cfg Config, logger zerolog.Logger) (*Cache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.RedisAddr,
		Password:     cfg.RedisPassword,
		DB:           cfg.RedisDB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     10,
		MinIdleConns: 2,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		logger.Warn().Err(err).Msg("Redis cache unavailable, running without caching")
		return &Cache{
			logger:   logger.With().Str("component", "cache").Logger(),
			config:   cfg,
			disabled: true,
		}, nil
	}

	logger.Info().Str("addr", cfg.RedisAddr).Msg("Redis cache initialized")

	return &Cache{
		client: client,
		logger: logger.With().Str("component", "cache").Logger(),
		config: cfg,
	}, nil
}

// Close closes the Redis connection.
func (c *Cache) Close() error {
	if c.client != nil {
		return c.client.Close()
	}
	return nil
}

// IsAvailable returns true if the cache is operational.
func (c *Cache) IsAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.disabled && c.client != nil
}

// handleError handles Redis errors with circuit breaker logic.
func (c *Cache) handleError(err error, operation string) {
	if err == nil || err == redis.Nil {
		return
	}

	c.logger.Debug().Err(err).Str("operation", operation).Msg("cache operation failed")

	if c.config.DisableOnError {
		c.mu.Lock()
		c.disabled = true
		c.mu.Unlock()
		c.logger.Warn().Msg("disabling cache due to Redis error")
	}
}

// get retrieves a value from cache and unmarshals it.
func (c *Cache) get(ctx context.Context, key string, dest any) (bool, error) {
	if !c.IsAvailable() {
		return false, nil
	}

	data, err := c.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		c.handleError(err, "get")
		return false, err
	}

	if err := json.Unmarshal(data, dest); err != nil {
		c.logger.Debug().Err(err).Str("key", key).Msg("failed to unmarshal cached value")
		return false, nil
	}

	return true, nil
}

// set stores a value in cache with TTL.
func (c *Cache) set(ctx context.Context, key string, value any, ttl time.Duration) error {
	if !c.IsAvailable() {
		return nil
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal cache value: %w", err)
	}

	if err := c.client.Set(ctx, key, data, ttl).Err(); err != nil {
		c.handleError(err, "set")
		return err
	}

	return nil
}

// delete removes a key from cache.
func (c *Cache) delete(ctx context.Context, key string) error {
	if !c.IsAvailable() {
		return nil
	}

	if err := c.client.Del(ctx, key).Err(); err != nil {
		c.handleError(err, "delete")
		return err
	}

	return nil
}

// deletePattern deletes all keys matching a pattern.
func (c *Cache) deletePattern(ctx context.Context, pattern string) error {
	if !c.IsAvailable() {
		return nil
	}

	// Use SCAN to find keys (safer than KEYS for production)
	var cursor uint64
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			c.handleError(err, "scan")
			return err
		}

		if len(keys) > 0 {
			if err := c.client.Del(ctx, keys...).Err(); err != nil {
				c.handleError(err, "delete_batch")
				return err
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return nil
}

// Station caching methods

// CachedStation represents a cached station record.
type CachedStation struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Timezone    string `json:"timezone"`
	Active      bool   `json:"active"`
	Public      bool   `json:"public"`
	Approved    bool   `json:"approved"`
	Featured    bool   `json:"featured"`
	SortOrder   int    `json:"sort_order"`
}

// GetStationList retrieves the cached list of stations.
func (c *Cache) GetStationList(ctx context.Context) ([]CachedStation, bool) {
	var stations []CachedStation
	found, err := c.get(ctx, KeyStationList, &stations)
	if err != nil || !found {
		return nil, false
	}
	c.logger.Debug().Int("count", len(stations)).Msg("station list cache hit")
	return stations, true
}

// SetStationList caches the list of stations.
func (c *Cache) SetStationList(ctx context.Context, stations []CachedStation) error {
	c.logger.Debug().Int("count", len(stations)).Msg("caching station list")
	return c.set(ctx, KeyStationList, stations, c.config.StationListTTL)
}

// InvalidateStationList removes the station list from cache.
func (c *Cache) InvalidateStationList(ctx context.Context) error {
	c.logger.Debug().Msg("invalidating station list cache")
	return c.delete(ctx, KeyStationList)
}

// Mount caching methods

// CachedMount represents a cached mount record.
type CachedMount struct {
	ID         string `json:"id"`
	StationID  string `json:"station_id"`
	Name       string `json:"name"`
	URL        string `json:"url"`
	Format     string `json:"format"`
	Bitrate    int    `json:"bitrate"`
	Channels   int    `json:"channels"`
	SampleRate int    `json:"sample_rate"`
}

// GetDefaultMount retrieves the cached default mount for a station.
func (c *Cache) GetDefaultMount(ctx context.Context, stationID string) (*CachedMount, bool) {
	var mount CachedMount
	found, err := c.get(ctx, KeyDefaultMount+stationID, &mount)
	if err != nil || !found {
		return nil, false
	}
	c.logger.Debug().Str("station_id", stationID).Str("mount_id", mount.ID).Msg("default mount cache hit")
	return &mount, true
}

// SetDefaultMount caches the default mount for a station.
func (c *Cache) SetDefaultMount(ctx context.Context, stationID string, mount *CachedMount) error {
	c.logger.Debug().Str("station_id", stationID).Str("mount_id", mount.ID).Msg("caching default mount")
	return c.set(ctx, KeyDefaultMount+stationID, mount, c.config.MountTTL)
}

// GetMount retrieves a cached mount by ID.
func (c *Cache) GetMount(ctx context.Context, mountID string) (*CachedMount, bool) {
	var mount CachedMount
	found, err := c.get(ctx, KeyMount+mountID, &mount)
	if err != nil || !found {
		return nil, false
	}
	c.logger.Debug().Str("mount_id", mountID).Msg("mount cache hit")
	return &mount, true
}

// SetMount caches a mount by ID.
func (c *Cache) SetMount(ctx context.Context, mount *CachedMount) error {
	c.logger.Debug().Str("mount_id", mount.ID).Msg("caching mount")
	return c.set(ctx, KeyMount+mount.ID, mount, c.config.MountTTL)
}

// InvalidateMounts removes all mount caches for a station.
func (c *Cache) InvalidateMounts(ctx context.Context, stationID string) error {
	c.logger.Debug().Str("station_id", stationID).Msg("invalidating mount caches")

	// Delete default mount cache
	if err := c.delete(ctx, KeyDefaultMount+stationID); err != nil {
		return err
	}

	// Delete station mounts cache
	if err := c.delete(ctx, KeyStationMounts+stationID); err != nil {
		return err
	}

	return nil
}

// InvalidateMount removes a specific mount from cache.
func (c *Cache) InvalidateMount(ctx context.Context, mountID, stationID string) error {
	c.logger.Debug().Str("mount_id", mountID).Str("station_id", stationID).Msg("invalidating mount cache")

	if err := c.delete(ctx, KeyMount+mountID); err != nil {
		return err
	}

	// Also invalidate station-level caches
	return c.InvalidateMounts(ctx, stationID)
}

// SmartBlock caching methods

// CachedSmartBlock represents a cached smart block definition.
type CachedSmartBlock struct {
	ID                 string         `json:"id"`
	StationID          string         `json:"station_id"`
	Name               string         `json:"name"`
	Description        string         `json:"description"`
	Rules              map[string]any `json:"rules"`
	RotationRules      map[string]any `json:"rotation_rules"`
	ArtistSeparation   int            `json:"artist_separation"`
	TrackSeparation    int            `json:"track_separation"`
	AlbumSeparation    int            `json:"album_separation"`
	MinEnergy          float64        `json:"min_energy"`
	MaxEnergy          float64        `json:"max_energy"`
	EnergyOrderEnabled bool           `json:"energy_order_enabled"`
	Active             bool           `json:"active"`
}

// GetSmartBlock retrieves a cached smart block by ID.
func (c *Cache) GetSmartBlock(ctx context.Context, smartBlockID string) (*CachedSmartBlock, bool) {
	var block CachedSmartBlock
	found, err := c.get(ctx, KeySmartBlock+smartBlockID, &block)
	if err != nil || !found {
		return nil, false
	}
	c.logger.Debug().Str("smartblock_id", smartBlockID).Msg("smart block cache hit")
	return &block, true
}

// SetSmartBlock caches a smart block.
func (c *Cache) SetSmartBlock(ctx context.Context, block *CachedSmartBlock) error {
	c.logger.Debug().Str("smartblock_id", block.ID).Msg("caching smart block")
	return c.set(ctx, KeySmartBlock+block.ID, block, c.config.SmartBlockTTL)
}

// InvalidateSmartBlock removes a smart block from cache.
func (c *Cache) InvalidateSmartBlock(ctx context.Context, smartBlockID string) error {
	c.logger.Debug().Str("smartblock_id", smartBlockID).Msg("invalidating smart block cache")
	return c.delete(ctx, KeySmartBlock+smartBlockID)
}

// MediaItem caching methods

// CachedMediaItem represents a cached media item record.
type CachedMediaItem struct {
	ID            string         `json:"id"`
	StationID     string         `json:"station_id"`
	Title         string         `json:"title"`
	Artist        string         `json:"artist"`
	Album         string         `json:"album"`
	Genre         string         `json:"genre"`
	Year          int            `json:"year"`
	Duration      int64          `json:"duration"` // Nanoseconds
	Path          string         `json:"path"`
	AnalysisState string         `json:"analysis_state"`
	Metadata      map[string]any `json:"metadata"`
	Energy        float64        `json:"energy"`
	IntroEnd      int64          `json:"intro_end"`
	OutroIn       int64          `json:"outro_in"`
}

// GetMediaItem retrieves a cached media item by ID.
func (c *Cache) GetMediaItem(ctx context.Context, mediaID string) (*CachedMediaItem, bool) {
	var item CachedMediaItem
	found, err := c.get(ctx, KeyMediaItem+mediaID, &item)
	if err != nil || !found {
		return nil, false
	}
	c.logger.Debug().Str("media_id", mediaID).Msg("media item cache hit")
	return &item, true
}

// SetMediaItem caches a media item.
func (c *Cache) SetMediaItem(ctx context.Context, item *CachedMediaItem) error {
	c.logger.Debug().Str("media_id", item.ID).Msg("caching media item")
	return c.set(ctx, KeyMediaItem+item.ID, item, c.config.MediaItemTTL)
}

// InvalidateMediaItem removes a media item from cache.
func (c *Cache) InvalidateMediaItem(ctx context.Context, mediaID string) error {
	c.logger.Debug().Str("media_id", mediaID).Msg("invalidating media item cache")
	return c.delete(ctx, KeyMediaItem+mediaID)
}

// Clock caching methods

// CachedClock represents a cached clock definition.
type CachedClock struct {
	ID          string `json:"id"`
	StationID   string `json:"station_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Active      bool   `json:"active"`
}

// CachedClockHour represents a cached clock hour with slots.
type CachedClockHour struct {
	ID        string            `json:"id"`
	StationID string            `json:"station_id"`
	Hour      int               `json:"hour"`
	DayOfWeek int               `json:"day_of_week"`
	ClockID   string            `json:"clock_id"`
	Slots     []CachedClockSlot `json:"slots"`
}

// CachedClockSlot represents a cached clock slot.
type CachedClockSlot struct {
	ID       string         `json:"id"`
	Position int            `json:"position"`
	Type     string         `json:"type"`
	Duration int64          `json:"duration"` // Nanoseconds
	Payload  map[string]any `json:"payload"`
}

// GetClock retrieves a cached clock by ID.
func (c *Cache) GetClock(ctx context.Context, clockID string) (*CachedClock, bool) {
	var clock CachedClock
	found, err := c.get(ctx, KeyClock+clockID, &clock)
	if err != nil || !found {
		return nil, false
	}
	c.logger.Debug().Str("clock_id", clockID).Msg("clock cache hit")
	return &clock, true
}

// SetClock caches a clock.
func (c *Cache) SetClock(ctx context.Context, clock *CachedClock) error {
	c.logger.Debug().Str("clock_id", clock.ID).Msg("caching clock")
	return c.set(ctx, KeyClock+clock.ID, clock, c.config.ClockTTL)
}

// InvalidateClock removes a clock from cache.
func (c *Cache) InvalidateClock(ctx context.Context, clockID string) error {
	c.logger.Debug().Str("clock_id", clockID).Msg("invalidating clock cache")
	return c.delete(ctx, KeyClock+clockID)
}

// GetClockHours retrieves cached clock hours for a station.
func (c *Cache) GetClockHours(ctx context.Context, stationID string) ([]CachedClockHour, bool) {
	var hours []CachedClockHour
	found, err := c.get(ctx, KeyClockHours+stationID, &hours)
	if err != nil || !found {
		return nil, false
	}
	c.logger.Debug().Str("station_id", stationID).Int("count", len(hours)).Msg("clock hours cache hit")
	return hours, true
}

// SetClockHours caches clock hours for a station.
func (c *Cache) SetClockHours(ctx context.Context, stationID string, hours []CachedClockHour) error {
	c.logger.Debug().Str("station_id", stationID).Int("count", len(hours)).Msg("caching clock hours")
	return c.set(ctx, KeyClockHours+stationID, hours, c.config.ClockTTL)
}

// InvalidateClockHours removes clock hours cache for a station.
func (c *Cache) InvalidateClockHours(ctx context.Context, stationID string) error {
	c.logger.Debug().Str("station_id", stationID).Msg("invalidating clock hours cache")
	return c.delete(ctx, KeyClockHours+stationID)
}

// Bulk invalidation methods

// InvalidateStation removes all caches related to a station.
func (c *Cache) InvalidateStation(ctx context.Context, stationID string) error {
	c.logger.Debug().Str("station_id", stationID).Msg("invalidating all station caches")

	// Invalidate station list
	if err := c.InvalidateStationList(ctx); err != nil {
		return err
	}

	// Invalidate mounts
	if err := c.InvalidateMounts(ctx, stationID); err != nil {
		return err
	}

	// Invalidate clock hours
	if err := c.InvalidateClockHours(ctx, stationID); err != nil {
		return err
	}

	return nil
}

// FlushAll removes all cached data (use sparingly).
func (c *Cache) FlushAll(ctx context.Context) error {
	c.logger.Warn().Msg("flushing all cache data")
	return c.deletePattern(ctx, "grimnir:cache:*")
}
