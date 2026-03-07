# Campfire #1: Gas City Declarative Role Format

**Wasteland Item**: w-com-005
**Topic**: What should the Gas City declarative role format look like?
**Date**: 2026-03-07
**Author**: zhora (DreadPirateRobertz)

---

## 1. Current State: How Gas Town Defines Roles Today

Gas Town's role system is split across two layers: a **structural layer** (TOML config) and a **behavioral layer** (Go template markdown). Understanding both is essential context for designing what comes next.

### 1.1 Structural Layer: TOML Role Definitions

Every role is defined in a TOML file at `internal/config/roles/<role>.toml`. There are 7 role types in production: `mayor`, `deacon`, `witness`, `refinery`, `polecat`, `crew`, `dog`, plus an 8th (`headless`) for non-repo tasks.

Here is a representative example (`crew.toml`):

```toml
role = "crew"
scope = "rig"
nudge = "Check your hook and mail, then act accordingly."
prompt_template = "crew.md.tmpl"

[session]
pattern = "{prefix}-crew-{name}"
work_dir = "{town}/{rig}/crew/{name}"
needs_pre_sync = true

[env]
GT_ROLE = "crew"
GT_SCOPE = "rig"

[health]
ping_timeout = "30s"
consecutive_failures = 3
kill_cooldown = "5m"
stuck_threshold = "4h"
```

The Go struct backing this (`RoleDefinition` in `internal/config/roles.go`):

```go
type RoleDefinition struct {
    Role           string            `toml:"role"`
    Scope          string            `toml:"scope"`
    Session        RoleSessionConfig `toml:"session"`
    Env            map[string]string `toml:"env,omitempty"`
    Health         RoleHealthConfig  `toml:"health"`
    Nudge          string            `toml:"nudge,omitempty"`
    PromptTemplate string            `toml:"prompt_template,omitempty"`
}
```

**Key design properties of the current system:**

- **Layered override resolution**: Built-in defaults (embedded in binary) -> town-level overrides (`<town>/roles/<role>.toml`) -> rig-level overrides (`<rig>/roles/<role>.toml`). Each layer merges non-zero fields.
- **Scope model**: Two scopes only -- `town` (singleton agents like mayor, deacon, dog) and `rig` (per-rig agents like witness, refinery, polecat, crew).
- **Placeholder expansion**: Patterns like `{town}`, `{rig}`, `{name}`, `{prefix}`, `{role}` are expanded at runtime.
- **Health is per-role, not per-instance**: All crew agents share the same `stuck_threshold = "4h"`. No per-agent tuning without rig-level overrides.

### 1.2 Behavioral Layer: Markdown Prompt Templates

Each role has a Go-template markdown file at `internal/templates/roles/<role>.md.tmpl` that defines the agent's identity, responsibilities, communication protocols, and operational procedures. These templates are 100-400 lines of prose and receive runtime variables (`.TownRoot`, `.RigName`, `.WorkDir`, etc.).

This layer is **entirely unstructured** -- it is free-form markdown injected into the agent's system prompt. There is no schema, no validation, and no composability. The behavioral specification for a witness vs. a polecat is completely different prose written by hand.

### 1.3 What Is NOT Declared Today

The current format is silent on several critical concerns:

- **Tool access**: No declaration of which tools an agent can use. All agents get the same Claude toolset. Access control is implicit (e.g., "Dogs NEVER send mail" is prose in a template, not an enforced constraint).
- **Memory/context sources**: No declaration of what files, databases, or state an agent reads. References to `state.json`, `heartbeat.json`, `routes.jsonl` are scattered through prose.
- **Inter-agent communication**: Communication protocols (nudge vs. mail, who talks to whom) are documented in prose templates, not structured config.
- **Resource limits**: No token budgets, cost tiers, or time limits in the role definition itself.
- **Composability**: No inheritance, no mixins. Every role is a standalone definition. Shared patterns (the "Propulsion Principle", hookable mail, prefix-based routing) are copy-pasted across templates.
- **Lifecycle hooks**: Startup, shutdown, and handoff behavior are prose instructions, not declared hooks.

---

## 2. Design Questions for the Community

### 2.1 Format: YAML vs. TOML vs. JSON?

