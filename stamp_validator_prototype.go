// Package main provides a stamp validation pipeline prototype for the Wasteland.
//
// This prototype automatically validates completion evidence against wanted item
// requirements and generates stamps with appropriate confidence levels. It is
// designed to run as a pre-screening pass before human or rig-lead review.
//
// Pipeline stages:
//  1. EvidenceAnalyzer - extracts structured signals from free-text evidence
//  2. RequirementsMatcher - compares evidence signals against wanted item specs
//  3. ConfidenceCalculator - computes a 0.0-1.0 confidence score
//  4. StampGenerator - produces a Stamp record with valence and reasoning
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"
)

// ---------------------------------------------------------------------------
// Domain types (aligned with internal/wasteland and internal/doltserver)
// ---------------------------------------------------------------------------

// Valence holds multi-dimensional reputation signals from a stamp.
// Scores are on a 0-5 scale matching the existing reputation system.
type Valence struct {
	Quality     float64 `json:"quality"`
	Reliability float64 `json:"reliability"`
	Creativity  float64 `json:"creativity"`
}

// WantedItem is a minimal representation of a wasteland wanted board entry.
// Fields mirror internal/doltserver.WantedItem for the subset we need.
type WantedItem struct {
	ID          string
	Title       string
	Description string
	Tags        []string
	EffortLevel string // "trivial", "small", "medium", "large", "epic"
	Priority    int
}

// CompletionSubmission represents a rig's claim that they completed a wanted item.
type CompletionSubmission struct {
	CompletionID string
	WantedID     string
	RigHandle    string
	Evidence     string // free-text evidence field
	Validated    bool   // true if already validated by a prior run
}

// ---------------------------------------------------------------------------
// Validation result types
// ---------------------------------------------------------------------------

// ValidationOutcome enumerates the possible results of the validation pipeline.
type ValidationOutcome string

const (
	// OutcomeApproved means evidence strongly supports the completion claim.
	OutcomeApproved ValidationOutcome = "approved"
	// OutcomeNeedsReview means evidence is ambiguous and requires human review.
	OutcomeNeedsReview ValidationOutcome = "needs_review"
	// OutcomeRejected means evidence does not support the completion claim.
	OutcomeRejected ValidationOutcome = "rejected"
	// OutcomeSkipped means the completion was already validated or is empty.
	OutcomeSkipped ValidationOutcome = "skipped"
)

// DimensionScores holds per-dimension scores for the validation.
type DimensionScores struct {
	Quality     float64 // How well the evidence demonstrates quality work
	Reliability float64 // How verifiable/trustworthy the evidence is
	Creativity  float64 // How creative or novel the approach appears
}

// ValidationResult is the output of the full validation pipeline.
type ValidationResult struct {
	Outcome    ValidationOutcome
	Confidence float64 // 0.0-1.0 confidence in the outcome
	Dimensions DimensionScores
	Reasoning  []string // human-readable explanation of each scoring decision
	Stamp      *StampRecord
}

// StampRecord is the stamp that would be written to the reputation system.
type StampRecord struct {
	Subject    string // rig handle being stamped
	Valence    Valence
	Confidence float64
	Severity   string // "leaf", "branch", "root"
	SkillTags  []string
	Message    string // explanation of the validation decision
}

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

