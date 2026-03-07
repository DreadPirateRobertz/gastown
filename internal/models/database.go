// Package models provides a model capability database, routing logic,
// and usage tracking for cost-aware orchestration in Gas Town.
package models

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// ModelEntry describes a model's capabilities, benchmarks, and pricing.
type ModelEntry struct {
	ID           string   `json:"id" toml:"id"`
	Provider     string   `json:"provider" toml:"provider"`
	Name         string   `json:"name" toml:"name"`
	OpenRouterID string   `json:"openrouter_id" toml:"openrouter_id"`

	// Benchmark scores (static, overridable via ~/.gt/models.toml).
	MMLUScore float64 `json:"mmlu_score" toml:"mmlu"`
	SWEScore  float64 `json:"swe_score" toml:"swe"`

	// Capabilities.
	Vision        bool `json:"vision" toml:"vision"`
	CodeExecution bool `json:"code_execution" toml:"code_execution"`
	ContextWindow int  `json:"context_window" toml:"context_window"`

	// Pricing in USD per 1K tokens (fetched from OpenRouter, cached 24h).
	CostPer1KIn  float64 `json:"cost_per_1k_in" toml:"cost_per_1k_in"`
	CostPer1KOut float64 `json:"cost_per_1k_out" toml:"cost_per_1k_out"`

	// SubscriptionEligible indicates the model can be accessed via a
	// subscription (e.g. Claude Code for Anthropic models).
	SubscriptionEligible bool `json:"subscription_eligible" toml:"subscription_eligible"`

	// Local indicates the model runs locally (e.g. via ollama) with zero cost.
	Local bool `json:"local" toml:"local"`

	// GoodFor lists task categories this model excels at.
	GoodFor []string `json:"good_for" toml:"good_for"`
}

// CombinedCostPer1K returns the average of input and output cost per 1K tokens.
func (m *ModelEntry) CombinedCostPer1K() float64 {
	return (m.CostPer1KIn + m.CostPer1KOut) / 2
}

// staticDB contains built-in model entries with benchmark scores.
// Pricing is updated from OpenRouter; these are fallback values.
var staticDB = []ModelEntry{
	// Anthropic models
	{
		ID: "claude-opus-4-6", Provider: "anthropic", Name: "Claude Opus 4.6",
		OpenRouterID: "anthropic/claude-opus-4-6",
		MMLUScore: 90.0, SWEScore: 72.0,
		Vision: true, ContextWindow: 200000,
		CostPer1KIn: 0.015, CostPer1KOut: 0.075,
		SubscriptionEligible: true,
		GoodFor:              []string{"coding", "reasoning", "analysis"},
	},
	{
		ID: "claude-sonnet-4-6", Provider: "anthropic", Name: "Claude Sonnet 4.6",
		OpenRouterID: "anthropic/claude-sonnet-4-6",
		MMLUScore: 88.0, SWEScore: 65.0,
		Vision: true, ContextWindow: 200000,
		CostPer1KIn: 0.003, CostPer1KOut: 0.015,
		SubscriptionEligible: true,
		GoodFor:              []string{"coding", "reasoning"},
	},
	{
		ID: "claude-haiku-4-5", Provider: "anthropic", Name: "Claude Haiku 4.5",
		OpenRouterID: "anthropic/claude-haiku-4-5",
		MMLUScore: 82.0, SWEScore: 45.0,
		Vision: true, ContextWindow: 200000,
		CostPer1KIn: 0.0008, CostPer1KOut: 0.004,
		SubscriptionEligible: true,
		GoodFor:              []string{"quick-tasks", "summarization"},
	},
	// OpenAI models
	{
		ID: "gpt-4o", Provider: "openai", Name: "GPT-4o",
		OpenRouterID: "openai/gpt-4o",
		MMLUScore: 88.7, SWEScore: 53.0,
		Vision: true, CodeExecution: true, ContextWindow: 128000,
		CostPer1KIn: 0.0025, CostPer1KOut: 0.01,
		GoodFor: []string{"coding", "reasoning"},
	},
	{
		ID: "gpt-4o-mini", Provider: "openai", Name: "GPT-4o Mini",
		OpenRouterID: "openai/gpt-4o-mini",
		MMLUScore: 82.0, SWEScore: 35.0,
		Vision: true, ContextWindow: 128000,
		CostPer1KIn: 0.00015, CostPer1KOut: 0.0006,
		GoodFor: []string{"quick-tasks", "summarization"},
	},
	// DeepSeek models
	{
		ID: "deepseek-v3", Provider: "deepseek", Name: "DeepSeek V3",
		OpenRouterID: "deepseek/deepseek-chat",
		MMLUScore: 88.5, SWEScore: 48.0,
		ContextWindow: 131072,
		CostPer1KIn:   0.00014, CostPer1KOut: 0.00028,
		GoodFor: []string{"coding", "reasoning"},
	},
	// Google models
	{
		ID: "gemini-2.5-pro", Provider: "google", Name: "Gemini 2.5 Pro",
		OpenRouterID: "google/gemini-2.5-pro",
		MMLUScore: 89.0, SWEScore: 55.0,
		Vision: true, ContextWindow: 1000000,
		CostPer1KIn: 0.00125, CostPer1KOut: 0.005,
		GoodFor: []string{"coding", "reasoning", "analysis"},
	},
	// Local models (ollama)
	{
		ID: "ollama/llama3.1", Provider: "ollama", Name: "Llama 3.1 (local)",
		MMLUScore: 73.0, SWEScore: 25.0,
		ContextWindow: 131072,
		CostPer1KIn: 0, CostPer1KOut: 0,
		Local:   true,
		GoodFor: []string{"quick-tasks", "summarization", "formatting"},
	},
	{
		ID: "ollama/codellama", Provider: "ollama", Name: "CodeLlama (local)",
		MMLUScore: 62.0, SWEScore: 20.0,
		ContextWindow: 16384,
		CostPer1KIn: 0, CostPer1KOut: 0,
		Local:   true,
		GoodFor: []string{"coding", "quick-tasks"},
	},
	{
		ID: "ollama/deepseek-coder-v2", Provider: "ollama", Name: "DeepSeek Coder V2 (local)",
		MMLUScore: 79.0, SWEScore: 35.0,
		ContextWindow: 131072,
		CostPer1KIn: 0, CostPer1KOut: 0,
		Local:   true,
		GoodFor: []string{"coding", "reasoning"},
	},
	{
		ID: "ollama/qwen2.5-coder", Provider: "ollama", Name: "Qwen 2.5 Coder (local)",
		MMLUScore: 76.0, SWEScore: 30.0,
		ContextWindow: 131072,
		CostPer1KIn: 0, CostPer1KOut: 0,
		Local:   true,
		GoodFor: []string{"coding", "quick-tasks"},
	},
}

