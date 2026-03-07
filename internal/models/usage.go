package models

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UsageEntry records a single model invocation.
type UsageEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	ModelID    string    `json:"model_id"`
	Provider   string    `json:"provider"`
	AccessType string    `json:"access_type"` // "subscription", "api_key", "local"
	TaskType   string    `json:"task_type,omitempty"`
	TokensIn   int       `json:"tokens_in"`
	TokensOut  int       `json:"tokens_out"`
	CostUSD    float64   `json:"cost_usd"`
	Success    bool      `json:"success"`
	LatencyMs  int       `json:"latency_ms"`
	Reason     string    `json:"reason,omitempty"`
}

// ModelStats aggregates usage for a single model.
type ModelStats struct {
	ModelID       string  `json:"model_id"`
	Provider      string  `json:"provider"`
	Invocations   int     `json:"invocations"`
	TotalTokensIn int     `json:"total_tokens_in"`
	TotalTokensOut int    `json:"total_tokens_out"`
	TotalCostUSD  float64 `json:"total_cost_usd"`
	Successes     int     `json:"successes"`
	Failures      int     `json:"failures"`
}

// usageFilename is the name of the usage tracking file.
const usageFilename = "usage.jsonl"

// usageMu serializes writes to the usage file.
var usageMu sync.Mutex

// RecordUsage appends a usage entry to ~/.gt/usage.jsonl.
func RecordUsage(gtDir string, entry UsageEntry) error {
	if os.Getenv("GT_TRACK_USAGE") == "false" {
		return nil
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling usage entry: %w", err)
	}

	usageMu.Lock()
	defer usageMu.Unlock()

	path := filepath.Join(gtDir, usageFilename)
	if err := os.MkdirAll(gtDir, 0755); err != nil {
		return fmt.Errorf("creating usage directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644) //nolint:gosec // G304: path from config
	if err != nil {
		return fmt.Errorf("opening usage file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing usage entry: %w", err)
	}
	return nil
}

// LoadUsage reads usage entries from ~/.gt/usage.jsonl, filtering by time.
func LoadUsage(gtDir string, since time.Time) ([]UsageEntry, error) {
	path := filepath.Join(gtDir, usageFilename)
	f, err := os.Open(path) //nolint:gosec // G304: path from config
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening usage file: %w", err)
	}
	defer f.Close()

	var entries []UsageEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry UsageEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue // Skip malformed lines.
		}
		if !since.IsZero() && entry.Timestamp.Before(since) {
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return entries, fmt.Errorf("reading usage file: %w", err)
	}
	return entries, nil
}

// MonthlyStats aggregates usage entries by model for a given year/month.
func MonthlyStats(entries []UsageEntry, year int, month time.Month) map[string]*ModelStats {
	stats := make(map[string]*ModelStats)
	for _, e := range entries {
		if e.Timestamp.Year() != year || e.Timestamp.Month() != month {
			continue
		}
		s, ok := stats[e.ModelID]
		if !ok {
			s = &ModelStats{
				ModelID:  e.ModelID,
				Provider: e.Provider,
			}
			stats[e.ModelID] = s
		}
		s.Invocations++
		s.TotalTokensIn += e.TokensIn
		s.TotalTokensOut += e.TokensOut
		s.TotalCostUSD += e.CostUSD
		if e.Success {
			s.Successes++
		} else {
			s.Failures++
		}
	}
	return stats
}

// TotalCost sums the USD cost of all entries.
func TotalCost(entries []UsageEntry) float64 {
	var total float64
	for _, e := range entries {
		total += e.CostUSD
	}
	return total
}

// EstimateCost estimates the cost of a model invocation based on token counts.
func EstimateCost(model *ModelEntry, tokensIn, tokensOut int) float64 {
	if model == nil || model.Local {
		return 0
	}
	return (model.CostPer1KIn * float64(tokensIn) / 1000) +
		(model.CostPer1KOut * float64(tokensOut) / 1000)
}