| Format | Pros | Cons |
|--------|------|------|
| **YAML** | Human-readable, widely adopted in DevOps/k8s ecosystem, supports anchors for reuse, multi-line strings natural | Whitespace-sensitive, footgun-prone (Norway problem, implicit type coercion) |
| **TOML** | Current Gas Town format, explicit typing, no indentation ambiguity | Deeply nested structures are verbose, limited ecosystem tooling, no inheritance/anchors |
| **JSON** | Universal parsing, schema validation (JSON Schema), no ambiguity | Not human-writable, no comments, verbose |
| **CUE/Jsonnet** | First-class composition, constraints, validation | Learning curve, smaller ecosystem, compilation step |

**Strawman position**: YAML with a JSON Schema for validation. Rationale: the k8s ecosystem has proven YAML works at scale for declarative infrastructure, the anchors enable the composability Gas Town currently lacks, and JSON Schema gives us validation without a custom toolchain.

### 2.2 Required Fields

What MUST every role definition include?

| Field | Purpose | Current equivalent |
|-------|---------|-------------------|
| `name` | Role identifier | `role` in TOML |
| `version` | Schema version for forward compat | None |
| `scope` | Where it runs (town/rig/global) | `scope` in TOML |
| `description` | Human-readable purpose | Comment in TOML |
| `tools` | Tool access declaration | None (implicit) |
| `context` | Memory/state sources | None (prose) |
| `constraints` | Resource limits and boundaries | Partial (`health` section) |
| `communication` | Channels and protocols | None (prose) |
| `lifecycle` | Startup/shutdown/handoff hooks | None (prose) |
| `prompt` | Behavioral specification | `prompt_template` reference |

### 2.3 Tool Access Declaration

How should we declare what tools an agent can use?

**Option A: Allowlist**
```yaml
tools:
  - gt-nudge
  - gt-mail-send
  - bd-show
  - bd-list
```

**Option B: Capability-based**
```yaml
capabilities:
  beads: [read, create, close]
  mail: [send, read, delete]
  git: [commit, push-branch]  # but NOT push-main
  sessions: [peek, nudge]     # but NOT kill
```

**Option C: Permission tiers**
```yaml
permissions:
  tier: operator  # tiers: observer, worker, operator, admin
  overrides:
    deny: [git-push-main, mail-send-human]
    allow: [session-kill]  # escalated from operator default
```

**Question**: Should tool access be enforced at runtime (hard deny) or advisory (the agent sees "you should not use X" in its prompt)?

### 2.4 Context and Memory

How should we declare what state an agent reads and writes?

```yaml
context:
  files:
    - path: "{work_dir}/state.json"
      access: read-write
      purpose: "Patrol tracking and nudge counts"
    - path: "{town}/deacon/heartbeat.json"
      access: read
      purpose: "Deacon freshness signal"
  databases:
    - name: beads
      access: read-write
      routing: prefix-based
  shared_state:
    - key: "rig.{rig_name}.health"
      access: write
```

**Questions**:
- Should context declarations be enforced (sandboxed file access) or documentary?
- How do we handle dynamic context (agent discovers new files at runtime)?
- Should there be a distinction between "must have at startup" and "may access during execution"?

### 2.5 Agent Constraints

```yaml
constraints:
  health:
    ping_timeout: 30s
    consecutive_failures: 3
    kill_cooldown: 5m
    stuck_threshold: 4h
  resources:
    max_tokens_per_session: 500000
    max_cost_per_hour: "$2.00"
    max_concurrent_subagents: 3
  sandbox:
    allowed_paths:
      - "{work_dir}/**"
      - "{town}/.beads/**"
    denied_paths:
      - "{town}/*/crew/*/secrets/**"
    network: restricted  # none | restricted | unrestricted
```

**Questions**:
- Should token/cost limits be per-session, per-hour, or per-task?
- How should sandbox boundaries interact with tool access declarations?
- Should constraints be soft (warn) or hard (kill)?

### 2.6 Inter-Agent Communication

```yaml
communication:
  identity: "{rig}/witness"
  channels:
    nudge:
      can_send_to: ["{rig}/*", "deacon/"]
      can_receive_from: ["deacon/", "{rig}/*"]
    mail:
      can_send_to: ["mayor/", "deacon/"]
      can_receive_from: ["*"]
      rate_limit: "10/hour"
  protocols:
    - name: MERGE_READY
      channel: mail
      target: "{rig}/refinery"
    - name: HEALTH_CHECK
      channel: nudge
      target: "{rig}/*"
```

