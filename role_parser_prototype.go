// Package main provides a prototype role parser for Gas Town.
//
// This parser reads declarative YAML role definitions and produces runtime
// agent configurations. It extends the existing TOML-based role system
// (internal/config/roles.go) with richer semantics: tool access control,
// context sources, communication channels, memory configuration, and
// single-inheritance role composition.
//
// This is a standalone prototype; it does not import from the gastown module
// to keep iteration fast. The structs here are designed to be compatible with
// a future migration path into internal/config.
package main

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// Core types
// ---------------------------------------------------------------------------

// RoleDefinition is the top-level parsed representation of a YAML role file.
// Every field except Name is optional; validation enforces required fields.
type RoleDefinition struct {
	// Name is the unique identifier for this role (e.g., "mayor", "crew").
	Name string `yaml:"name"`

	// Description is a human-readable summary of the role's purpose.
	Description string `yaml:"description,omitempty"`

	// Extends names a parent role whose fields are inherited.
	// Only single inheritance is supported. Empty means no parent.
	Extends string `yaml:"extends,omitempty"`

	// Tools lists the external tools this role may invoke.
	Tools []ToolAccess `yaml:"tools,omitempty"`

	// Context lists data sources available to the role at runtime.
	Context []ContextSource `yaml:"context,omitempty"`

	// Constraints governs resource limits and sandboxing.
	Constraints Constraints `yaml:"constraints,omitempty"`

	// Communication lists the messaging channels the role participates in.
	Communication []Channel `yaml:"communication,omitempty"`

	// Memory configures persistent state for the role.
	Memory MemoryConfig `yaml:"memory,omitempty"`
}

// ToolAccess describes a single tool binding with permission controls.
type ToolAccess struct {
	// Name is the tool identifier (e.g., "bash", "gh", "bd").
	Name string `yaml:"name"`

	// Permissions is a list of granted capabilities (e.g., "read", "write", "execute").
	Permissions []string `yaml:"permissions,omitempty"`

	// Config holds tool-specific key/value settings.
	Config map[string]string `yaml:"config,omitempty"`
}

// ContextSource describes a data source the role can read at runtime.
type ContextSource struct {
	// Type is the source kind: "file", "database", or "api".
	Type string `yaml:"type"`

	// Path is the location of the source. Interpretation depends on Type:
	//   file:     filesystem path (may contain {town}/{rig} placeholders)
	//   database: DSN or database name
	//   api:      URL or service identifier
	Path string `yaml:"path"`

	// ReadOnly indicates the role should not mutate this source.
	ReadOnly bool `yaml:"readonly,omitempty"`
}

// Constraints governs resource limits and security boundaries.
type Constraints struct {
	// MaxTokens caps the context window budget for a single invocation.
	MaxTokens int `yaml:"max_tokens,omitempty"`

	// MaxTime caps wall-clock execution time per task.
	MaxTime time.Duration `yaml:"max_time,omitempty"`

	// Sandbox, when true, restricts filesystem and network access.
	Sandbox bool `yaml:"sandbox,omitempty"`

	// AllowedCommands whitelists shell commands when Sandbox is true.
	// An empty list with Sandbox=true means no commands are allowed.
	AllowedCommands []string `yaml:"allowed_commands,omitempty"`
}

// Channel describes a messaging channel the role participates in.
type Channel struct {
	// Name is the channel identifier (e.g., "crew-sync", "escalation").
	Name string `yaml:"name"`

	// Type is the messaging pattern: "pubsub", "direct", or "broadcast".
	Type string `yaml:"type"`

	// Topics lists the event topics this channel carries.
	Topics []string `yaml:"topics,omitempty"`
}

// MemoryConfig describes how the role persists state across sessions.
type MemoryConfig struct {
	// Persistent indicates whether memory survives session restarts.
	Persistent bool `yaml:"persistent,omitempty"`

	// SharedWith lists role names that can read this role's memory.
	SharedWith []string `yaml:"shared_with,omitempty"`

	// Path is the filesystem path for memory storage.
	// Supports {town}/{rig}/{name} placeholders.
	Path string `yaml:"path,omitempty"`
}

