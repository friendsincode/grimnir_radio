package clock

import "time"

// SlotPlan describes a planned slot for the scheduler.
type SlotPlan struct {
	SlotID   string
	StartsAt time.Time
	EndsAt   time.Time
	Duration time.Duration
	SlotType string
	Payload  map[string]any
}

// Compiler produces slot plans.
type Compiler interface {
	Compile(stationID string, start time.Time, horizon time.Duration) ([]SlotPlan, error)
}
