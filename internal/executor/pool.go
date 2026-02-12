/*
Copyright (C) 2026 Friends Incode

SPDX-License-Identifier: AGPL-3.0-or-later
*/

package executor

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"sort"
	"sync"

	"github.com/friendsincode/grimnir_radio/internal/events"
	meclient "github.com/friendsincode/grimnir_radio/internal/mediaengine/client"
	"github.com/friendsincode/grimnir_radio/internal/models"
	"github.com/friendsincode/grimnir_radio/internal/priority"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

// Pool manages a distributed pool of executors across multiple instances.
// Uses consistent hashing to assign stations to instances.
type Pool struct {
	instanceID   string
	db           *gorm.DB
	stateManager *StateManager
	prioritySvc  *priority.Service
	bus          *events.Bus
	mediaClient  *meclient.Client
	logger       zerolog.Logger

	mu        sync.RWMutex
	executors map[string]*Executor // station_id -> executor
	instances []string             // sorted list of instance IDs for consistent hashing
	ring      *consistentHashRing
}

// consistentHashRing implements consistent hashing for executor distribution.
type consistentHashRing struct {
	mu       sync.RWMutex
	nodes    []uint32          // sorted hash values
	nodeMap  map[uint32]string // hash -> instance ID
	replicas int               // virtual nodes per physical instance
}

// newConsistentHashRing creates a new consistent hash ring.
func newConsistentHashRing(replicas int) *consistentHashRing {
	return &consistentHashRing{
		nodes:    []uint32{},
		nodeMap:  make(map[uint32]string),
		replicas: replicas,
	}
}

// addNode adds an instance to the hash ring.
func (r *consistentHashRing) addNode(instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := 0; i < r.replicas; i++ {
		// Use multiple hash variations for better distribution
		hash := hashKey(fmt.Sprintf("%s:%d:vnode", instanceID, i))
		r.nodes = append(r.nodes, hash)
		r.nodeMap[hash] = instanceID
	}

	sort.Slice(r.nodes, func(i, j int) bool {
		return r.nodes[i] < r.nodes[j]
	})
}

// removeNode removes an instance from the hash ring.
func (r *consistentHashRing) removeNode(instanceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := 0; i < r.replicas; i++ {
		hash := hashKey(fmt.Sprintf("%s:%d:vnode", instanceID, i))
		delete(r.nodeMap, hash)
	}

	// Rebuild nodes slice
	newNodes := make([]uint32, 0, len(r.nodeMap))
	for hash := range r.nodeMap {
		newNodes = append(newNodes, hash)
	}
	sort.Slice(newNodes, func(i, j int) bool {
		return newNodes[i] < newNodes[j]
	})
	r.nodes = newNodes
}

// getNode returns the instance responsible for the given key.
func (r *consistentHashRing) getNode(key string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.nodes) == 0 {
		return "", false
	}

	hash := hashKey(key)

	// Binary search to find the first node >= hash
	idx := sort.Search(len(r.nodes), func(i int) bool {
		return r.nodes[i] >= hash
	})

	// Wrap around if necessary
	if idx == len(r.nodes) {
		idx = 0
	}

	return r.nodeMap[r.nodes[idx]], true
}

// hashKey computes FNV-1a hash of a string.
func hashKey(key string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(key))
	return h.Sum32()
}

// NewPool creates a new executor pool.
func NewPool(instanceID string, db *gorm.DB, stateManager *StateManager, prioritySvc *priority.Service, bus *events.Bus, mediaClient *meclient.Client, logger zerolog.Logger) *Pool {
	ring := newConsistentHashRing(150) // 150 virtual nodes per instance for better distribution
	ring.addNode(instanceID)

	return &Pool{
		instanceID:   instanceID,
		db:           db,
		stateManager: stateManager,
		prioritySvc:  prioritySvc,
		bus:          bus,
		mediaClient:  mediaClient,
		logger:       logger.With().Str("component", "executor_pool").Logger(),
		executors:    make(map[string]*Executor),
		instances:    []string{instanceID},
		ring:         ring,
	}
}

