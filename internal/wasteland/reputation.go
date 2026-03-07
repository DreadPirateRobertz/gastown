package wasteland

import (
	"encoding/json"
	"fmt"
	"math"
)

// Valence holds the multi-dimensional reputation signals from a stamp.
type Valence struct {
	Quality     float64 `json:"quality"`
	Reliability float64 `json:"reliability"`
	Creativity  float64 `json:"creativity"`
}

// Stamp represents a stamp record for reputation scoring.
type Stamp struct {
	ID         string
	Author     string
	Subject    string
	Valence    Valence
	Confidence float64
	Severity   string // leaf, branch, root
	SkillTags  []string
}

// ParseValence parses a JSON valence string into a Valence struct.
func ParseValence(raw string) (Valence, error) {
	var v Valence
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return Valence{}, fmt.Errorf("parsing valence JSON: %w", err)
	}
	return v, nil
}

// severityWeight returns the multiplier for a stamp severity level.
func severityWeight(severity string) float64 {
	switch severity {
	case "root":
		return 3.0
	case "branch":
		return 2.0
	case "leaf", "":
		return 1.0
	default:
		return 1.0
	}
}

// DimensionScore holds the computed score for a single reputation dimension.
type DimensionScore struct {
	Score      float64 // Weighted average (0-5 scale)
	TotalWeight float64 // Sum of weights contributing to this score
	Count      int     // Number of stamps contributing
}

// ReputationScore is the computed reputation for a rig.
type ReputationScore struct {
	Handle      string
	Quality     DimensionScore
	Reliability DimensionScore
	Creativity  DimensionScore
	Composite   float64 // Overall score (0-5 scale)
	StampCount  int
	SkillMap    map[string]int // skill tag -> count of stamps with that tag
}

// ComputeReputation calculates a rig's reputation score from their stamps.
func ComputeReputation(handle string, stamps []Stamp) *ReputationScore {
	rep := &ReputationScore{
		Handle:   handle,
		SkillMap: make(map[string]int),
	}

	if len(stamps) == 0 {
		return rep
	}

	var qSum, rSum, cSum float64
	var qWeight, rWeight, cWeight float64

	for _, s := range stamps {
		w := severityWeight(s.Severity) * clampConfidence(s.Confidence)

		qSum += s.Valence.Quality * w
		qWeight += w

		rSum += s.Valence.Reliability * w
		rWeight += w

		cSum += s.Valence.Creativity * w
		cWeight += w

		for _, tag := range s.SkillTags {
			rep.SkillMap[tag]++
		}
	}

	rep.StampCount = len(stamps)

	rep.Quality = DimensionScore{
		Score:       safeDiv(qSum, qWeight),
		TotalWeight: qWeight,
		Count:       len(stamps),
	}
	rep.Reliability = DimensionScore{
		Score:       safeDiv(rSum, rWeight),
		TotalWeight: rWeight,
		Count:       len(stamps),
	}
	rep.Creativity = DimensionScore{
		Score:       safeDiv(cSum, cWeight),
		TotalWeight: cWeight,
		Count:       len(stamps),
	}

	// Composite: equal-weight average of the three dimensions
	rep.Composite = (rep.Quality.Score + rep.Reliability.Score + rep.Creativity.Score) / 3.0
	// Round to 2 decimal places
	rep.Composite = math.Round(rep.Composite*100) / 100

	return rep
}

func clampConfidence(c float64) float64 {
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

func safeDiv(num, denom float64) float64 {
	if denom == 0 {
		return 0
	}
	return math.Round(num/denom*100) / 100
}
