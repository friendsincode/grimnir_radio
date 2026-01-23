package telemetry

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Scheduler metrics
var (
	ScheduleBuildDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grimnir_schedule_build_duration_seconds",
			Help:    "Time taken to build schedule for a station",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"station_id"},
	)

	ScheduleEntriesTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_schedule_entries_total",
			Help: "Total number of schedule entries generated",
		},
		[]string{"station_id"},
	)

	SmartBlockMaterializeDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grimnir_smart_block_materialize_duration_seconds",
			Help:    "Time taken to materialize a smart block",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"station_id", "smart_block_id"},
	)

	SchedulerTicksTotal = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "grimnir_scheduler_ticks_total",
			Help: "Total number of scheduler ticks",
		},
	)

	SchedulerErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_scheduler_errors_total",
			Help: "Total number of scheduler errors",
		},
		[]string{"station_id", "error_type"},
	)
)

// Executor metrics
var (
	ExecutorState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_executor_state",
			Help: "Current executor state (0=idle, 1=preloading, 2=playing, 3=fading, 4=live, 5=emergency)",
		},
		[]string{"station_id", "executor_id"},
	)

	PlayoutBufferDepth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_playout_buffer_depth_samples",
			Help: "Current playout buffer depth in samples",
		},
		[]string{"station_id", "mount_id"},
	)

	PlayoutDropoutCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_playout_dropout_count_total",
			Help: "Total number of playout dropouts (underruns)",
		},
		[]string{"station_id", "mount_id"},
	)

	PlayoutCPUUsage = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_playout_cpu_usage_percent",
			Help: "CPU usage percentage for playout",
		},
		[]string{"station_id", "mount_id"},
	)

	ExecutorStateTransitions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_executor_state_transitions_total",
			Help: "Total number of executor state transitions",
		},
		[]string{"station_id", "from_state", "to_state"},
	)

	ExecutorPriorityChanges = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_executor_priority_changes_total",
			Help: "Total number of priority changes",
		},
		[]string{"station_id", "from_priority", "to_priority"},
	)
)

// Media Engine metrics
var (
	MediaEngineLoudness = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_media_engine_loudness_lufs",
			Help: "Current loudness level in LUFS",
		},
		[]string{"station_id", "mount_id"},
	)

	MediaEngineOutputHealth = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_media_engine_output_health",
			Help: "Output health status (1=healthy, 0=unhealthy)",
		},
		[]string{"station_id", "mount_id", "output_type"},
	)

	MediaEngineConnectionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_media_engine_connection_status",
			Help: "Media engine gRPC connection status (1=connected, 0=disconnected)",
		},
		[]string{"executor_id"},
	)

	MediaEnginePipelineRestarts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_media_engine_pipeline_restarts_total",
			Help: "Total number of pipeline restarts",
		},
		[]string{"station_id", "mount_id", "reason"},
	)

	MediaEngineAudioLevelLeft = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_media_engine_audio_level_left_db",
			Help: "Audio level for left channel in dB",
		},
		[]string{"station_id", "mount_id"},
	)

	MediaEngineAudioLevelRight = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_media_engine_audio_level_right_db",
			Help: "Audio level for right channel in dB",
		},
		[]string{"station_id", "mount_id"},
	)

	MediaEngineOperations = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_media_engine_operations_total",
			Help: "Total number of media engine operations",
		},
		[]string{"station_id", "mount_id", "operation", "status"},
	)

	MediaEngineOperationDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grimnir_media_engine_operation_duration_seconds",
			Help:    "Duration of media engine operations",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
		},
		[]string{"station_id", "mount_id", "operation"},
	)

	MediaEnginePlaybackState = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_media_engine_playback_state",
			Help: "Current playback state (0=unspecified, 1=idle, 2=loading, 3=playing, 4=paused, 5=fading, 6=error)",
		},
		[]string{"station_id", "mount_id"},
	)

	MediaEngineActivePipelines = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_media_engine_active_pipelines",
			Help: "Number of active pipelines (1=active, 0=inactive)",
		},
		[]string{"station_id", "mount_id"},
	)
)

// API metrics
var (
	APIRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grimnir_api_request_duration_seconds",
			Help:    "HTTP request duration in seconds",
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		},
		[]string{"method", "endpoint", "status_code"},
	)

	APIRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_api_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"method", "endpoint", "status_code"},
	)

	APIActiveConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "grimnir_api_active_connections",
			Help: "Number of active API connections",
		},
	)

	APIWebSocketConnections = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "grimnir_api_websocket_connections",
			Help: "Number of active WebSocket connections",
		},
	)
)

// Live & Webstream metrics
var (
	LiveSessionsActive = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_live_sessions_active",
			Help: "Number of active live DJ sessions",
		},
		[]string{"station_id"},
	)

	LiveSessionDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grimnir_live_session_duration_seconds",
			Help:    "Duration of live DJ sessions",
			Buckets: []float64{60, 300, 600, 1800, 3600, 7200, 14400},
		},
		[]string{"station_id", "user_id"},
	)

	WebstreamHealthStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_webstream_health_status",
			Help: "Webstream health status (2=healthy, 1=degraded, 0=unhealthy)",
		},
		[]string{"webstream_id", "station_id"},
	)

	WebstreamFailoversTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_webstream_failovers_total",
			Help: "Total number of webstream failovers",
		},
		[]string{"webstream_id", "station_id", "from_url", "to_url"},
	)

	WebstreamHealthChecksTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_webstream_health_checks_total",
			Help: "Total number of webstream health checks",
		},
		[]string{"webstream_id", "status"},
	)
)

// Database metrics
var (
	DatabaseQueryDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "grimnir_database_query_duration_seconds",
			Help:    "Database query duration in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"operation", "table"},
	)

	DatabaseConnectionsActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "grimnir_database_connections_active",
			Help: "Number of active database connections",
		},
	)

	DatabaseErrorsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_database_errors_total",
			Help: "Total number of database errors",
		},
		[]string{"operation", "error_type"},
	)
)

// Leader election metrics
var (
	LeaderElectionStatus = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "grimnir_leader_election_status",
			Help: "Leader election status (1=leader, 0=follower)",
		},
		[]string{"instance_id"},
	)

	LeaderElectionChanges = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "grimnir_leader_election_changes_total",
			Help: "Total number of leadership changes",
		},
		[]string{"instance_id", "event"},
	)
)

// Handler exposes Prometheus metrics endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}
