package models

import (
	"testing"
)

func testDB() []ModelEntry {
	return []ModelEntry{
		{
			ID: "claude-opus-4-6", Provider: "anthropic", Name: "Claude Opus 4.6",
			MMLUScore: 90.0, SWEScore: 72.0,
			Vision: true, ContextWindow: 200000,
			CostPer1KIn: 0.015, CostPer1KOut: 0.075,
			SubscriptionEligible: true,
		},
		{
			ID: "claude-sonnet-4-6", Provider: "anthropic", Name: "Claude Sonnet 4.6",
			MMLUScore: 88.0, SWEScore: 65.0,
			Vision: true, ContextWindow: 200000,
			CostPer1KIn: 0.003, CostPer1KOut: 0.015,
			SubscriptionEligible: true,
		},
		{
			ID: "gpt-4o-mini", Provider: "openai", Name: "GPT-4o Mini",
			MMLUScore: 82.0, SWEScore: 35.0,
			Vision: true, ContextWindow: 128000,
			CostPer1KIn: 0.00015, CostPer1KOut: 0.0006,
		},
		{
			ID: "ollama/llama3.1", Provider: "ollama", Name: "Llama 3.1 (local)",
			MMLUScore: 73.0, SWEScore: 25.0,
			ContextWindow: 131072,
			Local:         true,
		},
		{
			ID: "ollama/deepseek-coder-v2", Provider: "ollama", Name: "DeepSeek Coder V2 (local)",
			MMLUScore: 79.0, SWEScore: 35.0,
			ContextWindow: 131072,
			Local:         true,
			GoodFor:       []string{"coding"},
		},
	}
}

func TestSelectModel_ExactModel(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{Model: "gpt-4o-mini"}, db)
	if err != nil {
		t.Fatal(err)
	}
	if decision.ModelID != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %s", decision.ModelID)
	}
	if decision.Reason != "exact model requested" {
		t.Errorf("expected exact reason, got %q", decision.Reason)
	}
}

func TestSelectModel_ExactModelNotFound(t *testing.T) {
	db := testDB()
	_, err := SelectModel(StepConstraints{Model: "nonexistent"}, db)
	if err == nil {
		t.Error("expected error for nonexistent model")
	}
}

func TestSelectModel_SubscriptionPreferred(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:              "auto",
		SubscriptionActive: true,
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	// Should select an Anthropic model due to subscription bonus.
	if decision.Provider != "anthropic" {
		t.Errorf("expected anthropic provider with subscription, got %s", decision.Provider)
	}
	if decision.AccessType != "subscription" {
		t.Errorf("expected subscription access, got %s", decision.AccessType)
	}
}

func TestSelectModel_LocalPreferred(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:       "auto",
		PreferLocal: true,
		MaxCost:     0.001, // Very low cost ceiling excludes expensive API models.
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	if !isLocalModel(decision, db) {
		t.Errorf("expected local model with PreferLocal + low MaxCost, got %s", decision.ModelID)
	}
	if decision.AccessType != "local" {
		t.Errorf("expected local access type, got %s", decision.AccessType)
	}
}

func isLocalModel(d *RoutingDecision, db []ModelEntry) bool {
	m := GetModel(db, d.ModelID)
	return m != nil && m.Local
}

func TestSelectModel_ProviderFilter(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:    "auto",
		Provider: "ollama",
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	if decision.Provider != "ollama" {
		t.Errorf("expected ollama provider, got %s", decision.Provider)
	}
}

func TestSelectModel_MinMMLUFilter(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:   "auto",
		MinMMLU: 85,
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	if decision.MMLUScore < 85 {
		t.Errorf("expected MMLU >= 85, got %.1f for %s", decision.MMLUScore, decision.ModelID)
	}
}

func TestSelectModel_MaxCostFilter(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:   "auto",
		MaxCost: 0.001, // Very cheap — should exclude opus/sonnet.
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	m := GetModel(db, decision.ModelID)
	if m == nil {
		t.Fatal("model not found")
	}
	if !m.Local && m.CombinedCostPer1K() > 0.001 {
		t.Errorf("expected cost <= 0.001, got %.4f for %s", m.CombinedCostPer1K(), decision.ModelID)
	}
}

func TestSelectModel_RequiresVision(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:    "auto",
		Requires: []string{"vision"},
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	m := GetModel(db, decision.ModelID)
	if m == nil || !m.Vision {
		t.Errorf("expected a model with vision, got %s", decision.ModelID)
	}
}

func TestSelectModel_NoMatchingModel(t *testing.T) {
	db := testDB()
	_, err := SelectModel(StepConstraints{
		Model:   "auto",
		MinMMLU: 99, // Nothing this high.
	}, db)
	if err == nil {
		t.Error("expected error when no model matches")
	}
}

func TestSelectModel_AccessTypeLocal(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:      "auto",
		AccessType: "local",
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	if decision.AccessType != "local" {
		t.Errorf("expected local access type, got %s", decision.AccessType)
	}
	m := GetModel(db, decision.ModelID)
	if m == nil || !m.Local {
		t.Errorf("expected a local model, got %s", decision.ModelID)
	}
}

func TestSelectModel_AccessTypeSubscription(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{
		Model:              "auto",
		AccessType:         "subscription",
		SubscriptionActive: true,
	}, db)
	if err != nil {
		t.Fatal(err)
	}
	if decision.AccessType != "subscription" {
		t.Errorf("expected subscription access type, got %s", decision.AccessType)
	}
}

func TestSelectModel_EmptyDB(t *testing.T) {
	_, err := SelectModel(StepConstraints{}, nil)
	if err == nil {
		t.Error("expected error for empty database")
	}
}

func TestSelectModel_Unconstrained(t *testing.T) {
	db := testDB()
	decision, err := SelectModel(StepConstraints{}, db)
	if err != nil {
		t.Fatal(err)
	}
	if decision.ModelID == "" {
		t.Error("expected a model to be selected")
	}
}