// AgentConfig is the runtime configuration consumed by the Gas Town daemon
// and session launcher. It flattens the parsed role definition into the
// shape the existing system expects.
type AgentConfig struct {
	// RoleName is the resolved role identifier.
	RoleName string

	// Description is carried forward for logging and diagnostics.
	Description string

	// Env holds environment variables to inject into the agent session.
	Env map[string]string

	// AllowedTools lists tool names the agent may invoke.
	AllowedTools []string

	// ToolConfigs holds per-tool configuration maps.
	ToolConfigs map[string]map[string]string

	// ContextPaths lists resolved context source paths.
	ContextPaths []string

	// MaxTokens is the token budget (0 = unlimited).
	MaxTokens int

	// MaxTime is the wall-clock limit (0 = unlimited).
	MaxTime time.Duration

	// Sandbox indicates whether the agent runs in a restricted environment.
	Sandbox bool

	// AllowedCommands lists commands permitted under sandbox mode.
	AllowedCommands []string

	// Channels lists communication channel names.
	Channels []string

	// MemoryPath is the resolved memory storage path.
	MemoryPath string

	// MemorySharedWith lists roles that share this memory.
	MemorySharedWith []string
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

// ParseRole deserializes a YAML document into a RoleDefinition.
// It returns a validation error if the YAML is syntactically valid but
// semantically incomplete (e.g., missing name).
func ParseRole(yamlBytes []byte) (*RoleDefinition, error) {
	var role RoleDefinition
	if err := yaml.Unmarshal(yamlBytes, &role); err != nil {
		return nil, fmt.Errorf("yaml parse error: %w", err)
	}
	return &role, nil
}

// ---------------------------------------------------------------------------
// Inheritance
// ---------------------------------------------------------------------------

// ParentRegistry maps role names to their definitions. Used by
// ResolveInheritance to look up parent roles.
type ParentRegistry map[string]*RoleDefinition

// ResolveInheritance merges the parent role (if any) into the child.
// Parent fields provide defaults; child fields override. Only one level
// of inheritance is resolved per call — callers must resolve the parent
// first for multi-level chains.
//
// The function returns a new RoleDefinition; neither parent nor child is
// mutated.
func ResolveInheritance(child *RoleDefinition, registry ParentRegistry) (*RoleDefinition, error) {
	if child.Extends == "" {
		// No parent — return a copy.
		merged := *child
		return &merged, nil
	}

	parent, ok := registry[child.Extends]
	if !ok {
		return nil, fmt.Errorf("parent role %q not found in registry", child.Extends)
	}

	// Detect trivial cycles (A extends A).
	if parent.Extends == child.Name {
		return nil, fmt.Errorf("circular inheritance: %s <-> %s", child.Name, parent.Extends)
	}

	merged := &RoleDefinition{}

	// Name and Description: child always wins.
	merged.Name = child.Name
	merged.Description = child.Description
	if merged.Description == "" {
		merged.Description = parent.Description
	}

	// Extends is cleared after resolution.
	merged.Extends = ""

	// Tools: child tools override parent tools with the same name;
	// parent-only tools are inherited.
	merged.Tools = mergeTools(parent.Tools, child.Tools)

	// Context: union of both, child entries override by path.
	merged.Context = mergeContextSources(parent.Context, child.Context)

	// Constraints: child values override non-zero parent values.
	merged.Constraints = mergeConstraints(parent.Constraints, child.Constraints)

	// Communication: union, child overrides by channel name.
	merged.Communication = mergeChannels(parent.Communication, child.Communication)

	// Memory: child overrides entirely if any field is set.
	merged.Memory = mergeMemory(parent.Memory, child.Memory)

	return merged, nil
}

// mergeTools produces a combined tool list. Child entries with the same
// Name as a parent entry replace the parent version.
func mergeTools(parent, child []ToolAccess) []ToolAccess {
	index := make(map[string]ToolAccess, len(parent)+len(child))
	order := make([]string, 0, len(parent)+len(child))

	for _, t := range parent {
		if _, exists := index[t.Name]; !exists {
			order = append(order, t.Name)
		}
		index[t.Name] = t
	}
	for _, t := range child {
		if _, exists := index[t.Name]; !exists {
			order = append(order, t.Name)
		}
		index[t.Name] = t
	}

	result := make([]ToolAccess, 0, len(order))
	for _, name := range order {
		result = append(result, index[name])
	}
	return result
}

// mergeContextSources unions parent and child context sources. Child
// entries with the same Path replace the parent entry.
func mergeContextSources(parent, child []ContextSource) []ContextSource {
	index := make(map[string]ContextSource, len(parent)+len(child))
	order := make([]string, 0, len(parent)+len(child))

	for _, c := range parent {
		if _, exists := index[c.Path]; !exists {
			order = append(order, c.Path)
		}
		index[c.Path] = c
	}
	for _, c := range child {
		if _, exists := index[c.Path]; !exists {
			order = append(order, c.Path)
		}
		index[c.Path] = c
	}

	result := make([]ContextSource, 0, len(order))
	for _, path := range order {
		result = append(result, index[path])
	}
	return result
}

// mergeConstraints produces a constraint set where child non-zero values
// override parent values.
func mergeConstraints(parent, child Constraints) Constraints {
	result := parent
	if child.MaxTokens != 0 {
		result.MaxTokens = child.MaxTokens
	}
	if child.MaxTime != 0 {
		result.MaxTime = child.MaxTime
	}
	if child.Sandbox {
		result.Sandbox = true
	}
	if len(child.AllowedCommands) > 0 {
		result.AllowedCommands = child.AllowedCommands
	}
	return result
}

// mergeChannels unions parent and child channels. Child entries with the
// same Name replace the parent entry.
func mergeChannels(parent, child []Channel) []Channel {
	index := make(map[string]Channel, len(parent)+len(child))
	order := make([]string, 0, len(parent)+len(child))

	for _, ch := range parent {
		if _, exists := index[ch.Name]; !exists {
			order = append(order, ch.Name)
		}
		index[ch.Name] = ch
	}
	for _, ch := range child {
		if _, exists := index[ch.Name]; !exists {
			order = append(order, ch.Name)
		}
		index[ch.Name] = ch
	}

	result := make([]Channel, 0, len(order))
	for _, name := range order {
		result = append(result, index[name])
	}
	return result
}

// mergeMemory returns the child memory config if any field is set,
// otherwise falls back to the parent config.
func mergeMemory(parent, child MemoryConfig) MemoryConfig {
	if child.Path != "" || child.Persistent || len(child.SharedWith) > 0 {
		return child
	}
	return parent
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

// ValidationError collects multiple validation failures.
type ValidationError struct {
	Errors []string
}

// Error implements the error interface.
func (v *ValidationError) Error() string {
	return fmt.Sprintf("role validation failed: %s", strings.Join(v.Errors, "; "))
}

// ValidateRole checks a RoleDefinition for internal consistency.
// Returns nil if the role is valid.
func ValidateRole(role *RoleDefinition) error {
	var errs []string

	// Name is required.
	if role.Name == "" {
		errs = append(errs, "name is required")
	}

	// Tool names must be non-empty and unique.
	toolNames := make(map[string]bool)
	for i, t := range role.Tools {
		if t.Name == "" {
			errs = append(errs, fmt.Sprintf("tools[%d]: name is required", i))
		} else if toolNames[t.Name] {
			errs = append(errs, fmt.Sprintf("tools[%d]: duplicate tool name %q", i, t.Name))
		}
		toolNames[t.Name] = true
	}

	// Context source types must be recognized.
	validContextTypes := map[string]bool{"file": true, "database": true, "api": true}
	for i, c := range role.Context {
		if !validContextTypes[c.Type] {
			errs = append(errs, fmt.Sprintf("context[%d]: invalid type %q (want file|database|api)", i, c.Type))
		}
		if c.Path == "" {
			errs = append(errs, fmt.Sprintf("context[%d]: path is required", i))
		}
	}

	// Channel types must be recognized.
	validChannelTypes := map[string]bool{"pubsub": true, "direct": true, "broadcast": true}
	for i, ch := range role.Communication {
		if ch.Name == "" {
			errs = append(errs, fmt.Sprintf("communication[%d]: name is required", i))
		}
		if !validChannelTypes[ch.Type] {
			errs = append(errs, fmt.Sprintf("communication[%d]: invalid type %q (want pubsub|direct|broadcast)", i, ch.Type))
		}
	}

	// Sandbox consistency: if sandbox is false but allowed_commands is set,
	// that is likely a configuration error.
	if !role.Constraints.Sandbox && len(role.Constraints.AllowedCommands) > 0 {
		errs = append(errs, "allowed_commands set but sandbox is false — commands are only restricted in sandbox mode")
	}

	// MaxTokens and MaxTime must be non-negative.
	if role.Constraints.MaxTokens < 0 {
		errs = append(errs, "max_tokens must be non-negative")
	}
	if role.Constraints.MaxTime < 0 {
		errs = append(errs, "max_time must be non-negative")
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Conversion to runtime config
// ---------------------------------------------------------------------------

// ToAgentConfig converts a fully resolved RoleDefinition into the runtime
// AgentConfig struct consumed by the Gas Town session launcher.
func ToAgentConfig(role *RoleDefinition) *AgentConfig {
	cfg := &AgentConfig{
		RoleName:    role.Name,
		Description: role.Description,
		Env: map[string]string{
			"GT_ROLE": role.Name,
		},
		AllowedTools:     make([]string, 0, len(role.Tools)),
		ToolConfigs:      make(map[string]map[string]string),
		ContextPaths:     make([]string, 0, len(role.Context)),
		MaxTokens:        role.Constraints.MaxTokens,
		MaxTime:          role.Constraints.MaxTime,
		Sandbox:          role.Constraints.Sandbox,
		AllowedCommands:  role.Constraints.AllowedCommands,
		Channels:         make([]string, 0, len(role.Communication)),
		MemoryPath:       role.Memory.Path,
		MemorySharedWith: role.Memory.SharedWith,
	}

	for _, t := range role.Tools {
		cfg.AllowedTools = append(cfg.AllowedTools, t.Name)
		if len(t.Config) > 0 {
			cfg.ToolConfigs[t.Name] = t.Config
		}
	}

	for _, c := range role.Context {
		cfg.ContextPaths = append(cfg.ContextPaths, c.Path)
	}

	for _, ch := range role.Communication {
		cfg.Channels = append(cfg.Channels, ch.Name)
	}

	return cfg
}
