package executor

import (
	"fmt"
	"testing"
)

func TestDistributor_Basic(t *testing.T) {
	dist := NewDistributor(500)

	// Add 3 instances
	dist.AddInstance("instance-1")
	dist.AddInstance("instance-2")
	dist.AddInstance("instance-3")

	// Test station assignment
	station1, err := dist.GetInstance("station-a")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	station2, err := dist.GetInstance("station-b")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	// Same station should always map to same instance
	station1Again, err := dist.GetInstance("station-a")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}

	if station1 != station1Again {
		t.Errorf("Station assignment not consistent: %s != %s", station1, station1Again)
	}

	// Different stations may map to different instances (not guaranteed, but likely)
	t.Logf("station-a → %s", station1)
	t.Logf("station-b → %s", station2)
}

func TestDistributor_Distribution(t *testing.T) {
	dist := NewDistributor(500)

	// Add 3 instances
	dist.AddInstance("instance-1")
	dist.AddInstance("instance-2")
	dist.AddInstance("instance-3")

	// Generate 300 station IDs (more stations for better distribution testing)
	stationIDs := make([]string, 300)
	for i := 0; i < 300; i++ {
		stationIDs[i] = fmt.Sprintf("station-%d", i)
	}

	// Count assignments per instance
	assignments := make(map[string]int)
	for _, stationID := range stationIDs {
		instanceID, err := dist.GetInstance(stationID)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}
		assignments[instanceID]++
	}

	// Each instance should get roughly 33% (±10% with more virtual nodes)
	expectedPerInstance := 100
	tolerance := 30 // Allow 30% variation

	for instanceID, count := range assignments {
		t.Logf("%s: %d stations (%.1f%%)", instanceID, count, float64(count)/3.0)
		if count < expectedPerInstance-tolerance || count > expectedPerInstance+tolerance {
			t.Errorf("%s has %d stations, expected ~%d (±%d)", instanceID, count, expectedPerInstance, tolerance)
		}
	}
}

func TestDistributor_AddInstance(t *testing.T) {
	dist := NewDistributor(500)

	// Start with 3 instances
	dist.AddInstance("instance-1")
	dist.AddInstance("instance-2")
	dist.AddInstance("instance-3")

	// Generate 100 station IDs
	stationIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		stationIDs[i] = fmt.Sprintf("station-%d", i)
	}

	// Get initial assignments
	beforeAssignments := dist.GetAllAssignments(stationIDs)

	// Add 4th instance
	dist.AddInstance("instance-4")

	// Get new assignments
	afterAssignments := dist.GetAllAssignments(stationIDs)

	// Count changes
	changes := 0
	for stationID, beforeInstance := range beforeAssignments {
		afterInstance := afterAssignments[stationID]
		if beforeInstance != afterInstance {
			changes++
		}
	}

	churnPct := float64(changes) / float64(len(stationIDs)) * 100
	t.Logf("Adding instance-4: %d/%d stations moved (%.1f%% churn)", changes, len(stationIDs), churnPct)

	// With consistent hashing, churn should be ~25% (1/4 of stations move to new instance)
	// Allow 5-40% range due to hash distribution variance
	if churnPct < 5 || churnPct > 40 {
		t.Errorf("Churn %.1f%% outside expected range (5-40%%)", churnPct)
	}
}

