package api

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type coverageBadge struct {
	SchemaVersion int    `json:"schemaVersion"`
	Label         string `json:"label"`
	Message       string `json:"message"`
	Color         string `json:"color"`
	CacheSeconds  int    `json:"cacheSeconds,omitempty"`
}

type coverageSummary struct {
	Percent     float64 `json:"percent"`
	Target      float64 `json:"target"`
	ProfilePath string  `json:"profile_path,omitempty"`
	Available   bool    `json:"available"`
}

func (a *API) handleCoverageSummary(w http.ResponseWriter, r *http.Request) {
	profilePath := coverageProfilePath()
	percent, err := parseCoverageProfile(profilePath)
	if err != nil {
		writeJSON(w, http.StatusOK, coverageSummary{Target: coverageTarget(), ProfilePath: profilePath, Available: false})
		return
	}
	writeJSON(w, http.StatusOK, coverageSummary{Percent: percent, Target: coverageTarget(), ProfilePath: profilePath, Available: true})
}

func (a *API) handleCoverageBadge(w http.ResponseWriter, r *http.Request) {
	profilePath := coverageProfilePath()
	percent, err := parseCoverageProfile(profilePath)
	badge := coverageBadge{
		SchemaVersion: 1,
		Label:         "coverage",
		CacheSeconds:  300,
	}
	if err != nil {
		badge.Message = "unavailable"
		badge.Color = "lightgrey"
		writeJSON(w, http.StatusOK, badge)
		return
	}
	badge.Message = fmt.Sprintf("%.1f%%", percent)
	badge.Color = coverageColor(percent, coverageTarget())
	writeJSON(w, http.StatusOK, badge)
}

func (a *API) handleCoverageTargetBadge(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, coverageBadge{
		SchemaVersion: 1,
		Label:         "coverage target",
		Message:       fmt.Sprintf("%.0f%%", coverageTarget()),
		Color:         "0a7f5a",
		CacheSeconds:  3600,
	})
}

func coverageProfilePath() string {
	if path := strings.TrimSpace(os.Getenv("GRIMNIR_COVERAGE_PROFILE")); path != "" {
		return path
	}
	return filepath.Join(".", "coverage.out")
}

func coverageTarget() float64 {
	raw := strings.TrimSpace(os.Getenv("GRIMNIR_COVERAGE_TARGET"))
	if raw == "" {
		return 80
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return 80
	}
	return value
}

func coverageColor(percent, target float64) string {
	switch {
	case percent >= target:
		return "0a7f5a"
	case percent >= target*0.75:
		return "dfb317"
	default:
		return "c0392b"
	}
}

func parseCoverageProfile(path string) (float64, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	var totalStmts, coveredStmts int64
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		lineNo++
		if lineNo == 1 {
			if !strings.HasPrefix(line, "mode:") {
				return 0, fmt.Errorf("invalid coverage profile header")
			}
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return 0, fmt.Errorf("invalid coverage profile line: %q", line)
		}
		numStmt, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse num stmt: %w", err)
		}
		count, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse count: %w", err)
		}
		totalStmts += numStmt
		if count > 0 {
			coveredStmts += numStmt
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	if totalStmts == 0 {
		return 0, fmt.Errorf("no statements in coverage profile")
	}
	return float64(coveredStmts) * 100 / float64(totalStmts), nil
}