// Start initializes and starts executors for stations assigned to this instance.
func (p *Pool) Start(ctx context.Context) error {
	p.logger.Info().Str("instance_id", p.instanceID).Msg("starting executor pool")

	// Load all active stations
	var stations []models.Station
	if err := p.db.WithContext(ctx).Where("active = ?", true).Find(&stations).Error; err != nil {
		return fmt.Errorf("load stations: %w", err)
	}

	// Start executors for stations assigned to this instance
	for _, station := range stations {
		if p.shouldRunExecutor(station.ID) {
			if err := p.StartExecutor(ctx, station.ID); err != nil {
				p.logger.Error().Err(err).Str("station_id", station.ID).Msg("failed to start executor")
				// Continue with other stations
			}
		}
	}

	p.logger.Info().Int("executor_count", len(p.executors)).Msg("executor pool started")
	return nil
}

// StartExecutor starts an executor for the given station if it's assigned to this instance.
func (p *Pool) StartExecutor(ctx context.Context, stationID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if executor already running
	if _, exists := p.executors[stationID]; exists {
		return fmt.Errorf("executor already running for station %s", stationID)
	}

	// Check if this instance should run this executor
	assignedInstance, ok := p.ring.getNode(stationID)
	if !ok || assignedInstance != p.instanceID {
		return fmt.Errorf("station %s not assigned to this instance (assigned to %s)", stationID, assignedInstance)
	}

	meClient, err := p.ensureMediaClient(ctx)
	if err != nil {
		return fmt.Errorf("media engine client unavailable: %w", err)
	}

	mountID := ""
	var mount models.Mount
	if err := p.db.WithContext(ctx).Where("station_id = ?", stationID).Order("created_at ASC").First(&mount).Error; err == nil {
		mountID = mount.ID
	}

	mediaCtrl := NewMediaController(meClient, stationID, mountID, p.logger)

	// Create and start executor
	executor := New(stationID, p.db, p.stateManager, p.prioritySvc, p.bus, mediaCtrl, p.logger)
	if err := executor.Start(ctx); err != nil {
		return fmt.Errorf("start executor: %w", err)
	}

	p.executors[stationID] = executor

	p.logger.Info().
		Str("station_id", stationID).
		Str("instance_id", p.instanceID).
		Msg("executor started")

	return nil
}

func (p *Pool) ensureMediaClient(ctx context.Context) (*meclient.Client, error) {
	if p.mediaClient != nil && p.mediaClient.IsConnected() {
		return p.mediaClient, nil
	}

	addr := os.Getenv("GRIMNIR_MEDIA_ENGINE_GRPC_ADDR")
	if addr == "" {
		addr = os.Getenv("MEDIA_ENGINE_GRPC_ADDR")
	}
	if addr == "" {
		addr = "mediaengine:9091"
	}

	cfg := meclient.DefaultConfig(addr)
	client := meclient.New(cfg, p.logger)
	if err := client.Connect(ctx); err != nil {
		return nil, err
	}
	p.mediaClient = client
	return p.mediaClient, nil
}

// StopExecutor stops the executor for the given station.
func (p *Pool) StopExecutor(ctx context.Context, stationID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	executor, exists := p.executors[stationID]
	if !exists {
		return ErrExecutorNotRunning
	}

	if err := executor.Stop(); err != nil {
		return fmt.Errorf("stop executor: %w", err)
	}

	delete(p.executors, stationID)

	p.logger.Info().
		Str("station_id", stationID).
		Str("instance_id", p.instanceID).
		Msg("executor stopped")

	return nil
}

