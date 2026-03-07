package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// sampleWanted returns a typical wanted item for testing.
func sampleWanted() WantedItem {
	return WantedItem{
		ID:          "w-test-001",
		Title:       "Build automated stamp validation pipeline",
		Description: "Create a validation pipeline that checks completion evidence against wanted item requirements and generates stamps with confidence scores.",
		Tags:        []string{"golang", "wasteland", "reputation"},
		EffortLevel: "medium",
		Priority:    3,
	}
}

// ---------------------------------------------------------------------------
// Full pipeline tests
// ---------------------------------------------------------------------------

func TestValidateCompletion_HighConfidence(t *testing.T) {
	wanted := sampleWanted()
	completion := CompletionSubmission{
		CompletionID: "c-001",
		WantedID:     wanted.ID,
		RigHandle:    "zhora",
		Evidence: `Implemented the stamp validation pipeline in Go.

PR: https://github.com/steveyegge/gastown/pull/2430

The pipeline has four stages:
1. EvidenceAnalyzer extracts PR URLs, commit hashes, and code blocks from evidence text
2. RequirementsMatcher compares evidence keywords against the wanted item description
3. ConfidenceCalculator computes a weighted confidence score from evidence quality, requirements match, and completeness
4. StampGenerator produces a stamp record with valence scores on the 0-5 reputation scale

Commit: a1b2c3d4e5f6 added the core validation logic.

Tags covered: golang, wasteland, reputation system integration.

` + "```go\nfunc ValidateCompletion(wanted, completion) ValidationResult { ... }\n```",
	}

	config := DefaultConfig()
	result := ValidateCompletion(wanted, completion, config)

	if result.Outcome != OutcomeApproved {
		t.Errorf("expected OutcomeApproved, got %s\nReasoning: %s",
			result.Outcome, strings.Join(result.Reasoning, "\n"))
	}
	if result.Confidence < 0.7 {
		t.Errorf("expected confidence >= 0.7, got %.2f", result.Confidence)
	}
	if result.Stamp == nil {
		t.Fatal("expected a stamp to be generated")
	}
	if result.Stamp.Subject != "zhora" {
		t.Errorf("expected stamp subject 'zhora', got %q", result.Stamp.Subject)
	}
	if result.Stamp.Severity != "leaf" {
		t.Errorf("expected severity 'leaf' for medium effort, got %q", result.Stamp.Severity)
	}
	if result.Stamp.Confidence != result.Confidence {
		t.Errorf("stamp confidence (%.2f) should match result confidence (%.2f)",
			result.Stamp.Confidence, result.Confidence)
	}
	// Valence scores should be positive.
	if result.Stamp.Valence.Quality <= 0 {
		t.Errorf("expected positive quality valence, got %.2f", result.Stamp.Valence.Quality)
	}
	if result.Stamp.Valence.Reliability <= 0 {
		t.Errorf("expected positive reliability valence, got %.2f", result.Stamp.Valence.Reliability)
	}
}

func TestValidateCompletion_LowConfidence(t *testing.T) {
	wanted := sampleWanted()
	completion := CompletionSubmission{
		CompletionID: "c-002",
		WantedID:     wanted.ID,
		RigHandle:    "mystery-rig",
		Evidence:     "I did the thing. It works now.",
	}

	config := DefaultConfig()
	result := ValidateCompletion(wanted, completion, config)

	// Vague evidence with no URLs or relevant keywords should not be approved.
	if result.Outcome == OutcomeApproved {
		t.Errorf("vague evidence should not be approved; got %s with confidence %.2f",
			result.Outcome, result.Confidence)
	}
	if result.Confidence >= 0.7 {
		t.Errorf("expected confidence < 0.7 for vague evidence, got %.2f", result.Confidence)
	}
}

func TestValidateCompletion_Mismatched(t *testing.T) {
	wanted := sampleWanted() // about stamp validation pipeline
	completion := CompletionSubmission{
		CompletionID: "c-003",
		WantedID:     wanted.ID,
		RigHandle:    "wrong-rig",
		Evidence: `Fixed the CSS styling on the dashboard header.

PR: https://github.com/steveyegge/gastown/pull/2499

Changed the background color from blue to green and adjusted the font size
for mobile viewports. Also fixed a z-index issue with the dropdown menu.`,
	}

	config := DefaultConfig()
	result := ValidateCompletion(wanted, completion, config)

	// Evidence about CSS/dashboard should have low keyword match against
	// a stamp validation pipeline wanted item.
	if result.Outcome == OutcomeApproved {
		t.Errorf("mismatched evidence should not be approved; got %s", result.Outcome)
	}

	// The keyword overlap should be low.
	if result.Dimensions.Quality > 0.5 {
		t.Errorf("expected low quality dimension for mismatched evidence, got %.2f",
			result.Dimensions.Quality)
	}
}

