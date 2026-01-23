package leadership

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"github.com/friendsincode/grimnir_radio/internal/telemetry"
)

const (
	// Default election key prefix in Redis
	defaultElectionKey = "grimnir:leader:scheduler"

	// Default lease duration - leader must renew before this expires
	defaultLeaseDuration = 15 * time.Second

	// Default renewal interval - how often leader renews lease
	defaultRenewalInterval = 5 * time.Second

	// Default retry interval - how often followers check for leadership
	defaultRetryInterval = 2 * time.Second
)

// Election manages distributed leader election using Redis
type Election struct {
	client   *redis.Client
	logger   zerolog.Logger
	config   ElectionConfig
	instanceID string

	// Internal state
	isLeader   bool
	cancelFunc context.CancelFunc
	stopCh     chan struct{}
	leaderCh   chan bool
}

// ElectionConfig configures leader election behavior
type ElectionConfig struct {
	// RedisAddr is the Redis server address
	RedisAddr string

	// RedisPassword is the Redis password (optional)
	RedisPassword string

	// RedisDB is the Redis database number
	RedisDB int

	// ElectionKey is the Redis key used for leader election
	ElectionKey string

	// LeaseDuration is how long the leader lease is valid
	LeaseDuration time.Duration

	// RenewalInterval is how often the leader renews its lease
	RenewalInterval time.Duration

	// RetryInterval is how often followers attempt to become leader
	RetryInterval time.Duration

	// InstanceID uniquely identifies this instance
	InstanceID string
}

// DefaultConfig returns default election configuration
func DefaultConfig() ElectionConfig {
	return ElectionConfig{
		RedisAddr:       "localhost:6379",
		RedisPassword:   "",
		RedisDB:         0,
		ElectionKey:     defaultElectionKey,
		LeaseDuration:   defaultLeaseDuration,
		RenewalInterval: defaultRenewalInterval,
		RetryInterval:   defaultRetryInterval,
		InstanceID:      uuid.New().String(),
	}
}

// NewElection creates a new leader election manager
func NewElection(config ElectionConfig, logger zerolog.Logger) (*Election, error) {
	if config.ElectionKey == "" {
		config.ElectionKey = defaultElectionKey
	}
	if config.LeaseDuration == 0 {
		config.LeaseDuration = defaultLeaseDuration
	}
	if config.RenewalInterval == 0 {
		config.RenewalInterval = defaultRenewalInterval
	}
	if config.RetryInterval == 0 {
		config.RetryInterval = defaultRetryInterval
	}
	if config.InstanceID == "" {
		config.InstanceID = uuid.New().String()
	}

	// Create Redis client
	client := redis.NewClient(&redis.Options{
		Addr:     config.RedisAddr,
		Password: config.RedisPassword,
		DB:       config.RedisDB,
	})

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info().
		Str("redis_addr", config.RedisAddr).
		Str("instance_id", config.InstanceID).
		Msg("connected to Redis for leader election")

	return &Election{
		client:     client,
		logger:     logger.With().Str("component", "leader_election").Logger(),
		config:     config,
		instanceID: config.InstanceID,
		isLeader:   false,
		stopCh:     make(chan struct{}),
		leaderCh:   make(chan bool, 1),
	}, nil
}

// Start begins the leader election process
func (e *Election) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	e.cancelFunc = cancel

	e.logger.Info().
		Str("instance_id", e.instanceID).
		Dur("lease_duration", e.config.LeaseDuration).
		Msg("starting leader election")

	go e.campaignLoop(ctx)

	return nil
}

// Stop stops the leader election and releases leadership if held
func (e *Election) Stop() error {
	e.logger.Info().Msg("stopping leader election")

	// Signal stop
	close(e.stopCh)

	// Cancel context
	if e.cancelFunc != nil {
		e.cancelFunc()
	}

	// Release leadership if held
	if e.isLeader {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := e.releaseLock(ctx); err != nil {
			e.logger.Error().Err(err).Msg("failed to release leadership lock")
		}
	}

	// Close Redis connection
	return e.client.Close()
}

