package eventbus

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/friendsincode/grimnir_radio/internal/events"
	"github.com/rs/zerolog"
)

// NATSBus implements a NATS-backed event bus with JetStream persistence.
// This is a placeholder implementation that will be completed when NATS dependency is added.
type NATSBus struct {
	logger   zerolog.Logger
	fallback *events.Bus // In-memory fallback
	mu       sync.RWMutex
	subs     map[events.EventType][]events.Subscriber
	nodeID   string
}

// NewNATSBus creates a NATS-backed event bus.
// Falls back to in-memory bus if NATS is unavailable.
func NewNATSBus(natsURL string, logger zerolog.Logger) (*NATSBus, error) {
	// TODO: Implement actual NATS connection when nats library is added
	// For now, use in-memory fallback
	logger.Warn().Msg("NATS not yet implemented, using in-memory event bus fallback")

	return &NATSBus{
		logger:   logger,
		fallback: events.NewBus(),
		subs:     make(map[events.EventType][]events.Subscriber),
		nodeID:   generateNodeID(),
	}, nil
}

// Subscribe registers a subscriber for an event type.
func (nb *NATSBus) Subscribe(eventType events.EventType) events.Subscriber {
	// Currently uses fallback
	return nb.fallback.Subscribe(eventType)
}

// Publish sends an event payload to all subscribers.
func (nb *NATSBus) Publish(eventType events.EventType, payload events.Payload) {
	// Currently uses fallback
	nb.fallback.Publish(eventType, payload)

	// TODO: Publish to NATS subject when implemented
	// This will look like:
	// subject := fmt.Sprintf("grimnir.events.%s", eventType)
	// data, _ := json.Marshal(natsMessage{...})
	// nb.natsConn.Publish(subject, data)
}

// Unsubscribe removes a subscriber.
func (nb *NATSBus) Unsubscribe(eventType events.EventType, sub events.Subscriber) {
	nb.fallback.Unsubscribe(eventType, sub)
}

// Close closes the NATS connection.
func (nb *NATSBus) Close() error {
	// TODO: Close NATS connection when implemented
	return nil
}

// NATSConfig contains NATS connection configuration.
type NATSConfig struct {
	URL          string
	Token        string
	// JetStream configuration
	StreamName   string
	Durable      string
	// Connection options
	MaxReconnects int
	ReconnectWait time.Duration
	Timeout       time.Duration
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
		MessageID: generateMessageID(),
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

func generateNodeID() string {
	// TODO: Generate unique node ID (hostname + UUID)
	return "node-placeholder"
}

func generateMessageID() string {
	// TODO: Generate unique message ID for deduplication
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// NOTE: Full NATS implementation will be added in a future commit when:
// 1. github.com/nats-io/nats.go dependency is added to go.mod
// 2. NATS connection management is implemented
// 3. JetStream stream and consumer setup is added
// 4. Subscription handlers are created
// 5. Message acknowledgment and retry logic is added
//
// Implementation sketch:
// - Use NATS JetStream for persistent, ordered event delivery
// - Subject pattern: "grimnir.events.{event_type}"
// - Stream: GRIMNIR_EVENTS with retention policy
// - Consumer: Durable consumer with acknowledgment
// - Automatic reconnection with backoff
// - Fallback to in-memory bus on connection failures
//
// Advantages of NATS over Redis:
// - Better message ordering guarantees
// - Built-in message persistence with JetStream
// - Lower latency for pub/sub
// - Better cluster support