func TestValidateCompletion_EmptyEvidence(t *testing.T) {
	wanted := sampleWanted()
	completion := CompletionSubmission{
		CompletionID: "c-004",
		WantedID:     wanted.ID,
		RigHandle:    "empty-rig",
		Evidence:     "",
	}

	config := DefaultConfig()
	result := ValidateCompletion(wanted, completion, config)

	if result.Outcome != OutcomeRejected {
		t.Errorf("empty evidence should be rejected, got %s", result.Outcome)
	}
	if result.Confidence != 0.0 {
		t.Errorf("empty evidence should have 0 confidence, got %.2f", result.Confidence)
	}
	if result.Stamp != nil {
		t.Error("no stamp should be generated for empty evidence")
	}
}

func TestValidateCompletion_WhitespaceOnlyEvidence(t *testing.T) {
	wanted := sampleWanted()
	completion := CompletionSubmission{
		CompletionID: "c-005",
		WantedID:     wanted.ID,
		RigHandle:    "whitespace-rig",
		Evidence:     "   \n\t  \n  ",
	}

	config := DefaultConfig()
	result := ValidateCompletion(wanted, completion, config)

	if result.Outcome != OutcomeRejected {
		t.Errorf("whitespace-only evidence should be rejected, got %s", result.Outcome)
	}
}

func TestValidateCompletion_AlreadyValidated(t *testing.T) {
	wanted := sampleWanted()
	completion := CompletionSubmission{
		CompletionID: "c-006",
		WantedID:     wanted.ID,
		RigHandle:    "done-rig",
		Evidence:     "Previously validated completion with lots of evidence.",
		Validated:    true,
	}

	config := DefaultConfig()
	result := ValidateCompletion(wanted, completion, config)

	if result.Outcome != OutcomeSkipped {
		t.Errorf("already-validated should be skipped, got %s", result.Outcome)
	}
	if result.Stamp != nil {
		t.Error("no stamp should be generated for skipped completions")
	}
}

// ---------------------------------------------------------------------------
// EvidenceAnalyzer tests
// ---------------------------------------------------------------------------

func TestEvidenceAnalyzer_PRURLs(t *testing.T) {
	ea := &EvidenceAnalyzer{}
	signals := ea.Analyze("See https://github.com/steveyegge/gastown/pull/2430 for details")

	if len(signals.PRURLs) != 1 {
		t.Fatalf("expected 1 PR URL, got %d", len(signals.PRURLs))
	}
	if signals.PRURLs[0] != "https://github.com/steveyegge/gastown/pull/2430" {
		t.Errorf("unexpected PR URL: %s", signals.PRURLs[0])
	}
	if !containsType(signals.Types, EvidencePRURL) {
		t.Error("expected pr_url in evidence types")
	}
}

func TestEvidenceAnalyzer_MultiplePRs(t *testing.T) {
	ea := &EvidenceAnalyzer{}
	signals := ea.Analyze(`
		First PR: https://github.com/steveyegge/gastown/pull/100
		Second PR: https://github.com/steveyegge/gastown/pull/200
	`)

	if len(signals.PRURLs) != 2 {
		t.Errorf("expected 2 PR URLs, got %d", len(signals.PRURLs))
	}
}

func TestEvidenceAnalyzer_CommitHashes(t *testing.T) {
	ea := &EvidenceAnalyzer{}
	signals := ea.Analyze("Fixed in commit a1b2c3d and also 9dea85be1234567890abcdef1234567890abcdef")

	if len(signals.CommitHashes) != 2 {
		t.Errorf("expected 2 commit hashes, got %d: %v", len(signals.CommitHashes), signals.CommitHashes)
	}
}

func TestEvidenceAnalyzer_CodeBlocks(t *testing.T) {
	ea := &EvidenceAnalyzer{}
	evidence := "Here is the fix:\n```go\nfunc foo() {}\n```\nDone."
	signals := ea.Analyze(evidence)

	if !signals.HasCodeBlock {
		t.Error("expected code block detection")
	}
	if !containsType(signals.Types, EvidenceCodeBlock) {
		t.Error("expected code_block in evidence types")
	}
}

func TestEvidenceAnalyzer_EmptyInput(t *testing.T) {
	ea := &EvidenceAnalyzer{}
	signals := ea.Analyze("")

	if len(signals.Types) != 0 {
		t.Errorf("expected no evidence types for empty input, got %v", signals.Types)
	}
	if signals.WordCount != 0 {
		t.Errorf("expected 0 word count, got %d", signals.WordCount)
	}
}