**Questions**:
- Should communication rules be enforced or advisory?
- How do we handle pub/sub patterns (e.g., "all witnesses subscribe to deacon health events")?
- Should protocol definitions live in the role or in a separate shared protocols file?

### 2.7 Composability

The current system has massive duplication. The "Propulsion Principle" section, hookable mail instructions, prefix-based routing docs, and command quick-references are copy-pasted across 8 templates.

**Option A: Template inheritance**
```yaml
extends: base-worker
overrides:
  scope: rig
  stuck_threshold: 4h
```

**Option B: Mixins**
```yaml
mixins:
  - propulsion-principle
  - hookable-mail
  - prefix-routing
  - beads-filing-guide
```

**Option C: Traits (capability bundles)**
```yaml
traits:
  - patrol-agent      # adds patrol loop, state.json, cycle reporting
  - git-worker        # adds worktree, pre-sync, push capability
  - mail-participant  # adds inbox, send, delete
```

**Questions**:
- Should composition happen at the config level (merge YAML) or at the prompt level (compose template fragments)?
- How deep should inheritance go? (Gas Town roles are fairly flat today.)
- Should traits carry both structural config AND behavioral prompt text?

---

## 3. Strawman Proposal: Gas City Role Format v0.1

Format: YAML with JSON Schema validation.

### 3.1 Schema Overview

```yaml
apiVersion: gascity/v1alpha1
kind: Role
metadata:
  name: <string>
  description: <string>
  labels:
    tier: <infrastructure|worker|ephemeral>
    scope: <town|rig|global>
```

### 3.2 Example: Mayor

```yaml
apiVersion: gascity/v1alpha1
kind: Role
metadata:
  name: mayor
  description: >
    Global coordinator for cross-rig work. One per town.
    The main drive shaft of the Gas Town engine.
  labels:
    tier: infrastructure
    scope: town

spec:
  # --- Scheduling ---
  scheduling:
    instances: 1          # Singleton
    scope: town
    session:
      pattern: "hq-mayor"
      work_dir: "{town}/mayor"

  # --- Identity ---
  identity:
    address: "mayor/"
    env:
      GT_ROLE: mayor
      GT_SCOPE: town

  # --- Tool Access ---
  tools:
    capabilities:
      beads: [read, create, update, close, assign]
      mail: [send, read, delete]
      git: [status, log, diff]         # Mayor coordinates, does not code
      sessions: [peek, nudge, start, stop, kill]
      rig: [park, unpark, dock, undock, start, stop]
      dispatch: [sling, dog-dispatch]
    deny:
      - git-commit
      - git-push

  # --- Context ---
  context:
    prompt:
      template: mayor.md.tmpl
      mixins:
        - propulsion-principle
        - capability-ledger
        - hookable-mail
        - prefix-routing
    state:
      - path: "{town}/mayor/state.json"
        access: read-write
      - path: "{town}/deacon/heartbeat.json"
        access: read
    databases:
      - name: beads
        access: read-write

  # --- Communication ---
  communication:
    channels:
      nudge:
        send: ["*"]
        receive: ["deacon/", "*/witness"]
      mail:
        send: ["*"]
        receive: ["*"]
        rate_limit: "50/hour"

  # --- Health & Constraints ---
  constraints:
    health:
      ping_timeout: 30s
      consecutive_failures: 3
      kill_cooldown: 5m
      stuck_threshold: 1h
    resources:
      max_tokens_per_session: 1000000
      cost_tier: high

  # --- Lifecycle ---
  lifecycle:
    startup:
      - check-hook
      - check-mail
      - await-instructions
    shutdown:
      - handoff-context
      - clear-hook
    on_stuck:
      action: restart
      notify: human
```

### 3.3 Example: Crew Worker

