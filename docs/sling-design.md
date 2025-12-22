# The Sling: Gas Town's Universal Work Dispatch

> **Status**: Design Draft
> **Issue**: gt-z3qf

## The Propulsion Principle

Gas Town runs on one rule:

> **If you find something on your hook, YOU RUN IT.**

No decisions. No "should I?" Just ignition. This is the Universal Gas Town
Propulsion Principle. Every agent - polecat, deacon, witness, refinery - follows
this single rule.

```
Hook has work → Work happens.
```

That's the whole engine. Everything else is plumbing.

## The Sling Operation

**`gt sling`** is the unified command for putting work on an agent's hook.

```bash
gt sling <thing> <target> [options]
```

Where:
- `<thing>` - What to sling (molecule, bead, epic)
- `<target>` - Who to sling it at (agent address)
- `[options]` - How to sling it (--wisp, --priority, etc.)

### Examples

```bash
# Sling a molecule at a polecat
gt sling feature polecat/alpha

# Sling a specific issue with a molecule
gt sling gt-xyz polecat/alpha --molecule bugfix

# Sling a wisp at the deacon (ephemeral, won't accumulate)
gt sling patrol deacon/ --wisp

# Sling an epic at refinery for batch processing
gt sling gt-epic-123 refinery/
```

### What Can Be Slung?

| Thing | Prefix | Example | Notes |
|-------|--------|---------|-------|
| Molecule proto | none | `gt sling feature polecat/alpha` | Spawns from proto |
| Issue/Bead | `gt-*`, `bd-*` | `gt sling gt-xyz polecat/alpha` | Work item |
| Epic | `gt-*` (type=epic) | `gt sling gt-epic refinery/` | Batch of issues |
| Wisp | `--wisp` flag | `gt sling patrol deacon/ --wisp` | Ephemeral molecule |

### What Happens When You Sling?

```
┌─────────────────────────────────────────────────────────┐
│                    gt sling lifecycle                    │
├─────────────────────────────────────────────────────────┤
│                                                          │
│  1. SPAWN (if proto)     2. ASSIGN           3. PIN     │
│     proto → molecule        mol → agent         → hook  │
│                                                          │
│  ┌─────────┐            ┌─────────┐        ┌─────────┐  │
│  │  Proto  │ ────────►  │Molecule │ ─────► │  Hook   │  │
│  │(catalog)│   spawn    │(instance)│ assign │(pinned) │  │
│  └─────────┘            └─────────┘        └─────────┘  │
│                                                  │       │
│                                            agent wakes   │
│                                                  │       │
│                                                  ▼       │
│                                            ┌─────────┐  │
│                                            │ IGNITION│  │
│                                            └─────────┘  │
└─────────────────────────────────────────────────────────┘
```

## The Agent's View

From the agent's perspective, life is simple:

```markdown
## Your One Rule

1. Check your hook: `gt mol status`
2. Found something? **Run it.** No thinking required.
3. Nothing? Check mail for new slings.
4. Repeat.
```

The agent never decides *whether* to run. The molecule tells them *what* to do.
They execute until complete, then check the hook again.

### Agent Startup (New Model)

```bash
# Old way (too much thinking)
gt mail inbox
if has_molecule; then
    gt molecule instantiate ...
    # figure out what to do...
fi

# New way (propulsion)
gt mol status          # What's on my hook?
# Output tells you exactly what to do
# Just follow the molecule phases
```

## Command Reference

### gt sling

```
gt sling <thing> <target> [flags]

Arguments:
  thing     Proto name, issue ID, or epic ID
  target    Agent address (polecat/name, deacon/, witness/, refinery/)

Flags:
  --wisp           Create ephemeral molecule (burned on complete, squashed to digest)
  --molecule, -m   Specify molecule proto when slinging an issue
  --priority, -p   Override priority (P0-P4)
  --force          Re-sling even if hook already has work

Examples:
  gt sling feature polecat/alpha              # Spawn feature mol, sling to alpha
  gt sling gt-xyz polecat/beta -m bugfix      # Sling issue with bugfix workflow
  gt sling patrol deacon/ --wisp              # Ephemeral patrol wisp
  gt sling gt-epic-batch refinery/            # Batch work to refinery
```

### gt mol status

```
gt mol status [target]

Shows what's on an agent's hook (or your own if no target).

Output:
  Slung: feature (gt-abc123)
  Phase: 2/4 - Implement
  Progress: ████████░░░░ 67%
  Wisp: no

  Next: Complete implementation, then run tests
```

### gt mol catalog

```
gt mol catalog

Lists available molecule protos that can be slung.

Output:
  NAME        PHASES  DESCRIPTION
  feature     4       Feature development workflow
  bugfix      3       Bug investigation and fix
  patrol      2       Operational check cycle (wisp-only)
  review      2       Code review workflow
```

## Relationship to bd mol

| Command | gt mol | bd mol | Notes |
|---------|--------|--------|-------|
| Create molecule | via `gt sling` | `bd mol spawn` | gt adds assignment |
| List protos | `gt mol catalog` | `bd mol catalog` | Same data |
| Show molecule | `gt mol status` | `bd mol show` | gt adds agent context |
| Combine | - | `bd mol bond` | Data operation only |
| Destroy | `gt mol burn` | `bd mol burn` | gt may need cleanup |
| Condense wisps | `gt mol squash` | `bd mol squash` | Same operation |

**Design principle**: `bd mol` is pure data operations. `gt sling` and `gt mol`
add agent context (assignment, hooks, sessions).

## Migration Path

### Old Commands → New

| Old | New | Notes |
|-----|-----|-------|
| `gt molecule instantiate` | `gt sling` | With assignment |
| `gt molecule attach` | `gt sling --force` | Re-sling to hook |
| `gt molecule detach` | `gt mol burn` | Or auto on complete |
| `gt molecule progress` | `gt mol status` | Better name |
| `gt molecule list` | `gt mol catalog` | Protos only |
| `gt spawn --molecule` | `gt sling` | Unified |

### Template Updates Required

1. **deacon.md.tmpl** - Use `gt mol status`, remove decision logic
2. **polecat.md.tmpl** - Propulsion principle, check hook on wake
3. **witness.md.tmpl** - Sling wisps to agents it spawns
4. **refinery.md.tmpl** - Accept slung epics

## Open Design Questions

1. **Default molecule**: If you `gt sling gt-xyz polecat/alpha` without `-m`,
   should we infer from issue type? (bug→bugfix, feature→feature)

2. **Hook collision**: If hook already has work and you sling more, error or queue?
   Current thinking: error unless `--force` (which replaces).

3. **Self-sling**: Can an agent sling to itself? (`gt sling patrol .`)
   Probably yes for wisp loops.

4. **Sling notification**: Should sling send mail to target, or is hook presence enough?
   Probably just hook - mail is for human-readable context.

## Implementation Plan

See beads:
- gt-z3qf: Parent issue (gt mol overhaul)
- [TBD]: gt sling command implementation
- [TBD]: gt mol status command
- [TBD]: Template updates for propulsion principle
- [TBD]: Documentation updates