func TestEvidenceAnalyzer_IssueURLNotCountedAsPR(t *testing.T) {
	ea := &EvidenceAnalyzer{}
	signals := ea.Analyze("See https://github.com/steveyegge/gastown/issues/42 for context")

	if len(signals.PRURLs) != 0 {
		t.Errorf("issue URL should not be counted as PR URL")
	}
	if len(signals.IssueURLs) != 1 {
		t.Errorf("expected 1 issue URL, got %d", len(signals.IssueURLs))
	}
}

// ---------------------------------------------------------------------------
// RequirementsMatcher tests
// ---------------------------------------------------------------------------

func TestRequirementsMatcher_HighOverlap(t *testing.T) {
	matcher := NewRequirementsMatcher()
	wanted := sampleWanted()
	evidence := "Built the automated stamp validation pipeline with confidence scoring and evidence analysis for wanted items"
	signals := EvidenceSignals{WordCount: 50}

	result := matcher.Match(wanted, evidence, signals)

	if result.KeywordOverlap < 0.3 {
		t.Errorf("expected keyword overlap >= 0.3 for related evidence, got %.2f", result.KeywordOverlap)
	}
}

func TestRequirementsMatcher_NoOverlap(t *testing.T) {
	matcher := NewRequirementsMatcher()
	wanted := sampleWanted()
	evidence := "Fixed the CSS margin on the login page"
	signals := EvidenceSignals{WordCount: 8}

	result := matcher.Match(wanted, evidence, signals)

	if result.KeywordOverlap > 0.2 {
		t.Errorf("expected low keyword overlap for unrelated evidence, got %.2f", result.KeywordOverlap)
	}
}

func TestRequirementsMatcher_TagCoverage(t *testing.T) {
	matcher := NewRequirementsMatcher()
	wanted := sampleWanted() // tags: golang, wasteland, reputation
	evidence := "Written in golang for the wasteland board"
	signals := EvidenceSignals{WordCount: 7}

	result := matcher.Match(wanted, evidence, signals)

	// Should match at least 2 of 3 tags.
	if result.TagCoverage < 0.6 {
		t.Errorf("expected tag coverage >= 0.6, got %.2f", result.TagCoverage)
	}
}

func TestRequirementsMatcher_EffortMatchTrivial(t *testing.T) {
	matcher := NewRequirementsMatcher()
	wanted := WantedItem{
		ID:          "w-trivial",
		Title:       "Fix typo",
		Description: "Fix a typo in the README",
		EffortLevel: "trivial",
	}
	// Reasonable amount of evidence for a trivial task.
	signals := EvidenceSignals{WordCount: 20}
	result := matcher.Match(wanted, "Fixed the typo in README line 42", signals)

	if result.EffortMatch < 0.5 {
		t.Errorf("expected decent effort match for trivial task with brief evidence, got %.2f",
			result.EffortMatch)
	}
}

// ---------------------------------------------------------------------------
// ConfidenceCalculator tests
// ---------------------------------------------------------------------------

func TestConfidenceCalculator_HighEvidence(t *testing.T) {
	calc := &ConfidenceCalculator{Config: DefaultConfig()}
	signals := EvidenceSignals{
		PRURLs:       []string{"https://github.com/example/repo/pull/1"},
		CommitHashes: []string{"a1b2c3d"},
		HasCodeBlock: true,
		WordCount:    100,
		Types:        []string{EvidencePRURL, EvidenceCommitHash, EvidenceCodeBlock},
	}
	match := MatchResult{
		KeywordOverlap: 0.8,
		TagCoverage:    0.7,
		EffortMatch:    1.0,
	}

	bd := calc.Calculate(signals, match)

	if bd.Final < 0.7 {
		t.Errorf("expected high confidence (>= 0.7), got %.2f", bd.Final)
	}
	if bd.EvidenceQuality < 0.8 {
		t.Errorf("expected evidence quality >= 0.8, got %.2f", bd.EvidenceQuality)
	}
}

func TestConfidenceCalculator_NoEvidence(t *testing.T) {
	calc := &ConfidenceCalculator{Config: DefaultConfig()}
	signals := EvidenceSignals{WordCount: 3}
	match := MatchResult{KeywordOverlap: 0.1}

	bd := calc.Calculate(signals, match)

	if bd.Final > 0.3 {
		t.Errorf("expected low confidence (<= 0.3) for no evidence, got %.2f", bd.Final)
	}
}

func TestConfidenceCalculator_Clamped(t *testing.T) {
	calc := &ConfidenceCalculator{Config: DefaultConfig()}
	signals := EvidenceSignals{WordCount: 0}
	match := MatchResult{}

	bd := calc.Calculate(signals, match)

	if bd.Final < 0.0 || bd.Final > 1.0 {
		t.Errorf("confidence should be clamped to [0, 1], got %.2f", bd.Final)
	}
}

// ---------------------------------------------------------------------------
// StampGenerator tests
// ---------------------------------------------------------------------------