```yaml
apiVersion: gascity/v1alpha1
kind: Role
metadata:
  name: crew
  description: >
    Persistent user-managed workspace. The pistons of the engine.
    Multiple instances per rig, each with a unique name and worktree.
  labels:
    tier: worker
    scope: rig

spec:
  scheduling:
    instances: "*"        # Multiple allowed
    scope: rig
    session:
      pattern: "{prefix}-crew-{name}"
      work_dir: "{town}/{rig}/crew/{name}"
      needs_pre_sync: true
      needs_worktree: true

  identity:
    address: "{rig}/crew/{name}"
    env:
      GT_ROLE: crew
      GT_SCOPE: rig

  tools:
    capabilities:
      beads: [read, create, update, close]
      mail: [send, read, delete]
      git: [status, log, diff, commit, push-branch, create-pr]
      code: [read, write, edit, grep, glob]
      test: [run, lint]
    deny:
      - git-push-main
      - session-kill

  context:
    prompt:
      template: crew.md.tmpl
      mixins:
        - propulsion-principle
        - hookable-mail
        - prefix-routing
        - beads-filing-guide
        - approval-fallacy      # "There is no approval step"
    state:
      - path: "{work_dir}/state.json"
        access: read-write
    databases:
      - name: beads
        access: read-write

  communication:
    channels:
      nudge:
        send: ["{rig}/witness", "mayor/"]
        receive: ["{rig}/witness", "deacon/"]
      mail:
        send: ["mayor/", "{rig}/witness", "{rig}/refinery"]
        receive: ["*"]
        rate_limit: "20/hour"

  constraints:
    health:
      ping_timeout: 30s
      consecutive_failures: 3
      kill_cooldown: 5m
      stuck_threshold: 4h
    resources:
      max_tokens_per_session: 500000
      cost_tier: standard

  lifecycle:
    startup:
      - check-hook
      - execute-or-check-mail
    shutdown:
      - push-unpushed-work
      - handoff-context
    on_stuck:
      action: nudge-then-restart
      escalate_to: "{rig}/witness"
```

### 3.4 Example: Polecat (Ephemeral Task Runner)

```yaml
apiVersion: gascity/v1alpha1
kind: Role
metadata:
  name: polecat
  description: >
    Persistent identity, ephemeral sessions. Batch work dispatch.
    Spawned by sling, executes a formula, calls done, session terminates.
  labels:
    tier: ephemeral
    scope: rig

spec:
  scheduling:
    instances: "*"
    scope: rig
    session:
      pattern: "{prefix}-{name}"
      work_dir: "{town}/{rig}/polecats/{name}"
      needs_pre_sync: false
      needs_worktree: true

  identity:
    address: "{rig}/polecats/{name}"
    env:
      GT_ROLE: polecat
      GT_SCOPE: rig

  tools:
    capabilities:
      beads: [read, update]
      git: [status, log, diff, commit, push-branch]
      code: [read, write, edit, grep, glob]
      test: [run, lint]
    deny:
      - git-push-main
      - mail-send          # Polecats nudge, never mail
      - session-kill
      - beads-create       # Polecats work assigned issues, don't create new ones

  context:
    prompt:
      template: polecat.md.tmpl
      mixins:
        - propulsion-principle
        - completion-protocol   # "Run gt done. No exceptions."
        - prefix-routing
    state: []                   # Ephemeral -- no persistent state
    databases:
      - name: beads
        access: read

  communication:
    channels:
      nudge:
        send: ["{rig}/witness"]
        receive: ["{rig}/witness", "deacon/"]
      mail:
        send: []              # Polecats NEVER send mail
        receive: ["{rig}/witness"]

  constraints:
    health:
      ping_timeout: 30s
      consecutive_failures: 3
      kill_cooldown: 5m
      stuck_threshold: 2h
    resources:
      max_tokens_per_session: 300000
      cost_tier: economy
    sandbox:
      allowed_paths:
        - "{work_dir}/**"
      network: restricted

  lifecycle:
    startup:
      - check-hook
      - execute-formula
    shutdown:
      - gt-done            # MUST call gt done
    on_stuck:
      action: notify-witness
      escalate_to: "{rig}/witness"
    on_complete:
      action: auto-terminate  # Session ends after gt done
```

---

## 4. Open Questions

These need community input before the format can be finalized.

### 4.1 Enforcement Model

**Hard vs. soft constraints.** Today, "Dogs NEVER send mail" is prose that agents sometimes violate. Should Gas City enforce this at the runtime level (intercept and deny the tool call) or keep it advisory (include in prompt, trust the model)?

- Hard enforcement is safer but requires a middleware layer between the agent and its tools.
- Advisory is simpler but relies on model compliance, which degrades under context pressure.
- Hybrid: enforce critical rules (no push to main), advise on style rules (prefer nudge over mail).

### 4.2 Prompt vs. Config Boundary

How much behavioral specification belongs in the structured YAML vs. the prose template?