// IsLeader returns whether this instance is currently the leader
func (e *Election) IsLeader() bool {
	return e.isLeader
}

// LeaderCh returns a channel that receives leadership status changes
func (e *Election) LeaderCh() <-chan bool {
	return e.leaderCh
}

// GetLeader returns the current leader instance ID
func (e *Election) GetLeader(ctx context.Context) (string, error) {
	leaderID, err := e.client.Get(ctx, e.config.ElectionKey).Result()
	if err == redis.Nil {
		return "", nil // No leader
	}
	if err != nil {
		return "", fmt.Errorf("get leader: %w", err)
	}
	return leaderID, nil
}

// campaignLoop continuously attempts to become/remain leader
func (e *Election) campaignLoop(ctx context.Context) {
	ticker := time.NewTicker(e.config.RetryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			e.attemptLeadership(ctx)
		}
	}
}

// attemptLeadership attempts to acquire or renew leadership
func (e *Election) attemptLeadership(ctx context.Context) {
	// Try to acquire leadership
	acquired, err := e.acquireLock(ctx)
	if err != nil {
		e.logger.Error().Err(err).Msg("failed to acquire leadership lock")
		e.updateLeadershipStatus(false)
		return
	}

	if acquired {
		if !e.isLeader {
			// Newly became leader
			e.logger.Info().
				Str("instance_id", e.instanceID).
				Msg("acquired leadership")
			e.updateLeadershipStatus(true)
		}
	} else {
		if e.isLeader {
			// Lost leadership
			e.logger.Warn().
				Str("instance_id", e.instanceID).
				Msg("lost leadership")
			e.updateLeadershipStatus(false)
		}
	}
}

// acquireLock attempts to acquire the leadership lock in Redis
func (e *Election) acquireLock(ctx context.Context) (bool, error) {
	// Use SET with NX (only if not exists) and PX (expiration in milliseconds)
	// If we're already the leader, this acts as a renewal
	result, err := e.client.SetNX(ctx, e.config.ElectionKey, e.instanceID, e.config.LeaseDuration).Result()
	if err != nil {
		return false, fmt.Errorf("set lock: %w", err)
	}

	if result {
		// Successfully acquired lock
		return true, nil
	}

	// Lock exists, check if we own it
	currentLeader, err := e.client.Get(ctx, e.config.ElectionKey).Result()
	if err == redis.Nil {
		// Lock disappeared, try again
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get current leader: %w", err)
	}

	if currentLeader == e.instanceID {
		// We own the lock, renew it
		err = e.client.Expire(ctx, e.config.ElectionKey, e.config.LeaseDuration).Err()
		if err != nil {
			return false, fmt.Errorf("renew lock: %w", err)
		}
		return true, nil
	}

	// Someone else is leader
	return false, nil
}

// releaseLock releases the leadership lock
func (e *Election) releaseLock(ctx context.Context) error {
	// Only delete if we still own it
	script := `
		if redis.call("get", KEYS[1]) == ARGV[1] then
			return redis.call("del", KEYS[1])
		else
			return 0
		end
	`

	err := e.client.Eval(ctx, script, []string{e.config.ElectionKey}, e.instanceID).Err()
	if err != nil {
		return fmt.Errorf("release lock: %w", err)
	}

	e.logger.Info().Msg("released leadership lock")
	return nil
}

// updateLeadershipStatus updates the leadership status and notifies listeners
func (e *Election) updateLeadershipStatus(isLeader bool) {
	if e.isLeader == isLeader {
		return
	}

	e.isLeader = isLeader

	// Update metrics
	if isLeader {
		telemetry.LeaderElectionStatus.WithLabelValues(e.instanceID).Set(1)
		telemetry.LeaderElectionChanges.WithLabelValues(e.instanceID, "acquired").Inc()
	} else {
		telemetry.LeaderElectionStatus.WithLabelValues(e.instanceID).Set(0)
		telemetry.LeaderElectionChanges.WithLabelValues(e.instanceID, "lost").Inc()
	}

	// Non-blocking send to leaderCh
	select {
	case e.leaderCh <- isLeader:
	default:
		// Channel full, skip
	}
}
