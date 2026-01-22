package priority

import (
	"testing"

	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/rs/zerolog"
)

func TestCanPreempt(t *testing.T) {
	resolver := NewResolver(nil, zerolog.Nop())

	tests := []struct {
		name         string
		current      *models.PrioritySource
		newPriority  models.PriorityLevel
		shouldPreempt bool
	}{
		{
			name:         "no current source, can activate",
			current:      nil,
			newPriority:  models.PriorityAutomation,
			shouldPreempt: true,
		},
		{
			name: "emergency preempts automation",
			current: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			newPriority:  models.PriorityEmergency,
			shouldPreempt: true,
		},
		{
			name: "live override preempts automation",
			current: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			newPriority:  models.PriorityLiveOverride,
			shouldPreempt: true,
		},
		{
			name: "automation cannot preempt live",
			current: &models.PrioritySource{
				Priority: models.PriorityLiveScheduled,
			},
			newPriority:  models.PriorityAutomation,
			shouldPreempt: false,
		},
		{
			name: "same priority cannot preempt",
			current: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			newPriority:  models.PriorityAutomation,
			shouldPreempt: false,
		},
		{
			name: "fallback cannot preempt automation",
			current: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			newPriority:  models.PriorityFallback,
			shouldPreempt: false,
		},
		{
			name: "emergency preempts live override",
			current: &models.PrioritySource{
				Priority: models.PriorityLiveOverride,
			},
			newPriority:  models.PriorityEmergency,
			shouldPreempt: true,
		},
		{
			name: "live scheduled preempts automation",
			current: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			newPriority:  models.PriorityLiveScheduled,
			shouldPreempt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.CanPreempt(tt.current, tt.newPriority)
			if result != tt.shouldPreempt {
				t.Errorf("CanPreempt() = %v, want %v", result, tt.shouldPreempt)
			}
		})
	}
}

func TestDetermineTransitionType(t *testing.T) {
	resolver := NewResolver(nil, zerolog.Nop())

	tests := []struct {
		name         string
		old          *models.PrioritySource
		new          *models.PrioritySource
		expectedType TransitionType
	}{
		{
			name: "no old source",
			old:  nil,
			new: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			expectedType: TransitionSwitch,
		},
		{
			name: "emergency transition",
			old: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			new: &models.PrioritySource{
				Priority: models.PriorityEmergency,
			},
			expectedType: TransitionEmergency,
		},
		{
			name: "higher priority preempts",
			old: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			new: &models.PrioritySource{
				Priority: models.PriorityLiveOverride,
			},
			expectedType: TransitionPreempt,
		},
		{
			name: "same priority switch",
			old: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			new: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			expectedType: TransitionSwitch,
		},
		{
			name: "lower priority is release",
			old: &models.PrioritySource{
				Priority: models.PriorityLiveScheduled,
			},
			new: &models.PrioritySource{
				Priority: models.PriorityAutomation,
			},
			expectedType: TransitionRelease,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.determineTransitionType(tt.old, tt.new)
			if result != tt.expectedType {
				t.Errorf("determineTransitionType() = %v, want %v", result, tt.expectedType)
			}
		})
	}
}

func TestRequiresFade(t *testing.T) {
	resolver := NewResolver(nil, zerolog.Nop())

	tests := []struct {
		name           string
		old            *models.PrioritySource
		new            *models.PrioritySource
		transitionType TransitionType
		expectedFade   bool
	}{
		{
			name:           "emergency no fade",
			old:            &models.PrioritySource{Priority: models.PriorityAutomation},
			new:            &models.PrioritySource{Priority: models.PriorityEmergency},
			transitionType: TransitionEmergency,
			expectedFade:   false,
		},
		{
			name:           "no old source no fade",
			old:            nil,
			new:            &models.PrioritySource{Priority: models.PriorityAutomation},
			transitionType: TransitionSwitch,
			expectedFade:   false,
		},
		{
			name:           "preempt requires fade",
			old:            &models.PrioritySource{Priority: models.PriorityAutomation},
			new:            &models.PrioritySource{Priority: models.PriorityLiveOverride},
			transitionType: TransitionPreempt,
			expectedFade:   true,
		},
		{
			name:           "switch requires fade",
			old:            &models.PrioritySource{Priority: models.PriorityAutomation},
			new:            &models.PrioritySource{Priority: models.PriorityAutomation},
			transitionType: TransitionSwitch,
			expectedFade:   true,
		},
		{
			name:           "release requires fade",
			old:            &models.PrioritySource{Priority: models.PriorityLiveScheduled},
			new:            &models.PrioritySource{Priority: models.PriorityAutomation},
			transitionType: TransitionRelease,
			expectedFade:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := resolver.requiresFade(tt.old, tt.new, tt.transitionType)
			if result != tt.expectedFade {
				t.Errorf("requiresFade() = %v, want %v", result, tt.expectedFade)
			}
		})
	}
}

func TestPriorityLevelString(t *testing.T) {
	tests := []struct {
		priority models.PriorityLevel
		expected string
	}{
		{models.PriorityEmergency, "Emergency"},
		{models.PriorityLiveOverride, "Live Override"},
		{models.PriorityLiveScheduled, "Live Scheduled"},
		{models.PriorityAutomation, "Automation"},
		{models.PriorityFallback, "Fallback"},
		{models.PriorityLevel(99), "Unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.priority.String()
			if result != tt.expected {
				t.Errorf("PriorityLevel.String() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestPrioritySourceIsActive(t *testing.T) {
	ps := &models.PrioritySource{
		Active: true,
		DeactivatedAt: nil,
	}

	if !ps.IsActive() {
		t.Error("IsActive() should return true for active source")
	}

	ps.Deactivate()

	if ps.IsActive() {
		t.Error("IsActive() should return false after deactivation")
	}

	if ps.DeactivatedAt == nil {
		t.Error("DeactivatedAt should be set after deactivation")
	}
}

func TestPrioritySourceIsEmergency(t *testing.T) {
	tests := []struct {
		priority   models.PriorityLevel
		isEmergency bool
	}{
		{models.PriorityEmergency, true},
		{models.PriorityLiveOverride, false},
		{models.PriorityAutomation, false},
	}

	for _, tt := range tests {
		t.Run(tt.priority.String(), func(t *testing.T) {
			ps := &models.PrioritySource{Priority: tt.priority}
			result := ps.IsEmergency()
			if result != tt.isEmergency {
				t.Errorf("IsEmergency() = %v, want %v", result, tt.isEmergency)
			}
		})
	}
}

func TestPrioritySourceIsLive(t *testing.T) {
	tests := []struct {
		name       string
		priority   models.PriorityLevel
		sourceType models.SourceType
		isLive     bool
	}{
		{"live override priority", models.PriorityLiveOverride, models.SourceTypeMedia, true},
		{"live scheduled priority", models.PriorityLiveScheduled, models.SourceTypeMedia, true},
		{"live source type", models.PriorityAutomation, models.SourceTypeLive, true},
		{"automation not live", models.PriorityAutomation, models.SourceTypeMedia, false},
		{"emergency not live", models.PriorityEmergency, models.SourceTypeEmergency, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ps := &models.PrioritySource{
				Priority:   tt.priority,
				SourceType: tt.sourceType,
			}
			result := ps.IsLive()
			if result != tt.isLive {
				t.Errorf("IsLive() = %v, want %v", result, tt.isLive)
			}
		})
	}
}
