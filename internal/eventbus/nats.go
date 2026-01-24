/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/rs/zerolog"
)

// NATSBus implements a NATS-backed event bus with JetStream persistence.
type NATSBus struct {
	conn     *nats.Conn
	js       jetstream.JetStream
	logger   zerolog.Logger
	fallback *events.Bus
	nodeID   string

	mu       sync.RWMutex
	subs     map[events.EventType][]events.Subscriber
	natsSubs map[events.EventType]jetstream.Consumer

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// Circuit breaker state
	useFallback bool
	failCount   int
	maxFails    int
}

// NATSConfig contains NATS connection configuration.
type NATSConfig struct {
	URL   string
	Token string

	// JetStream configuration
	StreamName string
	Durable    string

	// Connection options
	MaxReconnects int
	ReconnectWait time.Duration
	Timeout       time.Duration

	// Circuit breaker
	MaxFailures int
}

// DefaultNATSConfig returns default NATS configuration.
func DefaultNATSConfig() NATSConfig {
	return NATSConfig{
		URL:           "nats://localhost:4222",
		StreamName:    "GRIMNIR_EVENTS",
		Durable:       "grimnir-consumer",
		MaxReconnects: -1, // Unlimited
		ReconnectWait: 2 * time.Second,
		Timeout:       5 * time.Second,
		MaxFailures:   5,
	}
}

// NewNATSBus creates a NATS-backed event bus with JetStream.
// Falls back to in-memory bus if NATS is unavailable.
func NewNATSBus(cfg NATSConfig, nodeID string, logger zerolog.Logger) (*NATSBus, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Connect to NATS
	opts := []nats.Option{
		nats.Name(fmt.Sprintf("grimnir-radio-%s", nodeID)),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.Timeout(cfg.Timeout),
		nats.DisconnectErrHandler(func(nc *nats.Conn, err error) {
			if err != nil {
				logger.Warn().Err(err).Msg("NATS disconnected")
			}
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info().Str("url", nc.ConnectedUrl()).Msg("NATS reconnected")
		}),
	}

	if cfg.Token != "" {
		opts = append(opts, nats.Token(cfg.Token))
	}

	conn, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		logger.Warn().Err(err).Msg("NATS connection failed, using in-memory fallback")
		cancel()

		return &NATSBus{
			logger:      logger,
			fallback:    events.NewBus(),
			nodeID:      nodeID,
			useFallback: true,
			maxFails:    cfg.MaxFailures,
			subs:        make(map[events.EventType][]events.Subscriber),
			natsSubs:    make(map[events.EventType]jetstream.Consumer),
			ctx:         context.Background(),
		}, nil
	}

	// Create JetStream context
	js, err := jetstream.New(conn)
	if err != nil {
		logger.Warn().Err(err).Msg("JetStream initialization failed, using in-memory fallback")
		conn.Close()
		cancel()

		return &NATSBus{
			logger:      logger,
			fallback:    events.NewBus(),
			nodeID:      nodeID,
			useFallback: true,
			maxFails:    cfg.MaxFailures,
			subs:        make(map[events.EventType][]events.Subscriber),
			natsSubs:    make(map[events.EventType]jetstream.Consumer),
			ctx:         context.Background(),
		}, nil
	}

	// Create or update stream
	if err := createOrUpdateStream(ctx, js, cfg.StreamName); err != nil {
		logger.Warn().Err(err).Msg("failed to create JetStream stream, using in-memory fallback")
		conn.Close()
		cancel()

		return &NATSBus{
			logger:      logger,
			fallback:    events.NewBus(),
			nodeID:      nodeID,
			useFallback: true,
			maxFails:    cfg.MaxFailures,
			subs:        make(map[events.EventType][]events.Subscriber),
			natsSubs:    make(map[events.EventType]jetstream.Consumer),
			ctx:         context.Background(),
		}, nil
	}

	nb := &NATSBus{
		conn:        conn,
		js:          js,
		logger:      logger,
		fallback:    events.NewBus(),
		nodeID:      nodeID,
		maxFails:    cfg.MaxFailures,
		subs:        make(map[events.EventType][]events.Subscriber),
		natsSubs:    make(map[events.EventType]jetstream.Consumer),
		ctx:         ctx,
		cancel:      cancel,
		useFallback: false,
	}

	logger.Info().Str("url", cfg.URL).Str("stream", cfg.StreamName).Msg("NATS event bus initialized")

	return nb, nil
}