func TestStampGenerator_ValenceScale(t *testing.T) {
	gen := &StampGenerator{Config: DefaultConfig()}
	confidence := ConfidenceBreakdown{
		EvidenceQuality:   0.9,
		RequirementsMatch: 0.8,
		Completeness:      0.7,
		Final:             0.82,
	}
	match := MatchResult{KeywordOverlap: 0.8}
	signals := EvidenceSignals{HasCodeBlock: true, WordCount: 60}
	wanted := sampleWanted()

	stamp := gen.Generate("test-rig", wanted, confidence, match, signals)

	// Valence should be on 0-5 scale.
	if stamp.Valence.Quality < 0 || stamp.Valence.Quality > 5 {
		t.Errorf("quality valence should be 0-5, got %.2f", stamp.Valence.Quality)
	}
	if stamp.Valence.Reliability < 0 || stamp.Valence.Reliability > 5 {
		t.Errorf("reliability valence should be 0-5, got %.2f", stamp.Valence.Reliability)
	}
	if stamp.Valence.Creativity < 0 || stamp.Valence.Creativity > 5 {
		t.Errorf("creativity valence should be 0-5, got %.2f", stamp.Valence.Creativity)
	}
}

func TestStampGenerator_SeverityMapping(t *testing.T) {
	gen := &StampGenerator{Config: DefaultConfig()}
	confidence := ConfidenceBreakdown{Final: 0.8}
	match := MatchResult{}
	signals := EvidenceSignals{WordCount: 20}

	tests := []struct {
		effort   string
		expected string
	}{
		{"trivial", "leaf"},
		{"small", "leaf"},
		{"medium", "leaf"},
		{"large", "branch"},
		{"epic", "branch"},
		{"unknown", "leaf"},
	}

	for _, tt := range tests {
		wanted := WantedItem{EffortLevel: tt.effort}
		stamp := gen.Generate("rig", wanted, confidence, match, signals)
		if stamp.Severity != tt.expected {
			t.Errorf("effort %q: expected severity %q, got %q", tt.effort, tt.expected, stamp.Severity)
		}
	}
}

func TestStampGenerator_MessageContent(t *testing.T) {
	gen := &StampGenerator{Config: DefaultConfig()}
	confidence := ConfidenceBreakdown{Final: 0.75, EvidenceQuality: 0.8}
	match := MatchResult{KeywordOverlap: 0.6, MatchedWords: []string{"pipeline", "validation"}}
	signals := EvidenceSignals{
		PRURLs:   []string{"https://github.com/example/repo/pull/1"},
		WordCount: 50,
	}

	stamp := gen.Generate("rig", sampleWanted(), confidence, match, signals)

	if !strings.Contains(stamp.Message, "75%") {
		t.Errorf("message should contain confidence percentage, got: %s", stamp.Message)
	}
	if !strings.Contains(stamp.Message, "PR") {
		t.Errorf("message should mention PRs, got: %s", stamp.Message)
	}
}

// ---------------------------------------------------------------------------
// ValenceJSON tests
// ---------------------------------------------------------------------------

func TestValenceJSON(t *testing.T) {
	v := Valence{Quality: 3.5, Reliability: 4.0, Creativity: 2.5}
	jsonStr, err := ValenceJSON(v)
	if err != nil {
		t.Fatalf("ValenceJSON error: %v", err)
	}

	if !strings.Contains(jsonStr, `"quality":3.5`) {
		t.Errorf("expected quality in JSON, got: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"reliability":4`) {
		t.Errorf("expected reliability in JSON, got: %s", jsonStr)
	}
}

// ---------------------------------------------------------------------------
// Config tests
// ---------------------------------------------------------------------------

func TestValidateCompletion_RequiredEvidenceTypes(t *testing.T) {
	wanted := sampleWanted()
	// Good evidence but no PR URL.
	completion := CompletionSubmission{
		CompletionID: "c-007",
		WantedID:     wanted.ID,
		RigHandle:    "no-pr-rig",
		Evidence: `Built the stamp validation pipeline with confidence scoring.
Commit a1b2c3d4e5f6 has the implementation.
The pipeline analyzes evidence, matches requirements against wanted items,
calculates confidence scores, and generates stamp records with valence.
Covered golang, wasteland, and reputation tags.`,
	}

	config := DefaultConfig()
	config.RequiredEvidenceTypes = []string{EvidencePRURL}

	result := ValidateCompletion(wanted, completion, config)

	// Should be downgraded from approved to needs_review because no PR URL.
	if result.Outcome == OutcomeApproved {
		t.Errorf("should not be approved without required PR URL; got %s", result.Outcome)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func containsType(types []string, target string) bool {
	for _, t := range types {
		if t == target {
			return true
		}
	}
	return false
}