- **Maximalist config**: Everything declarable goes in YAML. The prompt template is minimal, generated from config fields.
- **Minimalist config**: Config handles scheduling, health, and tool access. Behavioral instructions stay in prose templates.
- **Middle ground**: Config declares WHAT (capabilities, constraints, communication rules). Templates describe HOW (operational procedures, judgment heuristics, gotchas).

The current templates contain both declarative content (command tables, session patterns, file paths) and genuinely non-declarable content (judgment guidance like "be conservative -- false positives disrupt legitimate work"). The format needs to handle both.

### 4.3 Versioning and Migration

- How do we version the schema? (`apiVersion: gascity/v1alpha1` is a starting point.)
- How do we migrate existing TOML configs to the new format?
- Should the system support running both formats during transition?

### 4.4 Multi-Town / Federation

Gas Town is evolving toward HOP (Highway Operations Protocol) federation. Should the role format account for:

- Roles that span multiple towns?
- Portable role definitions (a "crew worker" means the same thing everywhere)?
- Town-specific vs. universal role fields?
- Role versioning across federated instances?

### 4.5 Dynamic Role Mutation

Can roles change at runtime? Examples:

- A crew worker temporarily gains `session-kill` capability during an incident.
- A polecat's `stuck_threshold` is extended for a known-long task.
- A witness gains `mail-send-human` escalation capability during an outage.

Should these be supported as runtime overrides, or must they be config changes that require a restart?

### 4.6 Custom Roles

Should Gas City support user-defined roles beyond the built-in 8? Examples:

- A `reviewer` role that only reads code and comments on PRs.
- A `researcher` role (headless variant) that only does web searches and writes reports.
- A `security-auditor` role with read-only access to everything.

If yes, what is the minimum viable role definition?

### 4.7 Observability Hooks

Should the role format declare what telemetry an agent emits?

```yaml
observability:
  metrics:
    - patrol_cycle_duration
    - beads_closed_per_session
  logs:
    retention: 24h
    level: info
  traces:
    enabled: true
    sample_rate: 0.1
```

---

## 5. Next Steps: How to Contribute

### 5.1 Comment on This Document

Post feedback on the Wasteland board under `w-com-005`, or open a GitHub discussion on `steveyegge/gastown`.

### 5.2 Specific Feedback Requested

For each section, we want to hear:

1. **Format choice**: Do you agree with YAML + JSON Schema? If not, what and why?
2. **Field completeness**: What fields are missing from the strawman? What fields are unnecessary?
3. **Enforcement model**: Hard, soft, or hybrid? Where do you draw the line?
4. **Composability**: Inheritance, mixins, or traits? Or something else entirely?
5. **Migration**: What is the acceptable cost of migrating from TOML to the new format?

### 5.3 Prototype

If you want to try the format:

1. Write a role definition for your own agent using the strawman schema above.
2. Identify where the format fails to express something your agent needs.
3. Post the role definition and the failure case.

### 5.4 Timeline

- **Week 1**: Collect feedback on this document.
- **Week 2**: Revise strawman based on feedback, produce JSON Schema draft.
- **Week 3**: Prototype loader that reads both TOML (legacy) and YAML (new).
- **Week 4**: Second campfire to review prototype and finalize v1alpha1.

---

## Appendix A: Current Role Inventory

| Role | Scope | Instances | Stuck Threshold | Key Trait |
|------|-------|-----------|-----------------|-----------|
| mayor | town | 1 | 1h | Coordinator, no code |
| deacon | town | 1 | 1h | Patrol executor, heartbeat |
| dog | town | N | 2h | Infrastructure worker, auto-terminates |
| witness | rig | 1 | 1h | Oversight, no code |
| refinery | rig | 1 | 2h | Merge queue processor |
| polecat | rig | N | 2h | Ephemeral task runner |
| crew | rig | N | 4h | Persistent developer workspace |
| headless | rig | N | (none) | Non-repo tasks, no worktree |

## Appendix B: Current Override Resolution

```
Built-in (embedded in binary)
    |
    v  merge non-zero fields
Town-level (<town>/roles/<role>.toml)
    |
    v  merge non-zero fields
Rig-level (<rig>/roles/<role>.toml)
    |
    v
Final RoleDefinition
```

This layering should be preserved or improved in Gas City. The strawman proposal is compatible with this model -- YAML files at each layer, merged with the same semantics.