// Stop stops all executors in the pool.
func (p *Pool) Stop(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.logger.Info().Msg("stopping executor pool")

	var lastErr error
	for stationID, executor := range p.executors {
		if err := executor.Stop(); err != nil {
			p.logger.Error().Err(err).Str("station_id", stationID).Msg("failed to stop executor")
			lastErr = err
		}
	}

	p.executors = make(map[string]*Executor)

	p.logger.Info().Msg("executor pool stopped")
	return lastErr
}

// AddInstance adds a new instance to the pool and rebalances executors.
func (p *Pool) AddInstance(ctx context.Context, instanceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if instance already exists
	for _, id := range p.instances {
		if id == instanceID {
			return fmt.Errorf("instance %s already exists", instanceID)
		}
	}

	p.instances = append(p.instances, instanceID)
	sort.Strings(p.instances)
	p.ring.addNode(instanceID)

	p.logger.Info().
		Str("instance_id", instanceID).
		Int("total_instances", len(p.instances)).
		Msg("instance added to pool")

	// Rebalance executors
	return p.rebalanceExecutors(ctx)
}

// RemoveInstance removes an instance from the pool and rebalances executors.
func (p *Pool) RemoveInstance(ctx context.Context, instanceID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Remove from instances list
	newInstances := make([]string, 0, len(p.instances)-1)
	found := false
	for _, id := range p.instances {
		if id != instanceID {
			newInstances = append(newInstances, id)
		} else {
			found = true
		}
	}

	if !found {
		return fmt.Errorf("instance %s not found", instanceID)
	}

	p.instances = newInstances
	p.ring.removeNode(instanceID)

	p.logger.Info().
		Str("instance_id", instanceID).
		Int("total_instances", len(p.instances)).
		Msg("instance removed from pool")

	// Rebalance executors
	return p.rebalanceExecutors(ctx)
}

// rebalanceExecutors reassigns executors based on the current hash ring.
// Must be called with pool mutex held.
func (p *Pool) rebalanceExecutors(ctx context.Context) error {
	// Build list of stations that should run on this instance
	shouldRun := make(map[string]bool)
	for stationID := range p.executors {
		if p.shouldRunExecutorLocked(stationID) {
			shouldRun[stationID] = true
		}
	}

	// Stop executors that should not run on this instance
	for stationID := range p.executors {
		if !shouldRun[stationID] {
			executor := p.executors[stationID]
			if err := executor.Stop(); err != nil {
				p.logger.Error().Err(err).Str("station_id", stationID).Msg("failed to stop executor during rebalance")
			}
			delete(p.executors, stationID)
			p.logger.Info().Str("station_id", stationID).Msg("executor stopped during rebalance")
		}
	}

	// Start executors for new assignments
	// (This would require loading stations from DB, omitted for now)

	return nil
}

// shouldRunExecutor determines if this instance should run the executor for a station.
func (p *Pool) shouldRunExecutor(stationID string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.shouldRunExecutorLocked(stationID)
}

// shouldRunExecutorLocked is the internal version that assumes the lock is held.
func (p *Pool) shouldRunExecutorLocked(stationID string) bool {
	assignedInstance, ok := p.ring.getNode(stationID)
	return ok && assignedInstance == p.instanceID
}

// GetExecutor returns the executor for a station if it's running on this instance.
func (p *Pool) GetExecutor(stationID string) (*Executor, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	executor, exists := p.executors[stationID]
	if !exists {
		return nil, ErrExecutorNotRunning
	}

	return executor, nil
}

// ListExecutors returns a list of station IDs with executors running on this instance.
func (p *Pool) ListExecutors() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	stationIDs := make([]string, 0, len(p.executors))
	for stationID := range p.executors {
		stationIDs = append(stationIDs, stationID)
	}

	sort.Strings(stationIDs)
	return stationIDs
}

// GetAssignment returns the instance ID responsible for a given station.
func (p *Pool) GetAssignment(stationID string) (string, error) {
	assignedInstance, ok := p.ring.getNode(stationID)
	if !ok {
		return "", fmt.Errorf("no instance available for station %s", stationID)
	}
	return assignedInstance, nil
}
