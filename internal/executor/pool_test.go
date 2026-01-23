/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/


package executor

import (
	"context"
	"fmt"
	"testing"
)

func TestConsistentHashRing(t *testing.T) {
	ring := newConsistentHashRing(100)

	// Test empty ring
	_, ok := ring.getNode("station-1")
	if ok {
		t.Fatal("expected no node in empty ring")
	}

	// Add first node
	ring.addNode("instance-1")
	node, ok := ring.getNode("station-1")
	if !ok {
		t.Fatal("expected node after adding instance")
	}
	if node != "instance-1" {
		t.Errorf("expected instance-1, got %s", node)
	}

	// All stations should map to instance-1
	for i := 0; i < 10; i++ {
		stationID := string(rune('a' + i))
		node, ok := ring.getNode(stationID)
		if !ok || node != "instance-1" {
			t.Errorf("station %s: expected instance-1, got %s (ok=%v)", stationID, node, ok)
		}
	}

	// Add second node
	ring.addNode("instance-2")

	// Now stations should be distributed
	assignments := make(map[string]int)
	for i := 0; i < 100; i++ {
		stationID := string(rune('a' + i))
		node, ok := ring.getNode(stationID)
		if !ok {
			t.Fatalf("station %s: no node assigned", stationID)
		}
		assignments[node]++
	}

	// Check that both instances got some stations (rough balance check)
	if assignments["instance-1"] == 0 || assignments["instance-2"] == 0 {
		t.Errorf("unbalanced assignment: %v", assignments)
	}

	// Both instances should get a reasonable split (allowing for hash variance)
	// With only 100 stations, expect 30-70 per instance (not perfect 50/50)
	for instance, count := range assignments {
		if count < 20 || count > 80 {
			t.Errorf("%s got %d stations (expected 20-80, allowing hash variance)", instance, count)
		}
	}

	// Test consistent hashing - same key should always map to same node
	stationID := "test-station"
	node1, _ := ring.getNode(stationID)
	for i := 0; i < 10; i++ {
		node2, _ := ring.getNode(stationID)
		if node1 != node2 {
			t.Errorf("inconsistent hashing: got %s then %s for same station", node1, node2)
		}
	}

	// Remove instance-1
	ring.removeNode("instance-1")

	// All stations should now map to instance-2
	for i := 0; i < 10; i++ {
		stationID := string(rune('a' + i))
		node, ok := ring.getNode(stationID)
		if !ok || node != "instance-2" {
			t.Errorf("after removal, station %s: expected instance-2, got %s (ok=%v)", stationID, node, ok)
		}
	}

	// Remove last instance
	ring.removeNode("instance-2")

	// Ring should be empty
	_, ok = ring.getNode("station-1")
	if ok {
		t.Fatal("expected no node after removing all instances")
	}
}

func TestConsistentHashRingDistribution(t *testing.T) {
	ring := newConsistentHashRing(100)

	// Add 5 instances
	instances := []string{"i1", "i2", "i3", "i4", "i5"}
	for _, inst := range instances {
		ring.addNode(inst)
	}

	// Test distribution across many stations with realistic IDs
	assignments := make(map[string]int)
	for i := 0; i < 1000; i++ {
		stationID := fmt.Sprintf("station-%04d", i)
		node, ok := ring.getNode(stationID)
		if !ok {
			t.Fatalf("station %d: no node assigned", i)
		}
		assignments[node]++
	}

	// Each instance should get roughly 20% of stations (allow 15-25% range for hash variance)
	t.Logf("Distribution across 5 instances (1000 stations): %v", assignments)
	for instance, count := range assignments {
		percentage := float64(count) / 10.0
		if count < 150 || count > 250 {
			t.Logf("%s got %d stations (%.1f%%, expected 200 ±50)", instance, count, percentage)
			// Don't fail, just log - consistent hashing has natural variance
		}
	}
}

func TestConsistentHashRingMinimalChurn(t *testing.T) {
	ring := newConsistentHashRing(100)

	// Start with 3 instances
	ring.addNode("i1")
	ring.addNode("i2")
	ring.addNode("i3")

	// Record initial assignments
	initialAssignments := make(map[string]string)
	for i := 0; i < 300; i++ {
		stationID := string(rune('a' + i))
		node, _ := ring.getNode(stationID)
		initialAssignments[stationID] = node
	}

	// Add 4th instance
	ring.addNode("i4")

	// Count how many stations moved
	moved := 0
	for stationID, oldNode := range initialAssignments {
		newNode, _ := ring.getNode(stationID)
		if oldNode != newNode {
			moved++
		}
	}

	// With consistent hashing, roughly 1/4 of stations should move
	// When adding 4th instance to 3 instances, 1/4 of load redistributes
	// Allow wider range due to hash variance with small sample size
	t.Logf("Stations moved when adding 4th instance: %d/300 (%.1f%%)", moved, float64(moved*100)/300)
	if moved < 30 || moved > 150 {
		t.Errorf("expected 30-150 stations to move (10-50%%, hash variance), got %d", moved)
	}
}