func TestDistributor_RemoveInstance(t *testing.T) {
	dist := NewDistributor(500)

	// Start with 4 instances
	dist.AddInstance("instance-1")
	dist.AddInstance("instance-2")
	dist.AddInstance("instance-3")
	dist.AddInstance("instance-4")

	// Generate 100 station IDs
	stationIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		stationIDs[i] = fmt.Sprintf("station-%d", i)
	}

	// Get initial assignments
	beforeAssignments := dist.GetAllAssignments(stationIDs)

	// Remove instance-3
	dist.RemoveInstance("instance-3")

	// Get new assignments
	afterAssignments := dist.GetAllAssignments(stationIDs)

	// Count changes
	changes := 0
	stationsFromRemoved := 0
	for stationID, beforeInstance := range beforeAssignments {
		afterInstance := afterAssignments[stationID]
		if beforeInstance == "instance-3" {
			stationsFromRemoved++
		}
		if beforeInstance != afterInstance {
			changes++
		}
	}

	churnPct := float64(changes) / float64(len(stationIDs)) * 100
	t.Logf("Removing instance-3: %d stations were on it, %d total moved (%.1f%% churn)",
		stationsFromRemoved, changes, churnPct)

	// All stations from removed instance should move
	if changes < stationsFromRemoved {
		t.Errorf("Only %d stations moved, but %d were on removed instance", changes, stationsFromRemoved)
	}

	// Churn should be roughly 25% (all stations from removed instance, which is 1/4 of total)
	// Allow 5-40% range due to hash distribution variance
	if churnPct < 5 || churnPct > 40 {
		t.Errorf("Churn %.1f%% outside expected range (5-40%%)", churnPct)
	}
}

func TestDistributor_GetInstanceStations(t *testing.T) {
	dist := NewDistributor(500)

	dist.AddInstance("instance-1")
	dist.AddInstance("instance-2")
	dist.AddInstance("instance-3")

	stationIDs := []string{"station-a", "station-b", "station-c", "station-d", "station-e"}

	// Get stations for instance-1
	instance1Stations := dist.GetInstanceStations("instance-1", stationIDs)

	t.Logf("instance-1 has %d stations: %v", len(instance1Stations), instance1Stations)

	// Verify all returned stations are actually assigned to instance-1
	for _, stationID := range instance1Stations {
		assignedInstance, err := dist.GetInstance(stationID)
		if err != nil {
			t.Fatalf("GetInstance failed: %v", err)
		}
		if assignedInstance != "instance-1" {
			t.Errorf("Station %s reported for instance-1 but assigned to %s", stationID, assignedInstance)
		}
	}
}

func TestDistributor_GetInstances(t *testing.T) {
	dist := NewDistributor(500)

	dist.AddInstance("instance-3")
	dist.AddInstance("instance-1")
	dist.AddInstance("instance-2")

	instances := dist.GetInstances()

	// Should return 3 instances
	if len(instances) != 3 {
		t.Errorf("Got %d instances, want 3", len(instances))
	}

	// Should be sorted
	expected := []string{"instance-1", "instance-2", "instance-3"}
	for i, instanceID := range instances {
		if instanceID != expected[i] {
			t.Errorf("instances[%d] = %s, want %s", i, instanceID, expected[i])
		}
	}
}

func TestDistributor_NoInstances(t *testing.T) {
	dist := NewDistributor(500)

	// No instances registered
	_, err := dist.GetInstance("station-a")
	if err == nil {
		t.Error("Expected error when no instances available, got nil")
	}
}

func TestDistributor_CalculateChurn(t *testing.T) {
	dist := NewDistributor(500)

	dist.AddInstance("instance-1")
	dist.AddInstance("instance-2")
	dist.AddInstance("instance-3")

	stationIDs := make([]string, 100)
	for i := 0; i < 100; i++ {
		stationIDs[i] = fmt.Sprintf("station-%d", i)
	}

	// Calculate churn for adding instance-4
	churn := dist.CalculateChurn(stationIDs, "add", "instance-4")
	t.Logf("Predicted churn for adding instance-4: %.1f%%", churn*100)

	if churn < 0.05 || churn > 0.4 {
		t.Errorf("Churn %.2f outside expected range (0.05-0.4)", churn)
	}

	// Calculate churn for removing instance-2
	churn = dist.CalculateChurn(stationIDs, "remove", "instance-2")
	t.Logf("Predicted churn for removing instance-2: %.1f%%", churn*100)

	if churn < 0.05 || churn > 0.4 {
		t.Errorf("Churn %.2f outside expected range (0.05-0.4)", churn)
	}
}

func BenchmarkDistributor_GetInstance(b *testing.B) {
	dist := NewDistributor(500)

	for i := 0; i < 10; i++ {
		dist.AddInstance(fmt.Sprintf("instance-%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stationID := fmt.Sprintf("station-%d", i%1000)
		_, _ = dist.GetInstance(stationID)
	}
}
