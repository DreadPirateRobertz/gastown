package models

import (
	"fmt"
	"math"
	"os"
)

// StepConstraints defines what a molecule step requires from its model.
type StepConstraints struct {
	Model    string   // Exact model ID, "auto", or "" (unconstrained).
	Provider string   // Require a specific provider (e.g., "anthropic", "ollama").
	MinMMLU  float64  // Minimum MMLU score.
	MinSWE   float64  // Minimum SWE-bench score.
	Requires []string // Required capabilities: "vision", "code_execution".
	MaxCost  float64  // Maximum USD per 1K tokens (combined input+output average).

	// AccessType constrains how the model is accessed.
	// Values: "subscription", "api_key", "local", or "" (any).
	AccessType string

	// SubscriptionActive indicates the caller has an active subscription
	// (e.g., Claude Code). Filled by caller from env/config.
	SubscriptionActive bool

	// PreferLocal indicates the caller prefers local models when they
	// meet quality thresholds. Set from GT_PREFER_LOCAL env or config.
	PreferLocal bool
}

// RoutingDecision describes which model was selected and why.
type RoutingDecision struct {
	ModelID      string  `json:"model_id"`
	Provider     string  `json:"provider"`
	AccessType   string  `json:"access_type"` // "subscription", "api_key", "local"
	Reason       string  `json:"reason"`
	CostPer1KIn  float64 `json:"cost_per_1k_in"`
	CostPer1KOut float64 `json:"cost_per_1k_out"`
	MMLUScore    float64 `json:"mmlu_score"`
	SWEScore     float64 `json:"swe_score"`
	Score        float64 `json:"score"` // Internal routing score.
}

// Scoring weights for the routing heuristic.
const (
	weightSubscription = 40.0
	weightLocal        = 35.0
	weightMMLU         = 30.0
	weightSWE          = 20.0
	weightCost         = 10.0
	costCeiling        = 0.10 // USD per 1K tokens — used for cost savings scoring.
)

// SelectModel picks the optimal model from db based on constraints.
// Pure heuristics — no LLM calls.
func SelectModel(constraints StepConstraints, db []ModelEntry) (*RoutingDecision, error) {
	if len(db) == 0 {
		return nil, fmt.Errorf("empty model database")
	}

	// Exact model requested.
	if constraints.Model != "" && constraints.Model != "auto" {
		m := GetModel(db, constraints.Model)
		if m == nil {
			return nil, fmt.Errorf("model %q not found in database", constraints.Model)
		}
		return &RoutingDecision{
			ModelID:      m.ID,
			Provider:     m.Provider,
			AccessType:   resolveAccessType(m, constraints),
			Reason:       "exact model requested",
			CostPer1KIn:  m.CostPer1KIn,
			CostPer1KOut: m.CostPer1KOut,
			MMLUScore:    m.MMLUScore,
			SWEScore:     m.SWEScore,
		}, nil
	}

	// Filter eligible models.
	var candidates []ModelEntry
	for _, m := range db {
		if !meetsConstraints(m, constraints) {
			continue
		}
		candidates = append(candidates, m)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no model satisfies constraints: provider=%q min_mmlu=%.0f min_swe=%.0f max_cost=%.4f requires=%v",
			constraints.Provider, constraints.MinMMLU, constraints.MinSWE, constraints.MaxCost, constraints.Requires)
	}

	// Score each candidate and pick the best.
	var best *RoutingDecision
	bestScore := math.Inf(-1)

	for _, m := range candidates {
		score := scoreModel(m, constraints)
		if score > bestScore {
			bestScore = score
			accessType := resolveAccessType(&m, constraints)
			reason := buildReason(m, constraints, accessType)
			best = &RoutingDecision{
				ModelID:      m.ID,
				Provider:     m.Provider,
				AccessType:   accessType,
				Reason:       reason,
				CostPer1KIn:  m.CostPer1KIn,
				CostPer1KOut: m.CostPer1KOut,
				MMLUScore:    m.MMLUScore,
				SWEScore:     m.SWEScore,
				Score:        score,
			}
		}
	}

	return best, nil
}

