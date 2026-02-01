/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

// RedisBus implements a Redis-backed event bus for distributed systems.
type RedisBus struct {
	client   *redis.Client
	pubsub   *redis.PubSub
	logger   zerolog.Logger
	fallback *events.Bus
	nodeID   string

	mu       sync.RWMutex
	subs     map[events.EventType][]events.Subscriber
	channels map[events.EventType]*redis.PubSub

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Circuit breaker state
	useFallback bool
	failCount   int
	maxFails    int
	lastCheck   time.Time
}

// RedisConfig contains Redis connection configuration.
type RedisConfig struct {
	Addr     string
	Password string
	DB       int

	// Connection pooling
	PoolSize     int
	MinIdleConns int

	// Timeouts
	DialTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration

	// Circuit breaker
	MaxFailures   int
	CheckInterval time.Duration
}

// DefaultRedisConfig returns default Redis configuration.
func DefaultRedisConfig() RedisConfig {
	return RedisConfig{
		Addr:          "localhost:6379",
		PoolSize:      10,
		MinIdleConns:  2,
		DialTimeout:   5 * time.Second,
		ReadTimeout:   3 * time.Second,
		WriteTimeout:  3 * time.Second,
		MaxFailures:   5,
		CheckInterval: 30 * time.Second,
	}
}

// NewRedisBus creates a Redis-backed event bus.
// Falls back to in-memory bus if Redis is unavailable (circuit breaker pattern).
func NewRedisBus(cfg RedisConfig, nodeID string, logger zerolog.Logger) (*RedisBus, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     cfg.PoolSize,
		MinIdleConns: cfg.MinIdleConns,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	})

	// Test connection
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	defer pingCancel()

	if err := client.Ping(pingCtx).Err(); err != nil {
		logger.Warn().Err(err).Msg("Redis connection failed, using in-memory fallback")
		cancel()

		return &RedisBus{
			logger:      logger,
			fallback:    events.NewBus(),
			nodeID:      nodeID,
			useFallback: true,
			maxFails:    cfg.MaxFailures,
			subs:        make(map[events.EventType][]events.Subscriber),
			channels:    make(map[events.EventType]*redis.PubSub),
			ctx:         context.Background(),
		}, nil
	}

	rb := &RedisBus{
		client:      client,
		logger:      logger,
		fallback:    events.NewBus(),
		nodeID:      nodeID,
		maxFails:    cfg.MaxFailures,
		subs:        make(map[events.EventType][]events.Subscriber),
		channels:    make(map[events.EventType]*redis.PubSub),
		ctx:         ctx,
		cancel:      cancel,
		useFallback: false,
	}

	logger.Info().Str("addr", cfg.Addr).Msg("Redis event bus initialized")

	return rb, nil
}

// Subscribe registers a subscriber for an event type.
func (rb *RedisBus) Subscribe(eventType events.EventType) events.Subscriber {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Create subscriber channel
	sub := make(events.Subscriber, 100)

	// Track subscriber
	rb.subs[eventType] = append(rb.subs[eventType], sub)

	// If using fallback, delegate to in-memory bus
	if rb.useFallback {
		return rb.fallback.Subscribe(eventType)
	}

	// Check if we already have a Redis subscription for this event type
	if _, exists := rb.channels[eventType]; !exists {
		// Create new Redis pub/sub channel
		pubsub := rb.client.Subscribe(rb.ctx, string(eventType))
		rb.channels[eventType] = pubsub

		// Start goroutine to receive messages
		rb.wg.Add(1)
		go rb.receiveMessages(eventType, pubsub)
	}

	return sub
}

// receiveMessages handles incoming Redis pub/sub messages.
func (rb *RedisBus) receiveMessages(eventType events.EventType, pubsub *redis.PubSub) {
	defer rb.wg.Done()

	ch := pubsub.Channel()

	rb.logger.Debug().Str("event_type", string(eventType)).Msg("started Redis message receiver")

	for {
		select {
		case <-rb.ctx.Done():
			rb.logger.Debug().Str("event_type", string(eventType)).Msg("stopping Redis message receiver")
			return

		case msg, ok := <-ch:
			if !ok {
				rb.logger.Warn().Str("event_type", string(eventType)).Msg("Redis channel closed")
				rb.handleFailure()
				return
			}

			// Unmarshal message
			redisMsg, err := unmarshalMessage([]byte(msg.Payload))
			if err != nil {
				rb.logger.Error().Err(err).Msg("failed to unmarshal Redis message")
				continue
			}

			// Skip messages from ourselves (prevent echo)
			if redisMsg.NodeID == rb.nodeID {
				continue
			}

			// Deliver to local subscribers
			rb.mu.RLock()
			subs := rb.subs[eventType]
			rb.mu.RUnlock()

			for _, sub := range subs {
				select {
				case sub <- redisMsg.Payload:
				default:
					rb.logger.Warn().Str("event_type", string(eventType)).Msg("subscriber channel full, dropping event")
				}
			}

			rb.logger.Debug().
				Str("event_type", string(eventType)).
				Str("source_node", redisMsg.NodeID).
				Msg("delivered Redis event to local subscribers")
		}
	}
}