// GetModel returns the model entry with the given ID, or nil if not found.
func GetModel(db []ModelEntry, id string) *ModelEntry {
	for i := range db {
		if db[i].ID == id {
			return &db[i]
		}
	}
	return nil
}

// LoadDatabase merges: static benchmarks -> OpenRouter pricing -> ~/.gt/models.toml overrides.
// gtDir is the path to ~/.gt (or equivalent). Pass "" to skip file-based loading.
func LoadDatabase(gtDir string) []ModelEntry {
	// Start with a copy of the static database.
	db := make([]ModelEntry, len(staticDB))
	copy(db, staticDB)

	if gtDir == "" {
		return db
	}

	// Try to load OpenRouter pricing cache.
	cachePath := filepath.Join(gtDir, "models_pricing_cache.json")
	applyPricingCache(&db, cachePath)

	// Try to refresh pricing if cache is stale (>24h).
	refreshPricingCache(gtDir, cachePath)

	// Apply user overrides from models.toml.
	overridePath := filepath.Join(gtDir, "models.toml")
	applyUserOverrides(&db, overridePath)

	// Detect locally available ollama models.
	detectOllamaModels(&db)

	return db
}

// openRouterPricingCache represents the cached OpenRouter pricing data.
type openRouterPricingCache struct {
	FetchedAt time.Time                    `json:"fetched_at"`
	Models    map[string]openRouterPricing `json:"models"`
}

type openRouterPricing struct {
	PromptCost     float64 `json:"prompt_cost"`     // USD per 1K tokens
	CompletionCost float64 `json:"completion_cost"` // USD per 1K tokens
}

func applyPricingCache(db *[]ModelEntry, cachePath string) {
	data, err := os.ReadFile(cachePath) //nolint:gosec // G304: path from config
	if err != nil {
		return
	}
	var cache openRouterPricingCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}
	for i := range *db {
		m := &(*db)[i]
		if m.OpenRouterID == "" {
			continue
		}
		if pricing, ok := cache.Models[m.OpenRouterID]; ok {
			m.CostPer1KIn = pricing.PromptCost
			m.CostPer1KOut = pricing.CompletionCost
		}
	}
}

// pricingCacheTTL is the duration before the pricing cache is considered stale.
const pricingCacheTTL = 24 * time.Hour

// openRouterTimeout is the HTTP timeout for fetching pricing.
const openRouterTimeout = 5 * time.Second

func refreshPricingCache(gtDir, cachePath string) {
	// Check if cache is fresh enough.
	info, err := os.Stat(cachePath)
	if err == nil && time.Since(info.ModTime()) < pricingCacheTTL {
		return
	}

	// Fetch from OpenRouter (non-fatal on failure).
	client := &http.Client{Timeout: openRouterTimeout}
	resp, err := client.Get("https://openrouter.ai/api/v1/models") //nolint:noctx // simple GET
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	// Parse OpenRouter response.
	var orResp struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &orResp); err != nil {
		return
	}

	cache := openRouterPricingCache{
		FetchedAt: time.Now(),
		Models:    make(map[string]openRouterPricing),
	}
	for _, m := range orResp.Data {
		var prompt, completion float64
		// OpenRouter returns per-token pricing as strings; convert to per-1K.
		if _, err := fmt.Sscanf(m.Pricing.Prompt, "%g", &prompt); err == nil {
			prompt *= 1000
		}
		if _, err := fmt.Sscanf(m.Pricing.Completion, "%g", &completion); err == nil {
			completion *= 1000
		}
		cache.Models[m.ID] = openRouterPricing{
			PromptCost:     prompt,
			CompletionCost: completion,
		}
	}

	// Write cache (best-effort).
	cacheData, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = os.MkdirAll(gtDir, 0755)
	_ = os.WriteFile(cachePath, cacheData, 0644) //nolint:gosec // G306: cache file
}

