package wasteland

import (
	"math"
	"testing"
)

func TestParseValence(t *testing.T) {
	v, err := ParseValence(`{"quality": 4, "reliability": 5, "creativity": 3}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Quality != 4 || v.Reliability != 5 || v.Creativity != 3 {
		t.Errorf("got %+v, want {4, 5, 3}", v)
	}
}

func TestParseValenceInvalid(t *testing.T) {
	_, err := ParseValence(`not json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestComputeReputationEmpty(t *testing.T) {
	rep := ComputeReputation("alice", nil)
	if rep.StampCount != 0 {
		t.Errorf("expected 0 stamps, got %d", rep.StampCount)
	}
	if rep.Composite != 0 {
		t.Errorf("expected 0 composite, got %f", rep.Composite)
	}
}

func TestComputeReputationSingleStamp(t *testing.T) {
	stamps := []Stamp{{
		Author:     "bob",
		Subject:    "alice",
		Valence:    Valence{Quality: 4, Reliability: 5, Creativity: 3},
		Confidence: 1.0,
		Severity:   "leaf",
		SkillTags:  []string{"go", "testing"},
	}}

	rep := ComputeReputation("alice", stamps)

	if rep.StampCount != 1 {
		t.Errorf("expected 1 stamp, got %d", rep.StampCount)
	}
	if rep.Quality.Score != 4.0 {
		t.Errorf("quality: got %f, want 4.0", rep.Quality.Score)
	}
	if rep.Reliability.Score != 5.0 {
		t.Errorf("reliability: got %f, want 5.0", rep.Reliability.Score)
	}
	if rep.Creativity.Score != 3.0 {
		t.Errorf("creativity: got %f, want 3.0", rep.Creativity.Score)
	}
	if rep.Composite != 4.0 {
		t.Errorf("composite: got %f, want 4.0", rep.Composite)
	}
	if rep.SkillMap["go"] != 1 || rep.SkillMap["testing"] != 1 {
		t.Errorf("skill map: got %v", rep.SkillMap)
	}
}

func TestComputeReputationSeverityWeighting(t *testing.T) {
	stamps := []Stamp{
		{
			Valence:    Valence{Quality: 5, Reliability: 5, Creativity: 5},
			Confidence: 1.0,
			Severity:   "root", // weight 3
		},
		{
			Valence:    Valence{Quality: 2, Reliability: 2, Creativity: 2},
			Confidence: 1.0,
			Severity:   "leaf", // weight 1
		},
	}

	rep := ComputeReputation("alice", stamps)

	// Expected quality: (5*3 + 2*1) / (3+1) = 17/4 = 4.25
	if rep.Quality.Score != 4.25 {
		t.Errorf("quality: got %f, want 4.25", rep.Quality.Score)
	}
}

func TestComputeReputationConfidenceWeighting(t *testing.T) {
	stamps := []Stamp{
		{
			Valence:    Valence{Quality: 5, Reliability: 5, Creativity: 5},
			Confidence: 1.0,
			Severity:   "leaf",
		},
		{
			Valence:    Valence{Quality: 1, Reliability: 1, Creativity: 1},
			Confidence: 0.5,
			Severity:   "leaf",
		},
	}

	rep := ComputeReputation("alice", stamps)

	// Expected quality: (5*1.0 + 1*0.5) / (1.0 + 0.5) = 5.5/1.5 = 3.67
	if rep.Quality.Score != 3.67 {
		t.Errorf("quality: got %f, want 3.67", rep.Quality.Score)
	}
}

func TestComputeReputationZeroConfidence(t *testing.T) {
	stamps := []Stamp{{
		Valence:    Valence{Quality: 5, Reliability: 5, Creativity: 5},
		Confidence: 0,
		Severity:   "leaf",
	}}

	rep := ComputeReputation("alice", stamps)

	if rep.Quality.Score != 0 {
		t.Errorf("quality: got %f, want 0 (zero confidence)", rep.Quality.Score)
	}
}

func TestClampConfidence(t *testing.T) {
	if clampConfidence(-0.5) != 0 {
		t.Error("negative should clamp to 0")
	}
	if clampConfidence(1.5) != 1 {
		t.Error(">1 should clamp to 1")
	}
	if clampConfidence(0.7) != 0.7 {
		t.Error("0.7 should pass through")
	}
}

func TestSeverityWeight(t *testing.T) {
	cases := map[string]float64{
		"root":    3.0,
		"branch":  2.0,
		"leaf":    1.0,
		"":        1.0,
		"unknown": 1.0,
	}
	for sev, want := range cases {
		if got := severityWeight(sev); got != want {
			t.Errorf("severityWeight(%q) = %f, want %f", sev, got, want)
		}
	}
}

func approxEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) < tolerance
}

func TestComputeReputationMultipleStamps(t *testing.T) {
	stamps := []Stamp{
		{Valence: Valence{Quality: 4, Reliability: 5, Creativity: 3}, Confidence: 1.0, Severity: "leaf"},
		{Valence: Valence{Quality: 3, Reliability: 4, Creativity: 4}, Confidence: 0.8, Severity: "branch"},
		{Valence: Valence{Quality: 5, Reliability: 5, Creativity: 5}, Confidence: 1.0, Severity: "root", SkillTags: []string{"go"}},
	}

	rep := ComputeReputation("alice", stamps)

	if rep.StampCount != 3 {
		t.Errorf("expected 3 stamps, got %d", rep.StampCount)
	}
	// Total weight: 1*1 + 0.8*2 + 1*3 = 1 + 1.6 + 3 = 5.6
	// Quality: (4*1 + 3*1.6 + 5*3) / 5.6 = (4 + 4.8 + 15) / 5.6 = 23.8/5.6 = 4.25
	if !approxEqual(rep.Quality.Score, 4.25, 0.01) {
		t.Errorf("quality: got %f, want ~4.25", rep.Quality.Score)
	}
	if rep.SkillMap["go"] != 1 {
		t.Errorf("expected go skill tag, got %v", rep.SkillMap)
	}
}
