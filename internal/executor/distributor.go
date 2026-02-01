/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package executor

import (
	"fmt"
	"hash/crc32"
	"sort"
	"sync"
)

// Distributor assigns executors (stations) to instances using consistent hashing.
// This ensures minimal churn when instances join/leave the cluster.
type Distributor struct {
	mu sync.RWMutex

	// ring contains sorted hash values for virtual nodes
	ring []uint32

	// ringMap maps hash values to instance IDs
	ringMap map[uint32]string

	// virtualNodes is the number of virtual nodes per instance
	virtualNodes int
}

// NewDistributor creates a new consistent hash distributor.
func NewDistributor(virtualNodes int) *Distributor {
	if virtualNodes <= 0 {
		virtualNodes = 500 // Default: 500 virtual nodes per instance for better distribution
	}

	return &Distributor{
		ring:         make([]uint32, 0),
		ringMap:      make(map[uint32]string),
		virtualNodes: virtualNodes,
	}
}

// AddInstance adds an instance to the consistent hash ring.
func (d *Distributor) AddInstance(instanceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Add virtual nodes for this instance
	for i := 0; i < d.virtualNodes; i++ {
		key := fmt.Sprintf("%s-%d", instanceID, i)
		hash := hashKeyConsistent(key)

		d.ring = append(d.ring, hash)
		d.ringMap[hash] = instanceID
	}

	// Sort ring
	sort.Slice(d.ring, func(i, j int) bool {
		return d.ring[i] < d.ring[j]
	})
}

// RemoveInstance removes an instance from the consistent hash ring.
func (d *Distributor) RemoveInstance(instanceID string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Remove virtual nodes for this instance
	newRing := make([]uint32, 0, len(d.ring))
	for _, hash := range d.ring {
		if d.ringMap[hash] != instanceID {
			newRing = append(newRing, hash)
		} else {
			delete(d.ringMap, hash)
		}
	}

	d.ring = newRing
}

// GetInstance returns the instance ID responsible for the given station ID.
func (d *Distributor) GetInstance(stationID string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if len(d.ring) == 0 {
		return "", fmt.Errorf("no instances available")
	}

	hash := hashKeyConsistent(stationID)

	// Binary search for the first ring position >= hash
	idx := sort.Search(len(d.ring), func(i int) bool {
		return d.ring[i] >= hash
	})

	// Wrap around if we went past the end
	if idx >= len(d.ring) {
		idx = 0
	}

	instanceID := d.ringMap[d.ring[idx]]
	return instanceID, nil
}

// GetAllAssignments returns a map of station IDs to instance IDs for the given stations.
func (d *Distributor) GetAllAssignments(stationIDs []string) map[string]string {
	assignments := make(map[string]string, len(stationIDs))

	for _, stationID := range stationIDs {
		instanceID, err := d.GetInstance(stationID)
		if err == nil {
			assignments[stationID] = instanceID
		}
	}

	return assignments
}

// GetInstanceStations returns all station IDs assigned to the given instance.
func (d *Distributor) GetInstanceStations(instanceID string, allStationIDs []string) []string {
	stations := make([]string, 0)

	for _, stationID := range allStationIDs {
		assignedInstance, err := d.GetInstance(stationID)
		if err == nil && assignedInstance == instanceID {
			stations = append(stations, stationID)
		}
	}

	return stations
}

// GetInstances returns all currently registered instances.
func (d *Distributor) GetInstances() []string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	instances := make(map[string]bool)
	for _, instanceID := range d.ringMap {
		instances[instanceID] = true
	}

	result := make([]string, 0, len(instances))
	for instanceID := range instances {
		result = append(result, instanceID)
	}

	sort.Strings(result)
	return result
}

// CalculateChurn calculates what percentage of stations would move when adding/removing an instance.
func (d *Distributor) CalculateChurn(stationIDs []string, operation string, instanceID string) float64 {
	// Get current assignments
	beforeAssignments := d.GetAllAssignments(stationIDs)

	// Make a copy of the distributor
	testDistributor := NewDistributor(d.virtualNodes)
	testDistributor.mu.Lock()
	testDistributor.ring = append([]uint32{}, d.ring...)
	testDistributor.ringMap = make(map[uint32]string, len(d.ringMap))
	for k, v := range d.ringMap {
		testDistributor.ringMap[k] = v
	}
	testDistributor.mu.Unlock()

	// Apply operation
	switch operation {
	case "add":
		testDistributor.AddInstance(instanceID)
	case "remove":
		testDistributor.RemoveInstance(instanceID)
	default:
		return 0
	}

	// Get after assignments
	afterAssignments := testDistributor.GetAllAssignments(stationIDs)

	// Count changes
	changes := 0
	for stationID, beforeInstance := range beforeAssignments {
		afterInstance := afterAssignments[stationID]
		if beforeInstance != afterInstance {
			changes++
		}
	}

	if len(stationIDs) == 0 {
		return 0
	}

	return float64(changes) / float64(len(stationIDs))
}

// hashKeyConsistent generates a hash for the given key using CRC32.
// CRC32 is preferred for consistent hashing due to better distribution.
func hashKeyConsistent(key string) uint32 {
	return crc32.ChecksumIEEE([]byte(key))
}