// userOverrideFile represents the structure of ~/.gt/models.toml.
type userOverrideFile struct {
	Models map[string]userModelOverride `toml:"models"`
}

type userModelOverride struct {
	Provider      string   `toml:"provider"`
	Name          string   `toml:"name"`
	MMLU          float64  `toml:"mmlu"`
	SWE           float64  `toml:"swe"`
	CostPer1KIn   float64  `toml:"cost_per_1k_in"`
	CostPer1KOut  float64  `toml:"cost_per_1k_out"`
	ContextWindow int      `toml:"context_window"`
	Vision        bool     `toml:"vision"`
	CodeExecution bool     `toml:"code_execution"`
	Local         bool     `toml:"local"`
	GoodFor       []string `toml:"good_for"`
}

func applyUserOverrides(db *[]ModelEntry, path string) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from config
	if err != nil {
		return
	}
	var overrides userOverrideFile
	if _, err := toml.Decode(string(data), &overrides); err != nil {
		return
	}

	for id, o := range overrides.Models {
		existing := GetModel(*db, id)
		if existing != nil {
			// Override fields if set.
			if o.MMLU > 0 {
				existing.MMLUScore = o.MMLU
			}
			if o.SWE > 0 {
				existing.SWEScore = o.SWE
			}
			if o.CostPer1KIn > 0 || o.Local {
				existing.CostPer1KIn = o.CostPer1KIn
			}
			if o.CostPer1KOut > 0 || o.Local {
				existing.CostPer1KOut = o.CostPer1KOut
			}
			if o.ContextWindow > 0 {
				existing.ContextWindow = o.ContextWindow
			}
			if o.Vision {
				existing.Vision = true
			}
			if o.CodeExecution {
				existing.CodeExecution = true
			}
			if o.Local {
				existing.Local = true
			}
			if len(o.GoodFor) > 0 {
				existing.GoodFor = o.GoodFor
			}
			if o.Provider != "" {
				existing.Provider = o.Provider
			}
			if o.Name != "" {
				existing.Name = o.Name
			}
		} else {
			// Add new model.
			entry := ModelEntry{
				ID:            id,
				Provider:      o.Provider,
				Name:          o.Name,
				MMLUScore:     o.MMLU,
				SWEScore:      o.SWE,
				CostPer1KIn:   o.CostPer1KIn,
				CostPer1KOut:  o.CostPer1KOut,
				ContextWindow: o.ContextWindow,
				Vision:        o.Vision,
				CodeExecution: o.CodeExecution,
				Local:         o.Local,
				GoodFor:       o.GoodFor,
			}
			if entry.Name == "" {
				entry.Name = id
			}
			if entry.Provider == "" {
				entry.Provider = "custom"
			}
			*db = append(*db, entry)
		}
	}
}

// detectOllamaModels checks if ollama is available and marks local models
// as available or adds discovered ollama models to the database.
func detectOllamaModels(db *[]ModelEntry) {
	// Check if OLLAMA_HOST is set or ollama is reachable.
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(host + "/api/tags") //nolint:noctx // simple GET
	if err != nil {
		// Ollama not running — mark all local models as unavailable by removing them.
		// Actually, keep them in the DB but the router will check availability.
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var tagsResp struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &tagsResp); err != nil {
		return
	}

	// Build set of available models.
	available := make(map[string]bool)
	for _, m := range tagsResp.Models {
		// Ollama returns names like "llama3.1:latest" — normalize.
		name := strings.Split(m.Name, ":")[0]
		available[name] = true
		available["ollama/"+name] = true
	}

	// Add any ollama models that aren't in the static DB.
	for name := range available {
		if !strings.HasPrefix(name, "ollama/") {
			continue
		}
		if GetModel(*db, name) == nil {
			// Discovered a new local model not in static DB.
			shortName := strings.TrimPrefix(name, "ollama/")
			*db = append(*db, ModelEntry{
				ID:            name,
				Provider:      "ollama",
				Name:          shortName + " (local)",
				MMLUScore:     50.0, // Conservative default
				SWEScore:      15.0,
				ContextWindow: 8192,
				CostPer1KIn:   0,
				CostPer1KOut:  0,
				Local:         true,
				GoodFor:       []string{"quick-tasks"},
			})
		}
	}
}

// IsOllamaAvailable checks if ollama is reachable.
func IsOllamaAvailable() bool {
	host := os.Getenv("OLLAMA_HOST")
	if host == "" {
		host = "http://localhost:11434"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(host + "/api/tags") //nolint:noctx // simple GET
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
