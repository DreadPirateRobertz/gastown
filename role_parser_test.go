package main

import (
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Example YAML role definitions (test constants)
// ---------------------------------------------------------------------------

// mayorYAML defines a mayor role: town-scope coordinator with broad access.
const mayorYAML = `
name: mayor
description: "Global coordinator for cross-rig work. One per town."
tools:
  - name: bash
    permissions: [read, write, execute]
  - name: gh
    permissions: [read, write]
    config:
      default_repo: steveyegge/gastown
  - name: bd
    permissions: [read, write]
  - name: gt
    permissions: [read, write, execute]
context:
  - type: file
    path: "{town}/mayor"
  - type: database
    path: beads
  - type: file
    path: "{town}/CLAUDE.md"
    readonly: true
constraints:
  max_tokens: 200000
  max_time: 4h
  sandbox: false
communication:
  - name: escalation
    type: broadcast
    topics: [critical, high, medium]
  - name: mail
    type: direct
  - name: crew-sync
    type: pubsub
    topics: [status, handoff]
memory:
  persistent: true
  shared_with: [deacon]
  path: "{town}/mayor/MEMORY.md"
`

// crewWorkerYAML defines a crew worker: rig-scope persistent workspace.
const crewWorkerYAML = `
name: crew-worker
description: "Persistent crew workspace for development work."
extends: base-agent
tools:
  - name: bash
    permissions: [read, write, execute]
  - name: gh
    permissions: [read, write]
  - name: bd
    permissions: [read, write]
context:
  - type: file
    path: "{town}/{rig}/crew/{name}"
  - type: database
    path: beads
    readonly: true
constraints:
  max_tokens: 150000
  max_time: 8h
  sandbox: false
communication:
  - name: mail
    type: direct
  - name: crew-sync
    type: pubsub
    topics: [status, pr-review]
memory:
  persistent: true
  shared_with: [mayor]
  path: "{town}/{rig}/crew/{name}/MEMORY.md"
`

// polecatYAML defines a polecat: ephemeral task runner with tight constraints.
const polecatYAML = `
name: polecat
description: "Ephemeral task runner. Persistent identity, disposable sessions."
extends: base-agent
tools:
  - name: bash
    permissions: [read, execute]
    config:
      timeout: "120s"
  - name: gh
    permissions: [read]
context:
  - type: file
    path: "{town}/{rig}/polecats/{name}"
constraints:
  max_tokens: 80000
  max_time: 2h
  sandbox: true
  allowed_commands: [git, go, make, grep, find]
communication:
  - name: mail
    type: direct
memory:
  persistent: false
  path: "{town}/{rig}/polecats/{name}/scratch"
`

// baseAgentYAML is a parent role used for inheritance tests.
const baseAgentYAML = `
name: base-agent
description: "Base configuration shared by all rig-scoped agents."
tools:
  - name: gt
    permissions: [read, execute]
context:
  - type: file
    path: "{town}/CLAUDE.md"
    readonly: true
constraints:
  max_tokens: 100000
  max_time: 4h
communication:
  - name: heartbeat
    type: broadcast
    topics: [ping, pong]
memory:
  persistent: false
  path: ""
`

// minimalYAML has only the required field (name) and one tool.
const minimalYAML = `
name: minimal
tools:
  - name: bash
    permissions: [execute]
`

// ---------------------------------------------------------------------------
// Parse tests
// ---------------------------------------------------------------------------

func TestParseRole_Minimal(t *testing.T) {
	role, err := ParseRole([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("ParseRole(minimal): %v", err)
	}

	if role.Name != "minimal" {
		t.Errorf("Name = %q, want %q", role.Name, "minimal")
	}
	if len(role.Tools) != 1 {
		t.Fatalf("len(Tools) = %d, want 1", len(role.Tools))
	}
	if role.Tools[0].Name != "bash" {
		t.Errorf("Tools[0].Name = %q, want %q", role.Tools[0].Name, "bash")
	}
	if role.Description != "" {
		t.Errorf("Description = %q, want empty", role.Description)
	}
}

func TestParseRole_FullMayor(t *testing.T) {
	role, err := ParseRole([]byte(mayorYAML))
	if err != nil {
		t.Fatalf("ParseRole(mayor): %v", err)
	}

	if role.Name != "mayor" {
		t.Errorf("Name = %q, want %q", role.Name, "mayor")
	}
	if role.Description == "" {
		t.Error("Description should not be empty")
	}

	// Tools
	if len(role.Tools) != 4 {
		t.Fatalf("len(Tools) = %d, want 4", len(role.Tools))
	}
	// gh tool should have config
	ghTool := role.Tools[1]
	if ghTool.Name != "gh" {
		t.Errorf("Tools[1].Name = %q, want %q", ghTool.Name, "gh")
	}
	if ghTool.Config["default_repo"] != "steveyegge/gastown" {
		t.Errorf("gh config default_repo = %q, want %q", ghTool.Config["default_repo"], "steveyegge/gastown")
	}

	// Context
	if len(role.Context) != 3 {
		t.Fatalf("len(Context) = %d, want 3", len(role.Context))
	}
	if role.Context[2].ReadOnly != true {
		t.Error("CLAUDE.md context should be readonly")
	}

	// Constraints
	if role.Constraints.MaxTokens != 200000 {
		t.Errorf("MaxTokens = %d, want 200000", role.Constraints.MaxTokens)
	}
	if role.Constraints.MaxTime != 4*time.Hour {
		t.Errorf("MaxTime = %v, want 4h", role.Constraints.MaxTime)
	}
	if role.Constraints.Sandbox {
		t.Error("Mayor should not be sandboxed")
	}

	// Communication
	if len(role.Communication) != 3 {
		t.Fatalf("len(Communication) = %d, want 3", len(role.Communication))
	}
	if role.Communication[0].Type != "broadcast" {
		t.Errorf("escalation channel type = %q, want broadcast", role.Communication[0].Type)
	}

	// Memory
	if !role.Memory.Persistent {
		t.Error("Mayor memory should be persistent")
	}
	if len(role.Memory.SharedWith) != 1 || role.Memory.SharedWith[0] != "deacon" {
		t.Errorf("Memory.SharedWith = %v, want [deacon]", role.Memory.SharedWith)
	}
}

func TestParseRole_Polecat(t *testing.T) {
	role, err := ParseRole([]byte(polecatYAML))
	if err != nil {
		t.Fatalf("ParseRole(polecat): %v", err)
	}

	if !role.Constraints.Sandbox {
		t.Error("Polecat should be sandboxed")
	}
	if len(role.Constraints.AllowedCommands) != 5 {
		t.Errorf("len(AllowedCommands) = %d, want 5", len(role.Constraints.AllowedCommands))
	}
	if role.Extends != "base-agent" {
		t.Errorf("Extends = %q, want %q", role.Extends, "base-agent")
	}
}

func TestParseRole_InvalidYAML(t *testing.T) {
	_, err := ParseRole([]byte("not: [valid: yaml: {{"))
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

// ---------------------------------------------------------------------------
// Inheritance tests
// ---------------------------------------------------------------------------

func TestResolveInheritance_NoParent(t *testing.T) {
	role, err := ParseRole([]byte(minimalYAML))
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}

	resolved, err := ResolveInheritance(role, ParentRegistry{})
	if err != nil {
		t.Fatalf("ResolveInheritance: %v", err)
	}
	if resolved.Name != "minimal" {
		t.Errorf("Name = %q, want %q", resolved.Name, "minimal")
	}
}

func TestResolveInheritance_CrewExtendsBase(t *testing.T) {
	base, err := ParseRole([]byte(baseAgentYAML))
	if err != nil {
		t.Fatalf("ParseRole(base): %v", err)
	}
	crew, err := ParseRole([]byte(crewWorkerYAML))
	if err != nil {
		t.Fatalf("ParseRole(crew): %v", err)
	}

	registry := ParentRegistry{"base-agent": base}
	resolved, err := ResolveInheritance(crew, registry)
	if err != nil {
		t.Fatalf("ResolveInheritance: %v", err)
	}

	// Name should be the child's.
	if resolved.Name != "crew-worker" {
		t.Errorf("Name = %q, want %q", resolved.Name, "crew-worker")
	}

	// Extends should be cleared after resolution.
	if resolved.Extends != "" {
		t.Errorf("Extends = %q, want empty (resolved)", resolved.Extends)
	}

	// gt tool from parent should be present.
	hasGT := false
	for _, tool := range resolved.Tools {
		if tool.Name == "gt" {
			hasGT = true
			break
		}
	}
	if !hasGT {
		t.Error("resolved role should inherit 'gt' tool from base-agent")
	}

	// Child's bash tool should be present.
	hasBash := false
	for _, tool := range resolved.Tools {
		if tool.Name == "bash" {
			hasBash = true
			break
		}
	}
	if !hasBash {
		t.Error("resolved role should have 'bash' tool from crew-worker")
	}

	// Constraints: child overrides max_tokens (150000 > base's 100000).
	if resolved.Constraints.MaxTokens != 150000 {
		t.Errorf("MaxTokens = %d, want 150000 (child override)", resolved.Constraints.MaxTokens)
	}

	// MaxTime: child sets 8h, overriding base's 4h.
	if resolved.Constraints.MaxTime != 8*time.Hour {
		t.Errorf("MaxTime = %v, want 8h (child override)", resolved.Constraints.MaxTime)
	}

	// Heartbeat channel from parent should be inherited.
	hasHeartbeat := false
	for _, ch := range resolved.Communication {
		if ch.Name == "heartbeat" {
			hasHeartbeat = true
			break
		}
	}
	if !hasHeartbeat {
		t.Error("resolved role should inherit 'heartbeat' channel from base-agent")
	}

	// CLAUDE.md context from parent should be inherited.
	hasClaudeMD := false
	for _, ctx := range resolved.Context {
		if ctx.Path == "{town}/CLAUDE.md" {
			hasClaudeMD = true
			if !ctx.ReadOnly {
				t.Error("inherited CLAUDE.md context should be readonly")
			}
			break
		}
	}
	if !hasClaudeMD {
		t.Error("resolved role should inherit CLAUDE.md context from base-agent")
	}

	// Memory: child sets persistent=true, should override parent's false.
	if !resolved.Memory.Persistent {
		t.Error("resolved memory should be persistent (child override)")
	}
}

func TestResolveInheritance_PolecatOverridesSandbox(t *testing.T) {
	base, err := ParseRole([]byte(baseAgentYAML))
	if err != nil {
		t.Fatalf("ParseRole(base): %v", err)
	}
	polecat, err := ParseRole([]byte(polecatYAML))
	if err != nil {
		t.Fatalf("ParseRole(polecat): %v", err)
	}

	registry := ParentRegistry{"base-agent": base}
	resolved, err := ResolveInheritance(polecat, registry)
	if err != nil {
		t.Fatalf("ResolveInheritance: %v", err)
	}

	// Polecat enables sandbox; base does not.
	if !resolved.Constraints.Sandbox {
		t.Error("resolved polecat should have sandbox=true")
	}

	// Polecat's allowed_commands should override (not merge with) parent.
	if len(resolved.Constraints.AllowedCommands) != 5 {
		t.Errorf("len(AllowedCommands) = %d, want 5", len(resolved.Constraints.AllowedCommands))
	}

	// MaxTokens: polecat sets 80000, overriding base's 100000.
	if resolved.Constraints.MaxTokens != 80000 {
		t.Errorf("MaxTokens = %d, want 80000", resolved.Constraints.MaxTokens)
	}
}

func TestResolveInheritance_MissingParent(t *testing.T) {
	crew, err := ParseRole([]byte(crewWorkerYAML))
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}

	_, err = ResolveInheritance(crew, ParentRegistry{})
	if err == nil {
		t.Fatal("expected error for missing parent, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to mention 'not found'", err.Error())
	}
}

func TestResolveInheritance_CircularDetection(t *testing.T) {
	roleA := &RoleDefinition{Name: "alpha", Extends: "beta"}
	roleB := &RoleDefinition{Name: "beta", Extends: "alpha"}

	registry := ParentRegistry{"beta": roleB}
	_, err := ResolveInheritance(roleA, registry)
	if err == nil {
		t.Fatal("expected error for circular inheritance, got nil")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %q, want it to mention 'circular'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Validation tests
// ---------------------------------------------------------------------------

func TestValidateRole_Valid(t *testing.T) {
	role, err := ParseRole([]byte(mayorYAML))
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}
	if err := ValidateRole(role); err != nil {
		t.Errorf("ValidateRole(mayor): unexpected error: %v", err)
	}
}

func TestValidateRole_MissingName(t *testing.T) {
	role := &RoleDefinition{
		Tools: []ToolAccess{{Name: "bash"}},
	}
	err := ValidateRole(role)
	if err == nil {
		t.Fatal("expected validation error for missing name")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if !containsSubstring(ve.Errors, "name is required") {
		t.Errorf("errors = %v, want 'name is required'", ve.Errors)
	}
}

func TestValidateRole_DuplicateToolName(t *testing.T) {
	role := &RoleDefinition{
		Name: "test",
		Tools: []ToolAccess{
			{Name: "bash"},
			{Name: "bash"},
		},
	}
	err := ValidateRole(role)
	if err == nil {
		t.Fatal("expected validation error for duplicate tool name")
	}
	ve := err.(*ValidationError)
	if !containsSubstring(ve.Errors, "duplicate tool name") {
		t.Errorf("errors = %v, want 'duplicate tool name'", ve.Errors)
	}
}

func TestValidateRole_InvalidContextType(t *testing.T) {
	role := &RoleDefinition{
		Name: "test",
		Context: []ContextSource{
			{Type: "magic", Path: "/tmp/foo"},
		},
	}
	err := ValidateRole(role)
	if err == nil {
		t.Fatal("expected validation error for invalid context type")
	}
	ve := err.(*ValidationError)
	if !containsSubstring(ve.Errors, "invalid type") {
		t.Errorf("errors = %v, want 'invalid type'", ve.Errors)
	}
}

func TestValidateRole_InvalidChannelType(t *testing.T) {
	role := &RoleDefinition{
		Name: "test",
		Communication: []Channel{
			{Name: "ch1", Type: "websocket"},
		},
	}
	err := ValidateRole(role)
	if err == nil {
		t.Fatal("expected validation error for invalid channel type")
	}
	ve := err.(*ValidationError)
	if !containsSubstring(ve.Errors, "invalid type") {
		t.Errorf("errors = %v, want 'invalid type'", ve.Errors)
	}
}

func TestValidateRole_SandboxConflict(t *testing.T) {
	role := &RoleDefinition{
		Name: "test",
		Constraints: Constraints{
			Sandbox:         false,
			AllowedCommands: []string{"git"},
		},
	}
	err := ValidateRole(role)
	if err == nil {
		t.Fatal("expected validation error for sandbox conflict")
	}
	ve := err.(*ValidationError)
	if !containsSubstring(ve.Errors, "sandbox") {
		t.Errorf("errors = %v, want mention of 'sandbox'", ve.Errors)
	}
}

func TestValidateRole_MultipleErrors(t *testing.T) {
	role := &RoleDefinition{
		// Missing name.
		Tools: []ToolAccess{
			{Name: ""},  // Empty tool name.
			{Name: "x"}, // OK.
			{Name: "x"}, // Duplicate.
		},
		Context: []ContextSource{
			{Type: "file", Path: ""}, // Empty path.
		},
	}
	err := ValidateRole(role)
	if err == nil {
		t.Fatal("expected validation errors")
	}
	ve := err.(*ValidationError)
	// Should have at least 3 errors: missing name, empty tool name, empty path.
	if len(ve.Errors) < 3 {
		t.Errorf("len(Errors) = %d, want >= 3; errors: %v", len(ve.Errors), ve.Errors)
	}
}

// ---------------------------------------------------------------------------
// ToAgentConfig tests
// ---------------------------------------------------------------------------

func TestToAgentConfig_Mayor(t *testing.T) {
	role, err := ParseRole([]byte(mayorYAML))
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}

	cfg := ToAgentConfig(role)

	if cfg.RoleName != "mayor" {
		t.Errorf("RoleName = %q, want %q", cfg.RoleName, "mayor")
	}
	if cfg.Env["GT_ROLE"] != "mayor" {
		t.Errorf("Env[GT_ROLE] = %q, want %q", cfg.Env["GT_ROLE"], "mayor")
	}
	if len(cfg.AllowedTools) != 4 {
		t.Errorf("len(AllowedTools) = %d, want 4", len(cfg.AllowedTools))
	}
	if cfg.ToolConfigs["gh"]["default_repo"] != "steveyegge/gastown" {
		t.Error("gh tool config should carry default_repo")
	}
	if len(cfg.ContextPaths) != 3 {
		t.Errorf("len(ContextPaths) = %d, want 3", len(cfg.ContextPaths))
	}
	if len(cfg.Channels) != 3 {
		t.Errorf("len(Channels) = %d, want 3", len(cfg.Channels))
	}
	if cfg.MaxTokens != 200000 {
		t.Errorf("MaxTokens = %d, want 200000", cfg.MaxTokens)
	}
	if cfg.Sandbox {
		t.Error("Mayor should not be sandboxed")
	}
	if cfg.MemoryPath != "{town}/mayor/MEMORY.md" {
		t.Errorf("MemoryPath = %q, want {town}/mayor/MEMORY.md", cfg.MemoryPath)
	}
}

func TestToAgentConfig_SandboxedPolecat(t *testing.T) {
	role, err := ParseRole([]byte(polecatYAML))
	if err != nil {
		t.Fatalf("ParseRole: %v", err)
	}

	cfg := ToAgentConfig(role)

	if !cfg.Sandbox {
		t.Error("Polecat config should be sandboxed")
	}
	if len(cfg.AllowedCommands) != 5 {
		t.Errorf("len(AllowedCommands) = %d, want 5", len(cfg.AllowedCommands))
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// containsSubstring checks if any string in the slice contains substr.
func containsSubstring(ss []string, substr string) bool {
	for _, s := range ss {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
