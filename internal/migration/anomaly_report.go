package migration

import (
	"fmt"
	"strings"
	"time"
)

// BuildAnomalyReport creates a normalized per-job anomaly artifact from import results.
func BuildAnomalyReport(result *Result) *AnomalyReport {
	if result == nil {
		return nil
	}

	report := &AnomalyReport{
		GeneratedAt: time.Now(),
		ByClass:     map[AnomalyClass]AnomalyBucket{},
	}

	for key, count := range result.Skipped {
		if count <= 0 {
			continue
		}
		report.Total += count
		example := fmt.Sprintf("%s (%d)", key, count)
		for _, class := range classifySkippedKey(key) {
			addAnomaly(report, class, count, example)
		}
	}

	for _, warning := range result.Warnings {
		msg := strings.TrimSpace(warning)
		if msg == "" {
			continue
		}
		report.Total++
		for _, class := range classifyWarning(msg) {
			addAnomaly(report, class, 1, msg)
		}
	}

	if report.Total == 0 {
		return nil
	}
	return report
}

func classifySkippedKey(key string) []AnomalyClass {
	lower := strings.ToLower(strings.TrimSpace(key))
	classes := []AnomalyClass{AnomalyClassSkippedEntities}
	if lower == "" {
		return classes
	}
	if strings.Contains(lower, "duration") {
		classes = append(classes, AnomalyClassDuration)
	}
	if strings.Contains(lower, "duplicate") || strings.Contains(lower, "deduplic") {
		classes = append(classes, AnomalyClassDuplicateResolution)
	}
	if strings.Contains(lower, "missing") ||
		strings.Contains(lower, "not_found") ||
		strings.Contains(lower, "no_station") ||
		strings.Contains(lower, "orphan") ||
		strings.Contains(lower, "link") {
		classes = append(classes, AnomalyClassMissingLinks)
	}
	return uniqueAnomalyClasses(classes)
}

func classifyWarning(warning string) []AnomalyClass {
	lower := strings.ToLower(strings.TrimSpace(warning))
	if lower == "" {
		return nil
	}
	var classes []AnomalyClass
	if strings.Contains(lower, "duration") {
		classes = append(classes, AnomalyClassDuration)
	}
	if strings.Contains(lower, "duplicate") || strings.Contains(lower, "deduplic") {
		classes = append(classes, AnomalyClassDuplicateResolution)
	}
	if strings.Contains(lower, "missing") ||
		strings.Contains(lower, "not found") ||
		strings.Contains(lower, "no target station mapping") ||
		strings.Contains(lower, "orphan") ||
		strings.Contains(lower, "link") {
		classes = append(classes, AnomalyClassMissingLinks)
	}
	if strings.Contains(lower, "skip") || strings.Contains(lower, "failed") {
		classes = append(classes, AnomalyClassSkippedEntities)
	}
	return uniqueAnomalyClasses(classes)
}

func addAnomaly(report *AnomalyReport, class AnomalyClass, count int, example string) {
	if report == nil || class == "" || count <= 0 {
		return
	}
	bucket := report.ByClass[class]
	bucket.Count += count
	if example != "" && !containsString(bucket.Examples, example) && len(bucket.Examples) < 5 {
		bucket.Examples = append(bucket.Examples, example)
	}
	report.ByClass[class] = bucket
}

func uniqueAnomalyClasses(classes []AnomalyClass) []AnomalyClass {
	if len(classes) < 2 {
		return classes
	}
	seen := make(map[AnomalyClass]struct{}, len(classes))
	out := make([]AnomalyClass, 0, len(classes))
	for _, class := range classes {
		if _, ok := seen[class]; ok {
			continue
		}
		seen[class] = struct{}{}
		out = append(out, class)
	}
	return out
}