// meetsConstraints checks if a model satisfies the hard constraints.
func meetsConstraints(m ModelEntry, c StepConstraints) bool {
	// Provider filter.
	if c.Provider != "" && m.Provider != c.Provider {
		return false
	}

	// Access type filter.
	if c.AccessType != "" {
		switch c.AccessType {
		case "subscription":
			if !m.SubscriptionEligible || !c.SubscriptionActive {
				return false
			}
		case "local":
			if !m.Local {
				return false
			}
		case "api_key":
			if m.Local {
				return false
			}
			if m.SubscriptionEligible && c.SubscriptionActive {
				// API key access still allowed even with active subscription.
			}
		}
	}

	// Quality thresholds.
	if c.MinMMLU > 0 && m.MMLUScore < c.MinMMLU {
		return false
	}
	if c.MinSWE > 0 && m.SWEScore < c.MinSWE {
		return false
	}

	// Cost ceiling.
	if c.MaxCost > 0 && !m.Local {
		combinedCost := m.CombinedCostPer1K()
		if combinedCost > c.MaxCost {
			return false
		}
	}

	// Required capabilities.
	for _, req := range c.Requires {
		switch req {
		case "vision":
			if !m.Vision {
				return false
			}
		case "code_execution":
			if !m.CodeExecution {
				return false
			}
		}
	}

	return true
}

// scoreModel computes a routing score for a candidate model.
func scoreModel(m ModelEntry, c StepConstraints) float64 {
	var score float64

	// Subscription bonus: subscription = zero incremental cost.
	if c.SubscriptionActive && m.SubscriptionEligible {
		score += weightSubscription
	}

	// Local model bonus: zero cost, low latency for simple tasks.
	if m.Local {
		score += weightLocal
		if c.PreferLocal {
			score += 10.0 // Extra bonus when local preference is set.
		}
	}

	// MMLU score (normalized 0-100 -> 0-30 pts).
	if m.MMLUScore > 0 {
		score += (m.MMLUScore / 100.0) * weightMMLU
	}

	// SWE score (normalized 0-100 -> 0-20 pts).
	if m.SWEScore > 0 {
		score += (m.SWEScore / 100.0) * weightSWE
	}

	// Cost savings (inverse of cost ceiling).
	combinedCost := m.CombinedCostPer1K()
	if combinedCost <= 0 {
		score += weightCost // Free = max cost savings.
	} else if combinedCost < costCeiling {
		score += (1.0 - combinedCost/costCeiling) * weightCost
	}

	return score
}

// resolveAccessType determines how a model will be accessed.
func resolveAccessType(m *ModelEntry, c StepConstraints) string {
	if m.Local {
		return "local"
	}
	if c.SubscriptionActive && m.SubscriptionEligible {
		return "subscription"
	}
	return "api_key"
}

func buildReason(m ModelEntry, c StepConstraints, accessType string) string {
	switch {
	case m.Local:
		return fmt.Sprintf("local model (zero cost, MMLU=%.0f)", m.MMLUScore)
	case accessType == "subscription":
		return fmt.Sprintf("subscription preferred (zero incremental cost, MMLU=%.0f)", m.MMLUScore)
	default:
		return fmt.Sprintf("best scoring model (MMLU=%.0f, SWE=%.0f, cost=$%.4f/1K)",
			m.MMLUScore, m.SWEScore, m.CombinedCostPer1K())
	}
}

// DefaultConstraintsFromEnv reads routing defaults from environment variables.
func DefaultConstraintsFromEnv() StepConstraints {
	c := StepConstraints{}

	if os.Getenv("CLAUDE_CODE_SUBSCRIPTION") == "active" {
		c.SubscriptionActive = true
	}
	if os.Getenv("GT_PREFER_LOCAL") == "true" {
		c.PreferLocal = true
	}

	var minMMLU float64
	if _, err := fmt.Sscanf(os.Getenv("GT_MIN_MMLU"), "%g", &minMMLU); err == nil {
		c.MinMMLU = minMMLU
	}
	var minSWE float64
	if _, err := fmt.Sscanf(os.Getenv("GT_MIN_SWE"), "%g", &minSWE); err == nil {
		c.MinSWE = minSWE
	}
	var maxCost float64
	if _, err := fmt.Sscanf(os.Getenv("GT_MAX_COST"), "%g", &maxCost); err == nil {
		c.MaxCost = maxCost
	}

	if model := os.Getenv("GT_DEFAULT_MODEL"); model != "" {
		c.Model = model
	}
	if provider := os.Getenv("GT_PREFERRED_PROVIDER"); provider != "" {
		c.Provider = provider
	}

	return c
}
