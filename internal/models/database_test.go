package models

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetModel(t *testing.T) {
	db := LoadDatabase("")

	// Should find known models.
	m := GetModel(db, "claude-opus-4-6")
	if m == nil {
		t.Fatal("expected to find claude-opus-4-6")
	}
	if m.Provider != "anthropic" {
		t.Errorf("expected provider=anthropic, got %q", m.Provider)
	}
	if m.MMLUScore <= 0 {
		t.Errorf("expected positive MMLU score, got %f", m.MMLUScore)
	}

	// Should find local models.
	m = GetModel(db, "ollama/llama3.1")
	if m == nil {
		t.Fatal("expected to find ollama/llama3.1")
	}
	if !m.Local {
		t.Error("expected ollama/llama3.1 to be local")
	}
	if m.CostPer1KIn != 0 || m.CostPer1KOut != 0 {
		t.Error("expected zero cost for local model")
	}

	// Should not find unknown models.
	m = GetModel(db, "nonexistent-model")
	if m != nil {
		t.Error("expected nil for nonexistent model")
	}
}

func TestLoadDatabaseWithOverrides(t *testing.T) {
	dir := t.TempDir()

	// Write a models.toml override.
	overrideContent := `
[models.claude-opus-4-6]
mmlu = 95.0

[models.my-custom-local]
provider = "ollama"
mmlu = 60.0
swe = 10.0
local = true
good_for = ["testing"]
`
	if err := os.WriteFile(filepath.Join(dir, "models.toml"), []byte(overrideContent), 0644); err != nil {
		t.Fatal(err)
	}

	db := LoadDatabase(dir)

	// Check override applied.
	m := GetModel(db, "claude-opus-4-6")
	if m == nil {
		t.Fatal("expected claude-opus-4-6")
	}
	if m.MMLUScore != 95.0 {
		t.Errorf("expected overridden MMLU=95.0, got %f", m.MMLUScore)
	}

	// Check new model added.
	m = GetModel(db, "my-custom-local")
	if m == nil {
		t.Fatal("expected my-custom-local to be added")
	}
	if m.Provider != "ollama" {
		t.Errorf("expected provider=ollama, got %q", m.Provider)
	}
	if !m.Local {
		t.Error("expected local=true")
	}
	if m.MMLUScore != 60.0 {
		t.Errorf("expected MMLU=60.0, got %f", m.MMLUScore)
	}
}

func TestCombinedCostPer1K(t *testing.T) {
	m := ModelEntry{CostPer1KIn: 0.003, CostPer1KOut: 0.015}
	got := m.CombinedCostPer1K()
	want := 0.009
	if got != want {
		t.Errorf("CombinedCostPer1K() = %f, want %f", got, want)
	}
}

func TestStaticDBHasLocalModels(t *testing.T) {
	db := LoadDatabase("")
	var localCount int
	for _, m := range db {
		if m.Local {
			localCount++
			if m.CostPer1KIn != 0 || m.CostPer1KOut != 0 {
				t.Errorf("local model %q should have zero cost", m.ID)
			}
			if m.Provider != "ollama" {
				t.Errorf("local model %q should have provider=ollama, got %q", m.ID, m.Provider)
			}
		}
	}
	if localCount == 0 {
		t.Error("expected at least one local model in static DB")
	}
}
