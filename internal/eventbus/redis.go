/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package eventbus

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

// RedisBus implements a Redis-backed event bus for distributed systems.
// This is a placeholder implementation that will be completed when Redis dependency is added.
type RedisBus struct {
	logger   zerolog.Logger
	fallback *events.Bus // In-memory fallback
	mu       sync.RWMutex
	subs     map[events.EventType][]events.Subscriber
}

// NewRedisBus creates a Redis-backed event bus.
// Falls back to in-memory bus if Redis is unavailable.
func NewRedisBus(redisURL string, logger zerolog.Logger) (*RedisBus, error) {
	// TODO: Implement actual Redis connection when redis library is added
	// For now, use in-memory fallback
	logger.Warn().Msg("Redis not yet implemented, using in-memory event bus fallback")

	return &RedisBus{
		logger:   logger,
		fallback: events.NewBus(),
		subs:     make(map[events.EventType][]events.Subscriber),
	}, nil
}

// Subscribe registers a subscriber for an event type.
func (rb *RedisBus) Subscribe(eventType events.EventType) events.Subscriber {
	// Currently uses fallback
	return rb.fallback.Subscribe(eventType)
}

// Publish sends an event payload to all subscribers.
func (rb *RedisBus) Publish(eventType events.EventType, payload events.Payload) {
	// Currently uses fallback
	rb.fallback.Publish(eventType, payload)

	// TODO: Publish to Redis pub/sub when implemented
	// This will look like:
	// data, _ := json.Marshal(payload)
	// rb.redisClient.Publish(ctx, string(eventType), data)
}

// Unsubscribe removes a subscriber.
func (rb *RedisBus) Unsubscribe(eventType events.EventType, sub events.Subscriber) {
	rb.fallback.Unsubscribe(eventType, sub)
}

// Close closes the Redis connection.
func (rb *RedisBus) Close() error {
	// TODO: Close Redis connection when implemented
	return nil
}

// RedisConfig contains Redis connection configuration.
type RedisConfig struct {
	URL      string
	Password string
	DB       int
	// Connection pooling
	PoolSize     int
	MinIdleConns int
	// Timeouts
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// DefaultRedisConfig returns default Redis configuration.
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		URL:          "redis://localhost:6379",
		PoolSize:     10,
		MinIdleConns: 2,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	}
}

// redisMessage represents a message published to Redis.
type redisMessage struct {
	EventType events.EventType   `json:"event_type"`
	Payload   events.Payload     `json:"payload"`
	Timestamp time.Time          `json:"timestamp"`
	NodeID    string             `json:"node_id"` // For identifying source node
}

// marshalMessage converts payload to Redis message format.
func marshalMessage(eventType events.EventType, payload events.Payload, nodeID string) ([]byte, error) {
	msg := redisMessage{
		EventType: eventType,
		Payload:   payload,
		Timestamp: time.Now(),
		NodeID:    nodeID,
	}
	return json.Marshal(msg)
}

// unmarshalMessage parses a Redis message.
func unmarshalMessage(data []byte) (*redisMessage, error) {
	var msg redisMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal redis message: %w", err)
	}
	return &msg, nil
}

// NOTE: Full Redis implementation will be added in a future commit when:
// 1. github.com/redis/go-redis dependency is added to go.mod
// 2. Redis connection management is implemented
// 3. Pub/Sub goroutines are created for distributed event delivery
// 4. Proper error handling and reconnection logic is added
//
// Implementation sketch:
// - Use Redis PUBLISH for sending events
// - Use Redis SUBSCRIBE for receiving events
// - One goroutine per subscribed event type
// - Automatic reconnection with exponential backoff
// - Circuit breaker for Redis failures (fallback to in-memory)
