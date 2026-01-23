package telemetry

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestAlertsFileValid verifies the Prometheus alerts configuration is valid YAML.
func TestAlertsFileValid(t *testing.T) {
	alertsPath := "../../deploy/prometheus/alerts.yml"

	data, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Skipf("Skipping test: alerts file not found at %s", alertsPath)
		return
	}

	var config map[string]interface{}
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("Invalid YAML in alerts.yml: %v", err)
	}

	// Verify groups exist
	groups, ok := config["groups"]
	if !ok {
		t.Error("alerts.yml missing 'groups' key")
		return
	}

	groupsList, ok := groups.([]interface{})
	if !ok || len(groupsList) == 0 {
		t.Error("alerts.yml 'groups' is empty or invalid")
	}

	t.Logf("Successfully parsed alerts.yml with %d alert groups", len(groupsList))
}

// TestCriticalAlertsPresent verifies critical alerts are defined.
func TestCriticalAlertsPresent(t *testing.T) {
	alertsPath := "../../deploy/prometheus/alerts.yml"

	data, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Skipf("Skipping test: alerts file not found at %s", alertsPath)
		return
	}

	content := string(data)

	criticalAlerts := []string{
		"MediaEngineDown",
		"HighAPIErrorRate",
		"SchedulerStuck",
		"PlayoutDropouts",
		"DatabaseDown",
	}

	for _, alertName := range criticalAlerts {
		if !strings.Contains(content, alertName) {
			t.Errorf("Critical alert '%s' not found in alerts.yml", alertName)
		}
	}
}

// TestAlertLabels verifies alerts have required labels.
func TestAlertLabels(t *testing.T) {
	alertsPath := "../../deploy/prometheus/alerts.yml"

	data, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Skipf("Skipping test: alerts file not found at %s", alertsPath)
		return
	}

	type Alert struct {
		Alert       string            `yaml:"alert"`
		Expr        string            `yaml:"expr"`
		For         string            `yaml:"for"`
		Labels      map[string]string `yaml:"labels"`
		Annotations map[string]string `yaml:"annotations"`
	}

	type Group struct {
		Name  string  `yaml:"name"`
		Rules []Alert `yaml:"rules"`
	}

	type Config struct {
		Groups []Group `yaml:"groups"`
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse alerts.yml: %v", err)
	}

	for _, group := range config.Groups {
		for _, alert := range group.Rules {
			if alert.Alert == "" {
				continue // Skip non-alert rules
			}

			// Every alert should have a severity label
			if _, ok := alert.Labels["severity"]; !ok {
				t.Errorf("Alert '%s' missing 'severity' label", alert.Alert)
			}

			// Every alert should have annotations
			if len(alert.Annotations) == 0 {
				t.Errorf("Alert '%s' missing annotations", alert.Alert)
			}

			// Check for summary annotation
			if _, ok := alert.Annotations["summary"]; !ok {
				t.Errorf("Alert '%s' missing 'summary' annotation", alert.Alert)
			}
		}
	}
}

// TestMetricsExist verifies key metrics used in alerts actually exist.
func TestMetricsExist(t *testing.T) {
	// These are the metric names that should be exported by our code
	expectedMetrics := []string{
		"grimnir_api_request_duration_seconds",
		"grimnir_api_requests_total",
		"grimnir_scheduler_ticks_total",
		"grimnir_scheduler_errors_total",
		"grimnir_executor_state",
		"grimnir_playout_dropout_count_total",
		"grimnir_media_engine_connection_status",
		"grimnir_database_connections_active",
		"grimnir_leader_election_status",
		"grimnir_live_sessions_active",
		"grimnir_webstream_health_status",
	}

	// Verify each metric is declared in metrics.go
	metricsFilePath := "metrics.go"
	data, err := os.ReadFile(metricsFilePath)
	if err != nil {
		t.Fatalf("Failed to read metrics.go: %v", err)
	}

	content := string(data)

	for _, metric := range expectedMetrics {
		if !strings.Contains(content, metric) {
			t.Errorf("Expected metric '%s' not found in metrics.go", metric)
		}
	}

	t.Logf("Verified %d metrics are declared in metrics.go", len(expectedMetrics))
}