// createOrUpdateStream creates or updates the JetStream stream.
func createOrUpdateStream(ctx context.Context, js jetstream.JetStream, streamName string) error {
	streamCfg := jetstream.StreamConfig{
		Name:        streamName,
		Subjects:    []string{"grimnir.events.>"},
		Retention:   jetstream.WorkQueuePolicy,
		MaxAge:      24 * time.Hour,
		Storage:     jetstream.FileStorage,
		Replicas:    1,
		Description: "Grimnir Radio event bus",
	}

	// Try to get existing stream
	_, err := js.Stream(ctx, streamName)
	if err != nil {
		// Stream doesn't exist, create it
		_, err = js.CreateStream(ctx, streamCfg)
		if err != nil {
			return fmt.Errorf("create stream: %w", err)
		}
	} else {
		// Stream exists, update it
		_, err = js.UpdateStream(ctx, streamCfg)
		if err != nil {
			return fmt.Errorf("update stream: %w", err)
		}
	}

	return nil
}

// Subscribe registers a subscriber for an event type.
func (nb *NATSBus) Subscribe(eventType events.EventType) events.Subscriber {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	// Create subscriber channel
	sub := make(events.Subscriber, 100)

	// Track subscriber
	nb.subs[eventType] = append(nb.subs[eventType], sub)

	// If using fallback, delegate to in-memory bus
	if nb.useFallback {
		return nb.fallback.Subscribe(eventType)
	}

	// Check if we already have a NATS consumer for this event type
	if _, exists := nb.natsSubs[eventType]; !exists {
		// Create durable consumer
		subject := fmt.Sprintf("grimnir.events.%s", eventType)
		consumerName := fmt.Sprintf("%s-%s", nb.nodeID, eventType)

		consumer, err := nb.js.CreateOrUpdateConsumer(nb.ctx, "GRIMNIR_EVENTS", jetstream.ConsumerConfig{
			Name:          consumerName,
			Durable:       consumerName,
			FilterSubject: subject,
			AckPolicy:     jetstream.AckExplicitPolicy,
			DeliverPolicy: jetstream.DeliverNewPolicy,
		})

		if err != nil {
			nb.logger.Error().Err(err).Str("event_type", string(eventType)).Msg("failed to create NATS consumer")
			nb.handleFailure()
			return nb.fallback.Subscribe(eventType)
		}

		nb.natsSubs[eventType] = consumer

		// Start goroutine to receive messages
		nb.wg.Add(1)
		go nb.receiveMessages(eventType, consumer)
	}

	return sub
}

// receiveMessages handles incoming NATS messages.
func (nb *NATSBus) receiveMessages(eventType events.EventType, consumer jetstream.Consumer) {
	defer nb.wg.Done()

	nb.logger.Debug().Str("event_type", string(eventType)).Msg("started NATS message receiver")

	// Consume messages
	msgs, err := consumer.Messages()
	if err != nil {
		nb.logger.Error().Err(err).Str("event_type", string(eventType)).Msg("failed to consume messages")
		nb.handleFailure()
		return
	}
	defer msgs.Stop()

	for {
		select {
		case <-nb.ctx.Done():
			nb.logger.Debug().Str("event_type", string(eventType)).Msg("stopping NATS message receiver")
			return

		default:
			// Fetch next message with timeout
			msg, err := msgs.Next()
			if err != nil {
				if err == jetstream.ErrMsgIteratorClosed {
					nb.logger.Warn().Str("event_type", string(eventType)).Msg("NATS message iterator closed")
					return
				}
				// Timeout or no messages, continue
				continue
			}

			// Unmarshal message
			natsMsg, err := unmarshalNATSMessage(msg.Data())
			if err != nil {
				nb.logger.Error().Err(err).Msg("failed to unmarshal NATS message")
				msg.Nak()
				continue
			}

			// Skip messages from ourselves (prevent echo)
			if natsMsg.NodeID == nb.nodeID {
				msg.Ack()
				continue
			}

			// Deliver to local subscribers
			nb.mu.RLock()
			subs := nb.subs[eventType]
			nb.mu.RUnlock()

			delivered := false
			for _, sub := range subs {
				select {
				case sub <- natsMsg.Payload:
					delivered = true
				default:
					nb.logger.Warn().Str("event_type", string(eventType)).Msg("subscriber channel full, dropping event")
				}
			}

			if delivered {
				msg.Ack()
				nb.logger.Debug().
					Str("event_type", string(eventType)).
					Str("source_node", natsMsg.NodeID).
					Msg("delivered NATS event to local subscribers")
			} else {
				msg.Nak()
			}
		}
	}
}