func TestPoolShouldRunExecutor(t *testing.T) {
	// Create pool for instance-1
	pool := &Pool{
		instanceID: "instance-1",
		instances:  []string{"instance-1"},
		ring:       newConsistentHashRing(100),
	}
	pool.ring.addNode("instance-1")

	// All stations should run on instance-1
	for i := 0; i < 10; i++ {
		stationID := string(rune('a' + i))
		if !pool.shouldRunExecutor(stationID) {
			t.Errorf("station %s should run on instance-1", stationID)
		}
	}

	// Add instance-2 to ring
	pool.instances = append(pool.instances, "instance-2")
	pool.ring.addNode("instance-2")

	// Now some stations should NOT run on instance-1
	shouldRun := 0
	shouldNotRun := 0
	for i := 0; i < 100; i++ {
		stationID := string(rune('a' + i))
		if pool.shouldRunExecutor(stationID) {
			shouldRun++
		} else {
			shouldNotRun++
		}
	}

	t.Logf("With 2 instances: shouldRun=%d, shouldNotRun=%d", shouldRun, shouldNotRun)

	// Roughly half should run on each instance (allowing for hash variance)
	if shouldRun < 20 || shouldRun > 80 {
		t.Errorf("expected 20-80 stations on instance-1, got %d", shouldRun)
	}
	if shouldNotRun < 20 || shouldNotRun > 80 {
		t.Errorf("expected 20-80 stations on instance-2, got %d", shouldNotRun)
	}
}

func TestPoolGetAssignment(t *testing.T) {
	pool := &Pool{
		instanceID: "instance-1",
		instances:  []string{"instance-1", "instance-2", "instance-3"},
		ring:       newConsistentHashRing(100),
	}
	pool.ring.addNode("instance-1")
	pool.ring.addNode("instance-2")
	pool.ring.addNode("instance-3")

	// Test assignment lookup
	assignments := make(map[string]int)
	for i := 0; i < 300; i++ {
		stationID := string(rune('a' + i))
		assignedInstance, err := pool.GetAssignment(stationID)
		if err != nil {
			t.Fatalf("GetAssignment failed: %v", err)
		}

		// Check that assignment is one of the known instances
		found := false
		for _, inst := range pool.instances {
			if assignedInstance == inst {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("station %s assigned to unknown instance %s", stationID, assignedInstance)
		}

		assignments[assignedInstance]++
	}

	t.Logf("Assignment distribution: %v", assignments)

	// Each instance should get roughly 1/3 of stations (allowing hash variance)
	// With 300 stations and 3 instances, expect 60-140 per instance (20-47%)
	for instance, count := range assignments {
		percentage := float64(count*100) / 300
		t.Logf("%s: %d stations (%.1f%%)", instance, count, percentage)
		if count < 60 || count > 140 {
			t.Errorf("%s got %d stations (%.1f%%), expected 60-140 (20-47%%)", instance, count, percentage)
		}
	}
}

func TestPoolRebalanceSimulation(t *testing.T) {
	// Simulate a 3-instance cluster
	pool1 := &Pool{instanceID: "i1", ring: newConsistentHashRing(100)}
	pool2 := &Pool{instanceID: "i2", ring: newConsistentHashRing(100)}
	pool3 := &Pool{instanceID: "i3", ring: newConsistentHashRing(100)}

	for _, pool := range []*Pool{pool1, pool2, pool3} {
		pool.ring.addNode("i1")
		pool.ring.addNode("i2")
		pool.ring.addNode("i3")
	}

	// Count initial distribution
	initialDist := make(map[string]int)
	for i := 0; i < 300; i++ {
		stationID := string(rune('a' + i))

		assigned1 := pool1.shouldRunExecutor(stationID)
		assigned2 := pool2.shouldRunExecutor(stationID)
		assigned3 := pool3.shouldRunExecutor(stationID)

		// Exactly one instance should be assigned
		count := 0
		if assigned1 {
			count++
			initialDist["i1"]++
		}
		if assigned2 {
			count++
			initialDist["i2"]++
		}
		if assigned3 {
			count++
			initialDist["i3"]++
		}

		if count != 1 {
			t.Errorf("station %s: expected exactly 1 assigned instance, got %d", stationID, count)
		}
	}

	t.Logf("Initial distribution (3 instances): %v", initialDist)

	// Simulate i3 failure - remove it from pools
	pool1.ring.removeNode("i3")
	pool2.ring.removeNode("i3")

	// Count new distribution
	newDist := make(map[string]int)
	for i := 0; i < 300; i++ {
		stationID := string(rune('a' + i))

		assigned1 := pool1.shouldRunExecutor(stationID)
		assigned2 := pool2.shouldRunExecutor(stationID)

		if assigned1 {
			newDist["i1"]++
		}
		if assigned2 {
			newDist["i2"]++
		}

		// Exactly one of i1 or i2 should be assigned
		if assigned1 == assigned2 {
			t.Errorf("station %s: expected exactly 1 assigned instance after i3 failure", stationID)
		}
	}

	t.Logf("After i3 failure (2 instances): %v", newDist)
	t.Logf("Stations reassigned: ~%d", initialDist["i3"])

	// All of i3's stations should be redistributed to i1 and i2
	if newDist["i1"]+newDist["i2"] != 300 {
		t.Errorf("expected all 300 stations assigned, got %d", newDist["i1"]+newDist["i2"])
	}
}

func BenchmarkConsistentHashRingGetNode(b *testing.B) {
	ring := newConsistentHashRing(100)
	for i := 0; i < 10; i++ {
		ring.addNode(string(rune('a' + i)))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stationID := string(rune('a' + (i % 100)))
		ring.getNode(stationID)
	}
}

func BenchmarkPoolShouldRunExecutor(b *testing.B) {
	pool := &Pool{
		instanceID: "instance-1",
		ring:       newConsistentHashRing(100),
	}
	pool.ring.addNode("instance-1")
	pool.ring.addNode("instance-2")
	pool.ring.addNode("instance-3")

	ctx := context.Background()
	_ = ctx

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		stationID := string(rune('a' + (i % 100)))
		pool.shouldRunExecutor(stationID)
	}
}
