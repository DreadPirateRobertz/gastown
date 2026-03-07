package formula

import (
	"testing"
)

func TestParse_Workflow(t *testing.T) {
	data := []byte(`
description = "Test workflow"
formula = "test-workflow"
type = "workflow"
version = 1

[[steps]]
id = "step1"
title = "First Step"
description = "Do the first thing"

[[steps]]
id = "step2"
title = "Second Step"
description = "Do the second thing"
needs = ["step1"]

[[steps]]
id = "step3"
title = "Third Step"
description = "Do the third thing"
needs = ["step2"]

[vars]
[vars.feature]
description = "The feature to implement"
required = true
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Name != "test-workflow" {
		t.Errorf("Name = %q, want %q", f.Name, "test-workflow")
	}
	if f.Type != TypeWorkflow {
		t.Errorf("Type = %q, want %q", f.Type, TypeWorkflow)
	}
	if len(f.Steps) != 3 {
		t.Errorf("len(Steps) = %d, want 3", len(f.Steps))
	}
	if f.Steps[1].Needs[0] != "step1" {
		t.Errorf("step2.Needs[0] = %q, want %q", f.Steps[1].Needs[0], "step1")
	}
}

func TestParse_Convoy(t *testing.T) {
	data := []byte(`
description = "Test convoy"
formula = "test-convoy"
type = "convoy"
version = 1

[[legs]]
id = "leg1"
title = "Leg One"
focus = "Focus area 1"
description = "First leg"

[[legs]]
id = "leg2"
title = "Leg Two"
focus = "Focus area 2"
description = "Second leg"

[synthesis]
title = "Synthesis"
description = "Combine results"
depends_on = ["leg1", "leg2"]
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Name != "test-convoy" {
		t.Errorf("Name = %q, want %q", f.Name, "test-convoy")
	}
	if f.Type != TypeConvoy {
		t.Errorf("Type = %q, want %q", f.Type, TypeConvoy)
	}
	if len(f.Legs) != 2 {
		t.Errorf("len(Legs) = %d, want 2", len(f.Legs))
	}
	if f.Synthesis == nil {
		t.Fatal("Synthesis is nil")
	}
	if len(f.Synthesis.DependsOn) != 2 {
		t.Errorf("len(Synthesis.DependsOn) = %d, want 2", len(f.Synthesis.DependsOn))
	}
}

func TestParse_Expansion(t *testing.T) {
	data := []byte(`
description = "Test expansion"
formula = "test-expansion"
type = "expansion"
version = 1

[[template]]
id = "{target}.draft"
title = "Draft: {target.title}"
description = "Initial draft"

[[template]]
id = "{target}.refine"
title = "Refine"
description = "Refine the draft"
needs = ["{target}.draft"]
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Name != "test-expansion" {
		t.Errorf("Name = %q, want %q", f.Name, "test-expansion")
	}
	if f.Type != TypeExpansion {
		t.Errorf("Type = %q, want %q", f.Type, TypeExpansion)
	}
	if len(f.Template) != 2 {
		t.Errorf("len(Template) = %d, want 2", len(f.Template))
	}
}

func TestParse_WorkflowWithAcceptance(t *testing.T) {
	data := []byte(`
description = "Test workflow with acceptance"
formula = "test-acceptance"
type = "workflow"
version = 1

[[steps]]
id = "design"
title = "Design"
description = "Plan it"
acceptance = "Design doc committed"

[[steps]]
id = "implement"
title = "Implement"
description = "Build it"
needs = ["design"]
acceptance = "All code written and committed"

[[steps]]
id = "test"
title = "Test"
description = "Test it"
needs = ["implement"]
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(f.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want 3", len(f.Steps))
	}

	// Steps with acceptance criteria
	if f.Steps[0].Acceptance != "Design doc committed" {
		t.Errorf("step[0].Acceptance = %q, want %q", f.Steps[0].Acceptance, "Design doc committed")
	}
	if f.Steps[1].Acceptance != "All code written and committed" {
		t.Errorf("step[1].Acceptance = %q, want %q", f.Steps[1].Acceptance, "All code written and committed")
	}
	// Step without acceptance criteria
	if f.Steps[2].Acceptance != "" {
		t.Errorf("step[2].Acceptance = %q, want empty", f.Steps[2].Acceptance)
	}
}

func TestParse_PourFlag(t *testing.T) {
	// pour = true: steps should be materialized as sub-wisps
	data := []byte(`
description = "Test pour workflow"
formula = "test-pour"
type = "workflow"
version = 1
pour = true

[[steps]]
id = "step1"
title = "First Step"
description = "Do the first thing"
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if !f.Pour {
		t.Error("Pour = false, want true")
	}
}

func TestParse_PourFlagDefault(t *testing.T) {
	// Default: pour is false (inline/root-only)
	data := []byte(`
description = "Test inline workflow"
formula = "test-inline"
type = "workflow"
version = 1

[[steps]]
id = "step1"
title = "First Step"
description = "Do the first thing"
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Pour {
		t.Error("Pour = true, want false (default)")
	}
}

func TestValidate_MissingName(t *testing.T) {
	data := []byte(`
type = "workflow"
version = 1
[[steps]]
id = "step1"
title = "Step"
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for missing formula name")
	}
}

func TestValidate_InvalidType(t *testing.T) {
	data := []byte(`
formula = "test"
type = "invalid"
version = 1
[[steps]]
id = "step1"
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for invalid type")
	}
}

func TestValidate_DuplicateStepID(t *testing.T) {
	data := []byte(`
formula = "test"
type = "workflow"
version = 1
[[steps]]
id = "step1"
title = "Step 1"
[[steps]]
id = "step1"
title = "Step 1 duplicate"
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for duplicate step id")
	}
}

func TestValidate_UnknownDependency(t *testing.T) {
	data := []byte(`
formula = "test"
type = "workflow"
version = 1
[[steps]]
id = "step1"
title = "Step 1"
needs = ["nonexistent"]
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for unknown dependency")
	}
}

func TestValidate_Cycle(t *testing.T) {
	data := []byte(`
formula = "test"
type = "workflow"
version = 1
[[steps]]
id = "step1"
title = "Step 1"
needs = ["step2"]
[[steps]]
id = "step2"
title = "Step 2"
needs = ["step1"]
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for cycle")
	}
}

func TestTopologicalSort(t *testing.T) {
	data := []byte(`
formula = "test"
type = "workflow"
version = 1
[[steps]]
id = "step3"
title = "Step 3"
needs = ["step2"]
[[steps]]
id = "step1"
title = "Step 1"
[[steps]]
id = "step2"
title = "Step 2"
needs = ["step1"]
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	order, err := f.TopologicalSort()
	if err != nil {
		t.Fatalf("TopologicalSort failed: %v", err)
	}

	// step1 must come before step2, step2 must come before step3
	indexOf := func(id string) int {
		for i, x := range order {
			if x == id {
				return i
			}
		}
		return -1
	}

	if indexOf("step1") > indexOf("step2") {
		t.Error("step1 should come before step2")
	}
	if indexOf("step2") > indexOf("step3") {
		t.Error("step2 should come before step3")
	}
}

func TestReadySteps(t *testing.T) {
	data := []byte(`
formula = "test"
type = "workflow"
version = 1
[[steps]]
id = "step1"
title = "Step 1"
[[steps]]
id = "step2"
title = "Step 2"
needs = ["step1"]
[[steps]]
id = "step3"
title = "Step 3"
needs = ["step1"]
[[steps]]
id = "step4"
title = "Step 4"
needs = ["step2", "step3"]
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// Initially only step1 is ready
	ready := f.ReadySteps(map[string]bool{})
	if len(ready) != 1 || ready[0] != "step1" {
		t.Errorf("ReadySteps({}) = %v, want [step1]", ready)
	}

	// After completing step1, step2 and step3 are ready
	ready = f.ReadySteps(map[string]bool{"step1": true})
	if len(ready) != 2 {
		t.Errorf("ReadySteps({step1}) = %v, want [step2, step3]", ready)
	}

	// After completing step1, step2, step3 is still ready
	ready = f.ReadySteps(map[string]bool{"step1": true, "step2": true})
	if len(ready) != 1 || ready[0] != "step3" {
		t.Errorf("ReadySteps({step1, step2}) = %v, want [step3]", ready)
	}

	// After completing step1, step2, step3, only step4 is ready
	ready = f.ReadySteps(map[string]bool{"step1": true, "step2": true, "step3": true})
	if len(ready) != 1 || ready[0] != "step4" {
		t.Errorf("ReadySteps({step1, step2, step3}) = %v, want [step4]", ready)
	}
}

func TestConvoyReadySteps(t *testing.T) {
	data := []byte(`
formula = "test"
type = "convoy"
version = 1
[[legs]]
id = "leg1"
title = "Leg 1"
[[legs]]
id = "leg2"
title = "Leg 2"
[[legs]]
id = "leg3"
title = "Leg 3"
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	// All legs are ready initially (parallel)
	ready := f.ReadySteps(map[string]bool{})
	if len(ready) != 3 {
		t.Errorf("ReadySteps({}) = %v, want 3 legs", ready)
	}

	// After completing leg1, leg2 and leg3 still ready
	ready = f.ReadySteps(map[string]bool{"leg1": true})
	if len(ready) != 2 {
		t.Errorf("ReadySteps({leg1}) = %v, want 2 legs", ready)
	}
}

func TestParse_ConvoyWithAgent(t *testing.T) {
	t.Parallel()
	data := []byte(`
formula = "agent-test"
type = "convoy"
version = 1
agent = "gemini"

[[legs]]
id = "default-agent"
title = "Uses formula default"
description = "No per-leg agent"

[[legs]]
id = "custom-agent"
title = "Uses custom agent"
description = "Has per-leg agent"
agent = "codex"
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Agent != "gemini" {
		t.Errorf("Formula.Agent = %q, want %q", f.Agent, "gemini")
	}
	if len(f.Legs) != 2 {
		t.Fatalf("len(Legs) = %d, want 2", len(f.Legs))
	}
	if f.Legs[0].Agent != "" {
		t.Errorf("Legs[0].Agent = %q, want empty", f.Legs[0].Agent)
	}
	if f.Legs[1].Agent != "codex" {
		t.Errorf("Legs[1].Agent = %q, want %q", f.Legs[1].Agent, "codex")
	}
}

func TestParse_ConvoyWithoutAgent(t *testing.T) {
	t.Parallel()
	// Existing formulas without agent field should continue to work
	data := []byte(`
formula = "no-agent"
type = "convoy"
version = 1

[[legs]]
id = "leg1"
title = "Normal leg"
description = "No agent override"
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if f.Agent != "" {
		t.Errorf("Formula.Agent = %q, want empty", f.Agent)
	}
	if f.Legs[0].Agent != "" {
		t.Errorf("Legs[0].Agent = %q, want empty", f.Legs[0].Agent)
	}
}

func TestParse_WorkflowWithModelConstraints(t *testing.T) {
	data := []byte(`
formula = "model-aware"
type = "workflow"
version = 1

[[steps]]
id = "quick-scan"
title = "Quick scan"
description = "Fast scan with local model"
model = "auto"
max_cost = 0.001
access_type = "local"

[[steps]]
id = "deep-work"
title = "Deep work"
description = "Quality work"
needs = ["quick-scan"]
model = "auto"
min_mmlu = 85
min_swe = 50

[[steps]]
id = "exact-model"
title = "Exact model"
description = "Specific model"
needs = ["quick-scan"]
model = "claude-sonnet-4-6"
requires = ["vision"]
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(f.Steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(f.Steps))
	}

	// Check quick-scan constraints.
	s := f.GetStep("quick-scan")
	if s.Model != "auto" {
		t.Errorf("quick-scan.Model = %q, want auto", s.Model)
	}
	if s.MaxCost != 0.001 {
		t.Errorf("quick-scan.MaxCost = %f, want 0.001", s.MaxCost)
	}
	if s.AccessType != "local" {
		t.Errorf("quick-scan.AccessType = %q, want local", s.AccessType)
	}

	// Check deep-work constraints.
	s = f.GetStep("deep-work")
	if s.MinMMLU != 85 {
		t.Errorf("deep-work.MinMMLU = %f, want 85", s.MinMMLU)
	}
	if s.MinSWE != 50 {
		t.Errorf("deep-work.MinSWE = %f, want 50", s.MinSWE)
	}

	// Check exact-model constraints.
	s = f.GetStep("exact-model")
	if s.Model != "claude-sonnet-4-6" {
		t.Errorf("exact-model.Model = %q, want claude-sonnet-4-6", s.Model)
	}
	if len(s.Requires) != 1 || s.Requires[0] != "vision" {
		t.Errorf("exact-model.Requires = %v, want [vision]", s.Requires)
	}
}

func TestParse_ModelAndProviderMutuallyExclusive(t *testing.T) {
	data := []byte(`
formula = "bad-formula"
type = "workflow"
version = 1

[[steps]]
id = "bad-step"
title = "Bad step"
model = "claude-sonnet-4-6"
provider = "anthropic"
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error when model and provider are both set")
	}
}

func TestParse_InvalidAccessType(t *testing.T) {
	data := []byte(`
formula = "bad-formula"
type = "workflow"
version = 1

[[steps]]
id = "bad-step"
title = "Bad step"
access_type = "invalid"
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for invalid access_type")
	}
}

func TestParse_InvalidCapability(t *testing.T) {
	data := []byte(`
formula = "bad-formula"
type = "workflow"
version = 1

[[steps]]
id = "bad-step"
title = "Bad step"
requires = ["teleportation"]
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for unknown capability")
	}
}

func TestParse_InvalidMMLURange(t *testing.T) {
	data := []byte(`
formula = "bad-formula"
type = "workflow"
version = 1

[[steps]]
id = "bad-step"
title = "Bad step"
min_mmlu = 150
`)

	_, err := Parse(data)
	if err == nil {
		t.Error("expected error for MMLU > 100")
	}
}

func TestParse_NoConstraintsBackwardCompatible(t *testing.T) {
	data := []byte(`
formula = "simple"
type = "workflow"
version = 1

[[steps]]
id = "step1"
title = "Simple step"
description = "No constraints"
`)

	f, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	s := f.GetStep("step1")
	if s.Model != "" || s.Provider != "" || s.AccessType != "" {
		t.Error("expected empty constraint fields for backward compatibility")
	}
}