// Publish sends an event payload to all subscribers (local and remote).
func (nb *NATSBus) Publish(eventType events.EventType, payload events.Payload) {
	// Always publish locally via fallback (for same-node subscribers)
	nb.fallback.Publish(eventType, payload)

	// If using fallback circuit breaker, don't try NATS
	if nb.useFallback {
		return
	}

	// Marshal message
	data, err := marshalNATSMessage(eventType, payload, nb.nodeID)
	if err != nil {
		nb.logger.Error().Err(err).Msg("failed to marshal NATS message")
		return
	}

	// Publish to NATS
	subject := fmt.Sprintf("grimnir.events.%s", eventType)

	ctx, cancel := context.WithTimeout(nb.ctx, 2*time.Second)
	defer cancel()

	if _, err := nb.js.Publish(ctx, subject, data); err != nil {
		nb.logger.Error().Err(err).Str("event_type", string(eventType)).Msg("failed to publish to NATS")
		nb.handleFailure()
		return
	}

	// Reset failure count on success
	nb.mu.Lock()
	nb.failCount = 0
	nb.mu.Unlock()

	nb.logger.Debug().
		Str("event_type", string(eventType)).
		Str("node_id", nb.nodeID).
		Msg("published event to NATS")
}

// Unsubscribe removes a subscriber.
func (nb *NATSBus) Unsubscribe(eventType events.EventType, sub events.Subscriber) {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	// Remove from tracking
	subs := nb.subs[eventType]
	for i, s := range subs {
		if s == sub {
			nb.subs[eventType] = append(subs[:i], subs[i+1:]...)
			break
		}
	}

	// Close subscriber channel
	close(sub)

	// Delegate to fallback
	nb.fallback.Unsubscribe(eventType, sub)

	// If no more subscribers, we can optionally delete the consumer
	// For now, keep it for durability
}

// Close closes the NATS connection.
func (nb *NATSBus) Close() error {
	nb.logger.Info().Msg("closing NATS event bus")

	// Cancel context to stop all goroutines
	if nb.cancel != nil {
		nb.cancel()
	}

	// Wait for all receivers to finish
	nb.wg.Wait()

	// Close NATS connection
	if nb.conn != nil {
		nb.conn.Close()
	}

	nb.logger.Info().Msg("NATS event bus closed")
	return nil
}

// handleFailure implements circuit breaker logic.
func (nb *NATSBus) handleFailure() {
	nb.mu.Lock()
	defer nb.mu.Unlock()

	nb.failCount++

	if nb.failCount >= nb.maxFails && !nb.useFallback {
		nb.logger.Warn().
			Int("fail_count", nb.failCount).
			Msg("NATS failure threshold reached, switching to in-memory fallback")

		nb.useFallback = true

		// Close NATS connection
		if nb.conn != nil {
			nb.conn.Close()
		}
	}
}

// natsMessage represents a message published to NATS.
type natsMessage struct {
	EventType events.EventType `json:"event_type"`
	Payload   events.Payload   `json:"payload"`
	Timestamp time.Time        `json:"timestamp"`
	NodeID    string           `json:"node_id"`
	MessageID string           `json:"message_id"` // For deduplication
}

// marshalNATSMessage converts payload to NATS message format.
func marshalNATSMessage(eventType events.EventType, payload events.Payload, nodeID string) ([]byte, error) {
	msg := natsMessage{
		EventType: eventType,
		Payload:   payload,
		Timestamp: time.Now(),
		NodeID:    nodeID,
		MessageID: uuid.New().String(),
	}
	return json.Marshal(msg)
}

// unmarshalNATSMessage parses a NATS message.
func unmarshalNATSMessage(data []byte) (*natsMessage, error) {
	var msg natsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal nats message: %w", err)
	}
	return &msg, nil
}

// generateNodeID creates a unique node identifier.
func generateNodeID() string {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	return fmt.Sprintf("%s-%s", hostname, uuid.New().String()[:8])
}
