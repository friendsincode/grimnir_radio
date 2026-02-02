/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package events

import "sync"

// EventType enumerates event categories.
type EventType string

const (
	EventNowPlaying         EventType = "now_playing"
	EventHealth             EventType = "health"
	EventListenerStats      EventType = "listener_stats"
	EventDJConnect          EventType = "dj_connect"
	EventDJDisconnect       EventType = "dj_disconnect"
	EventScheduleUpdate     EventType = "schedule_update"
	EventPriorityEmergency  EventType = "priority.emergency"
	EventPriorityOverride   EventType = "priority.override"
	EventPriorityReleased   EventType = "priority.released"
	EventPriorityChange     EventType = "priority.change"
	EventLiveHandover       EventType = "live.handover"
	EventLiveReleased       EventType = "live.released"
	EventWebstreamFailover  EventType = "webstream.failover"
	EventWebstreamRecovered EventType = "webstream.recovered"
	EventMigration          EventType = "migration"

	// Cache invalidation events
	EventStationUpdated    EventType = "cache.station_updated"
	EventStationCreated    EventType = "cache.station_created"
	EventStationDeleted    EventType = "cache.station_deleted"
	EventMountUpdated      EventType = "cache.mount_updated"
	EventMountCreated      EventType = "cache.mount_created"
	EventMountDeleted      EventType = "cache.mount_deleted"
	EventSmartBlockUpdated EventType = "cache.smartblock_updated"
	EventSmartBlockDeleted EventType = "cache.smartblock_deleted"
	EventClockUpdated      EventType = "cache.clock_updated"
	EventClockDeleted      EventType = "cache.clock_deleted"
	EventMediaUpdated      EventType = "cache.media_updated"
	EventMediaDeleted      EventType = "cache.media_deleted"
	EventAnalysisComplete  EventType = "cache.analysis_complete"

	// Show transition events
	EventShowStart EventType = "show.start"
	EventShowEnd   EventType = "show.end"

	// Audit events (for operations that need explicit audit logging)
	EventAuditAPIKeyCreate    EventType = "audit.apikey.create"
	EventAuditAPIKeyRevoke    EventType = "audit.apikey.revoke"
	EventAuditWebstreamCreate EventType = "audit.webstream.create"
	EventAuditWebstreamUpdate EventType = "audit.webstream.update"
	EventAuditWebstreamDelete EventType = "audit.webstream.delete"
	EventAuditScheduleRefresh EventType = "audit.schedule.refresh"
	EventAuditStationCreate   EventType = "audit.station.create"
)

// Payload generic event payload.
type Payload map[string]any

// Subscriber receives event payloads.
type Subscriber chan Payload

// Bus implements a simple in-process pubsub.
type Bus struct {
	mu   sync.RWMutex
	subs map[EventType][]Subscriber
}

// NewBus creates an event bus.
func NewBus() *Bus {
	return &Bus{subs: make(map[EventType][]Subscriber)}
}

// Subscribe registers a subscriber for event type.
func (b *Bus) Subscribe(eventType EventType) Subscriber {
	ch := make(Subscriber, 8)
	b.mu.Lock()
	b.subs[eventType] = append(b.subs[eventType], ch)
	b.mu.Unlock()
	return ch
}

// Publish sends payload to subscribers.
func (b *Bus) Publish(eventType EventType, payload Payload) {
	b.mu.RLock()
	subs := append([]Subscriber(nil), b.subs[eventType]...)
	b.mu.RUnlock()
	for _, sub := range subs {
		select {
		case sub <- payload:
		default:
		}
	}
}

// Unsubscribe removes the subscriber.
func (b *Bus) Unsubscribe(eventType EventType, sub Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[eventType]
	for i, candidate := range subs {
		if candidate == sub {
			subs = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	b.subs[eventType] = subs
	close(sub)
}