// Publish sends an event payload to all subscribers (local and remote).
func (rb *RedisBus) Publish(eventType events.EventType, payload events.Payload) {
	// Always publish locally via fallback (for same-node subscribers)
	rb.fallback.Publish(eventType, payload)

	// If using fallback circuit breaker, don't try Redis
	if rb.useFallback {
		return
	}

	// Marshal message
	data, err := marshalMessage(eventType, payload, rb.nodeID)
	if err != nil {
		rb.logger.Error().Err(err).Msg("failed to marshal Redis message")
		return
	}

	// Publish to Redis
	ctx, cancel := context.WithTimeout(rb.ctx, 2*time.Second)
	defer cancel()

	if err := rb.client.Publish(ctx, string(eventType), data).Err(); err != nil {
		rb.logger.Error().Err(err).Str("event_type", string(eventType)).Msg("failed to publish to Redis")
		rb.handleFailure()
		return
	}

	// Reset failure count on success
	rb.mu.Lock()
	rb.failCount = 0
	rb.mu.Unlock()

	rb.logger.Debug().
		Str("event_type", string(eventType)).
		Str("node_id", rb.nodeID).
		Msg("published event to Redis")
}

// Unsubscribe removes a subscriber.
func (rb *RedisBus) Unsubscribe(eventType events.EventType, sub events.Subscriber) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	// Remove from tracking
	subs := rb.subs[eventType]
	for i, s := range subs {
		if s == sub {
			rb.subs[eventType] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	// Close subscriber channel
	close(sub)

	// Delegate to fallback
	rb.fallback.Unsubscribe(eventType, sub)

	// If no more subscribers, close Redis subscription
	if len(rb.subs[eventType]) == 0 {
		if pubsub, exists := rb.channels[eventType]; exists {
			pubsub.Close()
			delete(rb.channels, eventType)
			rb.logger.Debug().Str("event_type", string(eventType)).Msg("closed Redis subscription")
		}
	}
}

// Close closes the Redis connection and all subscriptions.
func (rb *RedisBus) Close() error {
	rb.logger.Info().Msg("closing Redis event bus")

	// Cancel context to stop all goroutines
	if rb.cancel != nil {
		rb.cancel()
	}

	// Wait for all receivers to finish
	rb.wg.Wait()

	// Close all pub/sub channels
	rb.mu.Lock()
	for eventType, pubsub := range rb.channels {
		pubsub.Close()
		rb.logger.Debug().Str("event_type", string(eventType)).Msg("closed Redis pub/sub")
	}
	rb.channels = make(map[events.EventType]*redis.PubSub)
	rb.mu.Unlock()

	// Close Redis client
	if rb.client != nil {
		if err := rb.client.Close(); err != nil {
			rb.logger.Error().Err(err).Msg("failed to close Redis client")
			return err
		}
	}

	rb.logger.Info().Msg("Redis event bus closed")
	return nil
}

// handleFailure implements circuit breaker logic.
func (rb *RedisBus) handleFailure() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.failCount++

	if rb.failCount >= rb.maxFails && !rb.useFallback {
		rb.logger.Warn().
			Int("fail_count", rb.failCount).
			Msg("Redis failure threshold reached, switching to in-memory fallback")

		rb.useFallback = true
		rb.lastCheck = time.Now()

		// Close Redis client
		if rb.client != nil {
			rb.client.Close()
		}
	}
}

// tryReconnect attempts to reconnect to Redis (called periodically).
func (rb *RedisBus) tryReconnect() error {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if !rb.useFallback {
		return nil
	}

	// Check if enough time has passed since last check
	if time.Since(rb.lastCheck) < 30*time.Second {
		return fmt.Errorf("too soon to retry")
	}

	rb.lastCheck = time.Now()

	// Try to ping Redis
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rb.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("Redis still unavailable: %w", err)
	}

	// Success! Re-enable Redis
	rb.useFallback = false
	rb.failCount = 0

	rb.logger.Info().Msg("reconnected to Redis, disabling fallback")

	return nil
}

// redisMessage represents a message published to Redis.
type redisMessage struct {
	EventType events.EventType `json:"event_type"`
	Payload   events.Payload   `json:"payload"`
	Timestamp time.Time        `json:"timestamp"`
	NodeID    string           `json:"node_id"` // For identifying source node
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