// ValidatorConfig controls validation thresholds and behavior.
type ValidatorConfig struct {
	// AutoStampFloor is the minimum confidence required to auto-approve.
	// Completions below this threshold get OutcomeNeedsReview.
	AutoStampFloor float64

	// RejectCeiling is the confidence ceiling below which we auto-reject.
	RejectCeiling float64

	// RequiredEvidenceTypes lists evidence types that must be present for
	// auto-approval. Empty means no hard requirements.
	RequiredEvidenceTypes []string

	// MinDescriptionWords is the minimum word count for the evidence text
	// to be considered substantive.
	MinDescriptionWords int

	// KeywordMatchThreshold is the fraction of wanted item keywords that
	// must appear in the evidence for a positive match (0.0-1.0).
	KeywordMatchThreshold float64
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() ValidatorConfig {
	return ValidatorConfig{
		AutoStampFloor:        0.7,
		RejectCeiling:         0.3,
		RequiredEvidenceTypes: nil, // no hard requirements by default
		MinDescriptionWords:   10,
		KeywordMatchThreshold: 0.3,
	}
}

// ---------------------------------------------------------------------------
// Evidence analysis
// ---------------------------------------------------------------------------

// Evidence types detected in free-text evidence.
const (
	EvidencePRURL      = "pr_url"
	EvidenceCommitHash = "commit_hash"
	EvidenceIssueURL   = "issue_url"
	EvidenceCodeBlock  = "code_block"
)

// Regex patterns for extracting structured evidence.
var (
	prURLPattern     = regexp.MustCompile(`https?://github\.com/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+/pull/\d+`)
	issueURLPattern  = regexp.MustCompile(`https?://github\.com/[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+/issues/\d+`)
	commitHashPattern = regexp.MustCompile(`\b[0-9a-f]{7,40}\b`)
	codeBlockPattern  = regexp.MustCompile("```[\\s\\S]*?```")
)

// EvidenceSignals holds structured data extracted from free-text evidence.
type EvidenceSignals struct {
	PRURLs       []string
	IssueURLs    []string
	CommitHashes []string
	HasCodeBlock bool
	WordCount    int
	Types        []string // distinct evidence types found
}

// EvidenceAnalyzer extracts structured signals from free-text completion evidence.
type EvidenceAnalyzer struct{}

// Analyze parses evidence text and returns extracted signals.
func (ea *EvidenceAnalyzer) Analyze(evidence string) EvidenceSignals {
	signals := EvidenceSignals{}

	if strings.TrimSpace(evidence) == "" {
		return signals
	}

	// Extract PR URLs.
	signals.PRURLs = prURLPattern.FindAllString(evidence, -1)
	if len(signals.PRURLs) > 0 {
		signals.Types = append(signals.Types, EvidencePRURL)
	}

	// Extract issue URLs (exclude any that are also PR URLs).
	allIssueURLs := issueURLPattern.FindAllString(evidence, -1)
	for _, u := range allIssueURLs {
		isPR := false
		for _, pr := range signals.PRURLs {
			if u == pr {
				isPR = true
				break
			}
		}
		if !isPR {
			signals.IssueURLs = append(signals.IssueURLs, u)
		}
	}
	if len(signals.IssueURLs) > 0 {
		signals.Types = append(signals.Types, EvidenceIssueURL)
	}

	// Extract commit hashes. Filter out things that look like hex but are
	// too short or appear inside URLs (which we already captured).
	stripped := prURLPattern.ReplaceAllString(evidence, "")
	stripped = issueURLPattern.ReplaceAllString(stripped, "")
	candidates := commitHashPattern.FindAllString(stripped, -1)
	for _, c := range candidates {
		// Require at least 7 hex chars and at least one digit + one letter
		// to reduce false positives from plain numbers or words.
		if len(c) >= 7 && containsDigit(c) && containsHexLetter(c) {
			signals.CommitHashes = append(signals.CommitHashes, c)
		}
	}
	if len(signals.CommitHashes) > 0 {
		signals.Types = append(signals.Types, EvidenceCommitHash)
	}

	// Detect code blocks.
	if codeBlockPattern.MatchString(evidence) {
		signals.HasCodeBlock = true
		signals.Types = append(signals.Types, EvidenceCodeBlock)
	}

	// Word count (rough, split on whitespace).
	signals.WordCount = len(strings.Fields(evidence))

	return signals
}

func containsDigit(s string) bool {
	for _, c := range s {
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

func containsHexLetter(s string) bool {
	for _, c := range s {
		if (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Requirements matching
// ---------------------------------------------------------------------------

// MatchResult holds the outcome of comparing evidence against requirements.
type MatchResult struct {
	KeywordOverlap float64 // 0.0-1.0 fraction of wanted keywords found in evidence
	TagCoverage    float64 // 0.0-1.0 fraction of wanted tags addressed
	EffortMatch    float64 // 0.0-1.0 how well evidence volume matches effort level
	MatchedWords   []string
}

// RequirementsMatcher compares completion evidence against wanted item requirements.
type RequirementsMatcher struct {
	// stopWords are common words excluded from keyword matching.
	stopWords map[string]bool
}

// NewRequirementsMatcher creates a matcher with a default English stop-word list.
func NewRequirementsMatcher() *RequirementsMatcher {
	stops := map[string]bool{
		"the": true, "a": true, "an": true, "and": true, "or": true,
		"but": true, "in": true, "on": true, "at": true, "to": true,
		"for": true, "of": true, "with": true, "by": true, "from": true,
		"is": true, "it": true, "that": true, "this": true, "was": true,
		"are": true, "be": true, "has": true, "have": true, "had": true,
		"not": true, "as": true, "can": true, "will": true, "do": true,
		"if": true, "so": true, "no": true, "up": true, "out": true,
	}
	return &RequirementsMatcher{stopWords: stops}
}

// Match compares completion evidence text against the wanted item and returns
// a match result with keyword overlap, tag coverage, and effort match scores.
func (rm *RequirementsMatcher) Match(wanted WantedItem, evidence string, signals EvidenceSignals) MatchResult {
	result := MatchResult{}

	// Extract significant keywords from the wanted item title + description.
	wantedText := strings.ToLower(wanted.Title + " " + wanted.Description)
	wantedWords := rm.extractKeywords(wantedText)

	evidenceLower := strings.ToLower(evidence)

	// Keyword overlap: what fraction of wanted keywords appear in the evidence.
	if len(wantedWords) > 0 {
		matched := 0
		for _, w := range wantedWords {
			if strings.Contains(evidenceLower, w) {
				matched++
				result.MatchedWords = append(result.MatchedWords, w)
			}
		}
		result.KeywordOverlap = float64(matched) / float64(len(wantedWords))
	}

	// Tag coverage: what fraction of wanted tags appear in the evidence.
	if len(wanted.Tags) > 0 {
		tagHits := 0
		for _, tag := range wanted.Tags {
			if strings.Contains(evidenceLower, strings.ToLower(tag)) {
				tagHits++
			}
		}
		result.TagCoverage = float64(tagHits) / float64(len(wanted.Tags))
	}

	// Effort match: does the evidence volume match the expected effort level?
	result.EffortMatch = rm.scoreEffortMatch(wanted.EffortLevel, signals)

	return result
}

// extractKeywords splits text into words, removes stop words, and returns
// the remaining significant terms (deduplicated).
func (rm *RequirementsMatcher) extractKeywords(text string) []string {
	words := strings.Fields(text)
	seen := make(map[string]bool)
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'")
		w = strings.ToLower(w)
		if len(w) < 3 || rm.stopWords[w] || seen[w] {
			continue
		}
		seen[w] = true
		keywords = append(keywords, w)
	}
	return keywords
}

// scoreEffortMatch compares evidence volume against expected effort level.
// A trivial task with a huge amount of evidence is suspicious (inflated).
// An epic task with a one-liner is suspicious (insufficient).
func (rm *RequirementsMatcher) scoreEffortMatch(effortLevel string, signals EvidenceSignals) float64 {
	// Expected minimum word counts by effort level.
	expectedMinWords := map[string]int{
		"trivial": 5,
		"small":   15,
		"medium":  30,
		"large":   60,
		"epic":    100,
	}

	// Expected maximum word counts (beyond this feels inflated).
	expectedMaxWords := map[string]int{
		"trivial": 100,
		"small":   300,
		"medium":  600,
		"large":   1200,
		"epic":    5000,
	}

	minW, ok := expectedMinWords[strings.ToLower(effortLevel)]
	if !ok {
		// Unknown effort level; give a neutral score.
		return 0.5
	}
	maxW := expectedMaxWords[strings.ToLower(effortLevel)]

	wc := signals.WordCount

	switch {
	case wc < minW:
		// Too little evidence for the effort level.
		return float64(wc) / float64(minW) * 0.5
	case wc > maxW:
		// Suspiciously verbose for the effort level (mild penalty).
		return 0.7
	default:
		return 1.0
	}
}

// ---------------------------------------------------------------------------
// Confidence calculation
// ---------------------------------------------------------------------------

// ConfidenceCalculator computes an overall confidence score from evidence
// signals and requirements match results.
type ConfidenceCalculator struct {
	Config ValidatorConfig
}

// ConfidenceBreakdown explains how the final confidence was computed.
type ConfidenceBreakdown struct {
	EvidenceQuality float64 // 0.0-1.0 from evidence types
	RequirementsMatch float64 // 0.0-1.0 from keyword/tag matching
	Completeness    float64 // 0.0-1.0 from description quality
	Final           float64 // weighted combination
}

// Calculate computes a confidence score from evidence signals and match results.
// The score is a weighted average of three sub-scores:
//   - Evidence quality (40%): are there verifiable artifacts (PRs, commits)?
//   - Requirements match (35%): does the evidence relate to the wanted item?
//   - Completeness (25%): is the description substantive?
func (cc *ConfidenceCalculator) Calculate(signals EvidenceSignals, match MatchResult) ConfidenceBreakdown {
	bd := ConfidenceBreakdown{}

	// Evidence quality: PRs > commits > code blocks > issue URLs > nothing.
	bd.EvidenceQuality = cc.scoreEvidenceQuality(signals)

	// Requirements match: weighted average of keyword overlap, tag coverage,
	// and effort match.
	bd.RequirementsMatch = 0.5*match.KeywordOverlap + 0.3*match.TagCoverage + 0.2*match.EffortMatch

	// Completeness: based on word count relative to the configured minimum.
	if signals.WordCount >= cc.Config.MinDescriptionWords {
		bd.Completeness = math.Min(1.0, float64(signals.WordCount)/float64(cc.Config.MinDescriptionWords*3))
	} else if signals.WordCount > 0 {
		bd.Completeness = float64(signals.WordCount) / float64(cc.Config.MinDescriptionWords) * 0.5
	}

	// Weighted combination.
	bd.Final = 0.40*bd.EvidenceQuality + 0.35*bd.RequirementsMatch + 0.25*bd.Completeness

	// Clamp to [0, 1].
	bd.Final = math.Max(0.0, math.Min(1.0, bd.Final))

	// Round to 2 decimal places.
	bd.Final = math.Round(bd.Final*100) / 100

	return bd
}

// scoreEvidenceQuality assigns a quality score based on the types of evidence
// present. Multiple evidence types compound the score.
func (cc *ConfidenceCalculator) scoreEvidenceQuality(signals EvidenceSignals) float64 {
	score := 0.0

	// PR URLs are the strongest evidence.
	if len(signals.PRURLs) > 0 {
		score += 0.5
	}
	// Commit hashes are good secondary evidence.
	if len(signals.CommitHashes) > 0 {
		score += 0.2
	}
	// Issue URLs show context awareness.
	if len(signals.IssueURLs) > 0 {
		score += 0.1
	}
	// Code blocks show technical detail.
	if signals.HasCodeBlock {
		score += 0.1
	}
	// Baseline for having any text at all.
	if signals.WordCount > 0 {
		score += 0.1
	}

	return math.Min(1.0, score)
}

// ---------------------------------------------------------------------------
// Stamp generation
// ---------------------------------------------------------------------------

// StampGenerator produces a StampRecord from the validation results.
type StampGenerator struct {
	Config ValidatorConfig
}

// Generate creates a StampRecord from the validation analysis.
// The valence scores (0-5 scale) are derived from the confidence breakdown
// and match results.
func (sg *StampGenerator) Generate(
	rigHandle string,
	wanted WantedItem,
	confidence ConfidenceBreakdown,
	match MatchResult,
	signals EvidenceSignals,
) StampRecord {
	// Derive valence on the 0-5 scale used by the reputation system.
	valence := Valence{
		Quality:     sg.scaleToFive(confidence.RequirementsMatch),
		Reliability: sg.scaleToFive(confidence.EvidenceQuality),
		Creativity:  sg.scaleToFive(sg.creativitySignal(signals, match)),
	}

	// Severity maps from effort level.
	severity := sg.effortToSeverity(wanted.EffortLevel)

	// Build explanation message.
	message := sg.buildMessage(confidence, match, signals)

	return StampRecord{
		Subject:    rigHandle,
		Valence:    valence,
		Confidence: confidence.Final,
		Severity:   severity,
		SkillTags:  wanted.Tags,
		Message:    message,
	}
}

// scaleToFive converts a 0.0-1.0 score to the 0-5 reputation scale,
// rounded to one decimal place.
func (sg *StampGenerator) scaleToFive(score float64) float64 {
	return math.Round(score*50) / 10
}

// creativitySignal estimates a creativity score from evidence characteristics.
// Code blocks and high word counts suggest more creative/detailed work.
func (sg *StampGenerator) creativitySignal(signals EvidenceSignals, match MatchResult) float64 {
	score := 0.3 // baseline

	if signals.HasCodeBlock {
		score += 0.3
	}
	if signals.WordCount > 50 {
		score += 0.2
	}
	// High keyword overlap can indicate thoughtful alignment with the task.
	score += match.KeywordOverlap * 0.2

	return math.Min(1.0, score)
}

// effortToSeverity maps wanted item effort levels to stamp severity values.
func (sg *StampGenerator) effortToSeverity(effort string) string {
	switch strings.ToLower(effort) {
	case "large", "epic":
		return "branch"
	case "trivial", "small":
		return "leaf"
	default:
		return "leaf"
	}
}

// buildMessage constructs a human-readable explanation of the stamp.
func (sg *StampGenerator) buildMessage(confidence ConfidenceBreakdown, match MatchResult, signals EvidenceSignals) string {
	var parts []string

	parts = append(parts, fmt.Sprintf("Confidence: %.0f%%", confidence.Final*100))

	// Evidence summary.
	var evTypes []string
	if len(signals.PRURLs) > 0 {
		evTypes = append(evTypes, fmt.Sprintf("%d PR(s)", len(signals.PRURLs)))
	}
	if len(signals.CommitHashes) > 0 {
		evTypes = append(evTypes, fmt.Sprintf("%d commit(s)", len(signals.CommitHashes)))
	}
	if signals.HasCodeBlock {
		evTypes = append(evTypes, "code blocks")
	}
	if len(evTypes) > 0 {
		parts = append(parts, "Evidence: "+strings.Join(evTypes, ", "))
	} else {
		parts = append(parts, "Evidence: description only")
	}

	// Match summary.
	parts = append(parts, fmt.Sprintf("Keyword match: %.0f%%", match.KeywordOverlap*100))

	if len(match.MatchedWords) > 0 {
		shown := match.MatchedWords
		if len(shown) > 5 {
			shown = shown[:5]
		}
		parts = append(parts, "Matched: "+strings.Join(shown, ", "))
	}

	return strings.Join(parts, ". ")
}

// ---------------------------------------------------------------------------
// Main pipeline
// ---------------------------------------------------------------------------

// ValidateCompletion runs the full validation pipeline: analyze evidence,
// match requirements, calculate confidence, and generate a stamp.
//
// Returns a ValidationResult with the outcome, confidence, dimension scores,
// reasoning, and (if applicable) a generated stamp record.
func ValidateCompletion(wanted WantedItem, completion CompletionSubmission, config ValidatorConfig) ValidationResult {
	result := ValidationResult{}

	// Short-circuit: already validated.
	if completion.Validated {
		result.Outcome = OutcomeSkipped
		result.Reasoning = append(result.Reasoning, "Completion already validated; skipping.")
		return result
	}

	// Short-circuit: empty evidence.
	if strings.TrimSpace(completion.Evidence) == "" {
		result.Outcome = OutcomeRejected
		result.Confidence = 0.0
		result.Reasoning = append(result.Reasoning, "No evidence provided.")
		return result
	}

	// Stage 1: Analyze evidence.
	analyzer := &EvidenceAnalyzer{}
	signals := analyzer.Analyze(completion.Evidence)
	result.Reasoning = append(result.Reasoning,
		fmt.Sprintf("Evidence analysis: %d words, types=%v", signals.WordCount, signals.Types))

	// Stage 2: Match requirements.
	matcher := NewRequirementsMatcher()
	match := matcher.Match(wanted, completion.Evidence, signals)
	result.Reasoning = append(result.Reasoning,
		fmt.Sprintf("Requirements match: keywords=%.0f%%, tags=%.0f%%, effort=%.0f%%",
			match.KeywordOverlap*100, match.TagCoverage*100, match.EffortMatch*100))

	// Stage 3: Calculate confidence.
	calc := &ConfidenceCalculator{Config: config}
	confidence := calc.Calculate(signals, match)
	result.Confidence = confidence.Final
	result.Reasoning = append(result.Reasoning,
		fmt.Sprintf("Confidence breakdown: evidence=%.2f, requirements=%.2f, completeness=%.2f => final=%.2f",
			confidence.EvidenceQuality, confidence.RequirementsMatch, confidence.Completeness, confidence.Final))

	// Stage 4: Determine outcome.
	switch {
	case confidence.Final >= config.AutoStampFloor:
		result.Outcome = OutcomeApproved
		result.Reasoning = append(result.Reasoning,
			fmt.Sprintf("Auto-approved: confidence %.2f >= threshold %.2f", confidence.Final, config.AutoStampFloor))
	case confidence.Final <= config.RejectCeiling:
		result.Outcome = OutcomeRejected
		result.Reasoning = append(result.Reasoning,
			fmt.Sprintf("Auto-rejected: confidence %.2f <= threshold %.2f", confidence.Final, config.RejectCeiling))
	default:
		result.Outcome = OutcomeNeedsReview
		result.Reasoning = append(result.Reasoning,
			fmt.Sprintf("Needs review: confidence %.2f between reject (%.2f) and approve (%.2f)",
				confidence.Final, config.RejectCeiling, config.AutoStampFloor))
	}

	// Check required evidence types.
	if result.Outcome == OutcomeApproved && len(config.RequiredEvidenceTypes) > 0 {
		for _, reqType := range config.RequiredEvidenceTypes {
			found := false
			for _, t := range signals.Types {
				if t == reqType {
					found = true
					break
				}
			}
			if !found {
				result.Outcome = OutcomeNeedsReview
				result.Reasoning = append(result.Reasoning,
					fmt.Sprintf("Downgraded to review: missing required evidence type %q", reqType))
				break
			}
		}
	}

	// Set dimension scores.
	result.Dimensions = DimensionScores{
		Quality:     confidence.RequirementsMatch,
		Reliability: confidence.EvidenceQuality,
		Creativity:  confidence.Completeness,
	}

	// Stage 5: Generate stamp (only for approved or needs-review).
	if result.Outcome != OutcomeRejected && result.Outcome != OutcomeSkipped {
		gen := &StampGenerator{Config: config}
		stamp := gen.Generate(completion.RigHandle, wanted, confidence, match, signals)
		result.Stamp = &stamp
	}

	return result
}

// ValenceJSON serializes a Valence struct to JSON for stamp storage.
func ValenceJSON(v Valence) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("marshaling valence: %w", err)
	}
	return string(b), nil
}
