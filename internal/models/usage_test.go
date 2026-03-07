package models

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordAndLoadUsage(t *testing.T) {
	dir := t.TempDir()

	entries := []UsageEntry{
		{
			Timestamp:  time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
			ModelID:    "claude-opus-4-6",
			Provider:   "anthropic",
			AccessType: "subscription",
			TokensIn:   1000,
			TokensOut:  500,
			CostUSD:    0,
			Success:    true,
			LatencyMs:  2500,
		},
		{
			Timestamp:  time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
			ModelID:    "ollama/llama3.1",
			Provider:   "ollama",
			AccessType: "local",
			TokensIn:   500,
			TokensOut:  200,
			CostUSD:    0,
			Success:    true,
			LatencyMs:  800,
		},
		{
			Timestamp:  time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC),
			ModelID:    "gpt-4o",
			Provider:   "openai",
			AccessType: "api_key",
			TokensIn:   2000,
			TokensOut:  1000,
			CostUSD:    0.015,
			Success:    false,
			LatencyMs:  5000,
		},
	}

	for _, e := range entries {
		if err := RecordUsage(dir, e); err != nil {
			t.Fatal(err)
		}
	}

	// Load all.
	loaded, err := LoadUsage(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(loaded))
	}

	// Load with time filter.
	loaded, err = LoadUsage(dir, time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 entries after filtering, got %d", len(loaded))
	}
}

func TestRecordUsageDisabled(t *testing.T) {
	t.Setenv("GT_TRACK_USAGE", "false")
	dir := t.TempDir()
	err := RecordUsage(dir, UsageEntry{ModelID: "test"})
	if err != nil {
		t.Fatal(err)
	}
	// File should not exist.
	_, err = os.Stat(filepath.Join(dir, "usage.jsonl"))
	if !os.IsNotExist(err) {
		t.Error("expected usage file to not be created when tracking disabled")
	}
}

func TestMonthlyStats(t *testing.T) {
	entries := []UsageEntry{
		{Timestamp: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), ModelID: "opus", Provider: "anthropic", CostUSD: 0.01, Success: true, TokensIn: 100, TokensOut: 50},
		{Timestamp: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC), ModelID: "opus", Provider: "anthropic", CostUSD: 0.02, Success: true, TokensIn: 200, TokensOut: 100},
		{Timestamp: time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC), ModelID: "local", Provider: "ollama", CostUSD: 0, Success: true, TokensIn: 50, TokensOut: 20},
		{Timestamp: time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC), ModelID: "opus", Provider: "anthropic", CostUSD: 0.05, Success: false},
	}

	stats := MonthlyStats(entries, 2026, time.March)
	if len(stats) != 2 {
		t.Fatalf("expected 2 models in March stats, got %d", len(stats))
	}

	opus := stats["opus"]
	if opus == nil {
		t.Fatal("expected opus stats")
	}
	if opus.Invocations != 2 {
		t.Errorf("expected 2 invocations, got %d", opus.Invocations)
	}
	if opus.TotalCostUSD != 0.03 {
		t.Errorf("expected cost 0.03, got %f", opus.TotalCostUSD)
	}
	if opus.TotalTokensIn != 300 {
		t.Errorf("expected 300 tokens in, got %d", opus.TotalTokensIn)
	}

	local := stats["local"]
	if local == nil {
		t.Fatal("expected local stats")
	}
	if local.TotalCostUSD != 0 {
		t.Errorf("expected zero cost for local, got %f", local.TotalCostUSD)
	}
}

func TestTotalCost(t *testing.T) {
	entries := []UsageEntry{
		{CostUSD: 0.01},
		{CostUSD: 0.02},
		{CostUSD: 0},
	}
	got := TotalCost(entries)
	want := 0.03
	if got != want {
		t.Errorf("TotalCost() = %f, want %f", got, want)
	}
}

func TestEstimateCost(t *testing.T) {
	api := &ModelEntry{CostPer1KIn: 0.003, CostPer1KOut: 0.015}
	got := EstimateCost(api, 1000, 500)
	want := 0.0105
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("EstimateCost(api) = %f, want %f", got, want)
	}

	local := &ModelEntry{Local: true}
	got = EstimateCost(local, 1000, 500)
	if got != 0 {
		t.Errorf("EstimateCost(local) = %f, want 0", got)
	}

	got = EstimateCost(nil, 1000, 500)
	if got != 0 {
		t.Errorf("EstimateCost(nil) = %f, want 0", got)
	}
}

func TestLoadUsageNonexistentFile(t *testing.T) {
	dir := t.TempDir()
	entries, err := LoadUsage(dir, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}
