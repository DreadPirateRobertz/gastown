# Pinned Beads Architecture

> **Status**: Design Draft
> **Author**: max (crew)
> **Date**: 2025-12-23

## Overview

Every Gas Town agent has a **pinned bead** - a persistent hook that serves as their
work attachment point. This document formalizes the semantics, discovery mechanism,
and lifecycle of pinned beads across all agent roles.

## The Pinned Bead Concept

A pinned bead is:
- A bead with `status: pinned` (never closes)
- Titled `"{role} Handoff"` (e.g., "Toast Handoff" for polecat Toast)
- Contains attachment fields in its description when work is slung

```
┌─────────────────────────────────────────────┐
│  Pinned Bead: "Toast Handoff"               │
│  Status: pinned                             │
│  Description:                               │
│    attached_molecule: gt-xyz                │
│    attached_at: 2025-12-23T15:30:45Z        │
└─────────────────────────────────────────────┘
         │
         ▼ (points to)
┌─────────────────────────────────────────────┐
│  Molecule: gt-xyz                           │
│  Title: "Implement feature X"               │
│  Type: epic (molecule root)                 │
│  Children: gt-abc, gt-def, gt-ghi (steps)   │
└─────────────────────────────────────────────┘
```

## Role-by-Role Pinned Bead Semantics

### 1. Polecat

| Aspect | Value |
|--------|-------|
| **Title Pattern** | `{name} Handoff` (e.g., "Toast Handoff") |
| **Beads Location** | Rig-level (`.beads/` in rig root) |
| **Prefix** | `gt-*` |
| **Created By** | `gt sling` on first assignment |
| **Typical Content** | Molecule for feature/bugfix work |
| **Lifecycle** | Created → Work attached → Work detached → Dormant |

**Discovery**:
```bash
gt mol status                  # Shows what's on your hook
bd list --status=pinned        # Low-level: finds pinned beads
```

**Protocol**:
1. On startup, check `gt mol status`
2. If work attached → Execute molecule steps
3. If no work → Check mail inbox for new assignments
4. On completion → Work auto-detached, check again

### 2. Crew

| Aspect | Value |
|--------|-------|
| **Title Pattern** | `{name} Handoff` (e.g., "joe Handoff") |
| **Beads Location** | Clone-level (`.beads/` in crew member's clone) |
| **Prefix** | `gt-*` |
| **Created By** | `gt sling` or manual pinned bead creation |
| **Typical Content** | Longer-lived work, multi-session tasks |
| **Lifecycle** | Persistent across sessions, human-managed |

**Key difference from Polecat**:
- No witness monitoring
- Human decides when to detach work
- Work persists until explicitly completed

**Discovery**: Same as polecat (`gt mol status`)

### 3. Witness

| Aspect | Value |
|--------|-------|
| **Title Pattern** | `Witness Handoff` |
| **Beads Location** | Rig-level |
| **Prefix** | `gt-*` |
| **Created By** | Deacon or Mayor sling |
| **Typical Content** | Patrol wisp (ephemeral) |
| **Lifecycle** | Wisp attached → Patrol executes → Wisp squashed → New wisp |

**Protocol**:
1. On startup, check `gt mol status`
2. If wisp attached → Execute patrol cycle
3. Squash wisp on completion (creates digest)
4. Loop or await new sling

### 4. Refinery

| Aspect | Value |
|--------|-------|
| **Title Pattern** | `Refinery Handoff` |
| **Beads Location** | Rig-level |
| **Prefix** | `gt-*` |
| **Created By** | Witness or Mayor sling |
| **Typical Content** | Epic with batch of issues |
| **Lifecycle** | Epic attached → Dispatch to polecats → Monitor → Epic completed |

**Protocol**:
1. Check `gt mol status` for attached epic
2. Dispatch issues to polecats via `gt sling issue polecat/name`
3. Monitor polecat progress
4. Report completion when all issues closed

### 5. Deacon

| Aspect | Value |
|--------|-------|
| **Title Pattern** | `Deacon Handoff` |
| **Beads Location** | Town-level (`~/gt/.beads/`) |
| **Prefix** | `hq-*` |
| **Created By** | Mayor sling or self-loop |
| **Typical Content** | Patrol wisp (always ephemeral) |
| **Lifecycle** | Wisp → Execute → Squash → Loop |

**Protocol**:
1. Check `gt mol status`
2. Execute patrol wisp steps
3. Squash wisp to digest
4. Self-sling new patrol wisp and loop

### 6. Mayor

| Aspect | Value |
|--------|-------|
| **Title Pattern** | `Mayor Handoff` |
| **Beads Location** | Town-level (`~/gt/.beads/`) |
| **Prefix** | `hq-*` |
| **Created By** | External sling or self-assignment |
| **Typical Content** | Strategic work, cross-rig coordination |
| **Lifecycle** | Human-managed like Crew |

**Key difference**: Mayor is human-controlled (like Crew), but operates at
town level with visibility into all rigs.

## Summary Table

| Role | Title Pattern | Beads Level | Prefix | Ephemeral? | Managed By |
|------|---------------|-------------|--------|------------|------------|
| Polecat | `{name} Handoff` | Rig | `gt-` | No | Witness |
| Crew | `{name} Handoff` | Clone | `gt-` | No | Human |
| Witness | `Witness Handoff` | Rig | `gt-` | Yes (wisp) | Deacon |
| Refinery | `Refinery Handoff` | Rig | `gt-` | No | Witness |
| Deacon | `Deacon Handoff` | Town | `hq-` | Yes (wisp) | Self/Mayor |
| Mayor | `Mayor Handoff` | Town | `hq-` | No | Human |

---

## Discovery Mechanism

### How Does a Worker Find Its Pinned Bead?

Workers find their pinned bead through a **title-based lookup**:

```go
// In beads.go
func (b *Beads) FindHandoffBead(role string) (*Issue, error) {
    issues := b.List(ListOptions{Status: StatusPinned})
    targetTitle := HandoffBeadTitle(role)  // "{role} Handoff"
    for _, issue := range issues {
        if issue.Title == targetTitle {
            return issue, nil
        }
    }
    return nil, nil  // Not found
}
```

**CLI path**:
```bash
gt mol status [target]
```

This:
1. Determines the agent's role/identity from `[target]` or environment
2. Calls `FindHandoffBead(role)` to find the pinned bead
3. Parses `AttachmentFields` from the description
4. Shows attached molecule and progress

### Current Limitation: Role Naming

The current implementation uses simple role names:
- Polecat: Uses polecat name (e.g., "Toast")
- Others: Use role name (e.g., "Witness", "Refinery")

**Problem**: If there are multiple polecats, each needs a unique handoff bead.

**Solution (current)**: The role passed to `FindHandoffBead` is the polecat's
name, not "polecat". So "Toast Handoff" is different from "Alpha Handoff".

---

## Dashboard Visibility

### How Do Users See Pinned Beads?

**Current state**: Limited visibility

**Proposed commands**:

```bash
# Show all hooks across a rig
gt hooks [rig]

# Output:
# 📌 Hooks in gastown:
#
#   Polecat/Toast:     gt-xyz (feature: Add login)
#   Polecat/Alpha:     (empty)
#   Witness:           wisp-abc (patrol cycle)
#   Refinery:          gt-epic-123 (batch: Q4 features)
#   Crew/joe:          gt-789 (long: Refactor auth)
#   Crew/max:          (empty)

# Show hook for specific agent
gt mol status polecat/Toast
gt mol status witness/
gt mol status crew/joe
```

### Dashboard Data Structure

```go
type HookStatus struct {
    Agent          string    // Full address (gastown/polecat/Toast)
    Role           string    // polecat, witness, refinery, crew, deacon, mayor
    HasWork        bool      // Is something attached?
    AttachedID     string    // Molecule/issue ID
    AttachedTitle  string    // Human-readable title
    AttachedAt     time.Time // When attached
    IsWisp         bool      // Ephemeral?
    Progress       *Progress // Completion percentage
}

type RigHooks struct {
    Rig      string
    Polecats []HookStatus
    Crew     []HookStatus
    Witness  HookStatus
    Refinery HookStatus
}

type TownHooks struct {
    Deacon HookStatus
    Mayor  HookStatus
    Rigs   []RigHooks
}
```

### Proposed: `gt dashboard`

A unified view of all hooks and work status:

```
╔══════════════════════════════════════════════════════════════╗
║                     Gas Town Dashboard                        ║
╠══════════════════════════════════════════════════════════════╣
║ Town: /Users/stevey/gt                                        ║
║                                                               ║
║ 🎩 Mayor:    (empty)                                          ║
║ ⛪ Deacon:   wisp-patrol (running patrol cycle)               ║
║                                                               ║
║ 📦 Rig: gastown                                               ║
║   👁 Witness:  wisp-watch (monitoring 2 polecats)             ║
║   🏭 Refinery: gt-epic-45 (3/8 issues merged)                 ║
║   🐱 Polecats:                                                ║
║      Toast:   gt-xyz [████████░░] 75% (Implement feature)     ║
║      Alpha:   (empty)                                         ║
║   👷 Crew:                                                    ║
║      joe:     gt-789 [██░░░░░░░░] 20% (Refactor auth)         ║
║      max:     (empty)                                         ║
╚══════════════════════════════════════════════════════════════╝
```

---

## Multiple Pins Semantics

### Question: What if a worker has multiple pinned beads?

**Current behavior**: Only first pinned bead with matching title is used.

**Design decision**: **One hook per agent** (enforced)

Rationale:
- The Propulsion Principle says "if you find something on your hook, run it"
- Multiple hooks would require decision-making about which to run
- Decision-making violates propulsion
- Work queuing belongs in mail or at dispatcher level (Witness/Refinery)

### Enforcement

```go
func (b *Beads) GetOrCreateHandoffBead(role string) (*Issue, error) {
    existing, err := b.FindHandoffBead(role)
    if existing != nil {
        return existing, nil  // Always return THE ONE handoff bead
    }
    // Create if not found...
}
```

**If somehow multiple exist** (data corruption):
- `gt doctor` should flag as error
- `FindHandoffBead` returns first match (deterministic by ID sort)
- Manual cleanup required

### Hook Collision on Sling

When slinging to an occupied hook:

```bash
$ gt sling feature polecat/Toast
Error: polecat/Toast hook already occupied with gt-xyz
Use --force to replace, or wait for current work to complete.

$ gt sling feature polecat/Toast --force
⚠️  Detaching gt-xyz from polecat/Toast
📌 Attached gt-abc to polecat/Toast
```

**`--force` semantics**:
1. Detach current work (leaves it orphaned in beads)
2. Attach new work
3. Log the replacement for audit

---

## Mail Attachments vs Pinned Attachments

### Question: What about mails with attached work that aren't pinned?

**Scenario**: Agent receives mail with `attached_molecule: gt-xyz` in body,
but the mail itself is not pinned, and their hook is empty.

### Current Protocol

```
1. Check hook (gt mol status)
   → If work on hook → Run it

2. Check mail inbox
   → If mail has attached work → Mail is the sling delivery mechanism
   → The attached work gets pinned to hook automatically

3. No work anywhere → Idle/await
```

### Design Decision: Mail Is Delivery, Hook Is Authority

**Mail with attachment** = "Here's work for you"
**Hook with attachment** = "This is your current work"

The sling operation does both:
1. Creates mail notification (optional, for context)
2. Attaches to hook (authoritative)

**But what if only mail exists?** (manual mail, broken sling):

| Situation | Expected Behavior |
|-----------|-------------------|
| Hook has work, mail has work | Hook wins. Mail is informational. |
| Hook empty, mail has work | Agent should self-pin from mail. |
| Hook has work, mail empty | Work continues from hook. |
| Both empty | Idle. |

### Protocol for Manual Work Assignment

If someone sends mail with attached work but doesn't sling:

```markdown
## Agent Startup Protocol (Extended)

1. Check hook: `gt mol status`
   - Found work? **Run it.**

2. Hook empty? Check mail: `gt mail inbox`
   - Found mail with `attached_molecule`?
   - Self-pin it: `gt mol attach <your-hook> <molecule-id>`
   - Then run it.

3. Nothing? Idle.
```

### Self-Pin Command

```bash
# Agent self-pins work from mail
gt mol attach-from-mail <mail-id>

# This:
# 1. Reads mail body for attached_molecule field
# 2. Attaches molecule to agent's hook
# 3. Marks mail as read
# 4. Returns control for execution
```

---

## `gt doctor` Checks

### Current State

`gt doctor` doesn't check pinned beads at all.

### Proposed Checks

```go
// DoctorCheck definitions for pinned beads
var pinnedBeadChecks = []DoctorCheck{
    {
        Name:        "hook-singleton",
        Description: "Each agent has at most one handoff bead",
        Check:       checkHookSingleton,
    },
    {
        Name:        "hook-attachment-valid",
        Description: "Attached molecules exist and are not closed",
        Check:       checkHookAttachmentValid,
    },
    {
        Name:        "orphaned-attachments",
        Description: "No molecules attached to non-existent hooks",
        Check:       checkOrphanedAttachments,
    },
    {
        Name:        "stale-attachments",
        Description: "Attached molecules not stale (>24h without progress)",
        Check:       checkStaleAttachments,
    },
    {
        Name:        "hook-agent-mismatch",
        Description: "Hook titles match existing agents",
        Check:       checkHookAgentMismatch,
    },
}
```

### Check Details

#### 1. hook-singleton
```
✗ Multiple handoff beads for polecat/Toast:
  - gt-abc: "Toast Handoff" (created 2025-12-01)
  - gt-xyz: "Toast Handoff" (created 2025-12-15)

  Fix: Delete duplicate(s) with `bd close gt-xyz --reason="duplicate hook"`
```

#### 2. hook-attachment-valid
```
✗ Hook attachment points to missing molecule:
  Hook: gt-abc (Toast Handoff)
  Attached: gt-xyz (not found)

  Fix: Clear attachment with `gt mol detach polecat/Toast`
```

#### 3. orphaned-attachments
```
⚠ Molecule attached but agent doesn't exist:
  Molecule: gt-xyz (attached to "Defunct Handoff")
  Agent: polecat/Defunct (not found)

  Fix: Re-sling to active agent or close molecule
```

#### 4. stale-attachments
```
⚠ Stale work on hook (48h without progress):
  Hook: polecat/Toast
  Molecule: gt-xyz (attached 2025-12-21T10:00:00Z)
  Last activity: 2025-12-21T14:30:00Z

  Suggestion: Check polecat status, consider nudge or reassignment
```

#### 5. hook-agent-mismatch
```
⚠ Handoff bead for non-existent agent:
  Hook: "OldPolecat Handoff" (gt-abc)
  Agent: polecat/OldPolecat (no worktree found)

  Fix: Close orphaned hook or recreate agent
```

### Implementation

```go
func (d *Doctor) checkPinnedBeads() []DoctorResult {
    results := []DoctorResult{}

    // Get all pinned beads
    pinned, _ := d.beads.List(ListOptions{Status: StatusPinned})

    // Group by title suffix (role)
    byRole := groupByRole(pinned)

    // Check singleton
    for role, beads := range byRole {
        if len(beads) > 1 {
            results = append(results, DoctorResult{
                Check:   "hook-singleton",
                Status:  "error",
                Message: fmt.Sprintf("Multiple hooks for %s", role),
                Details: beads,
            })
        }
    }

    // Check attachment validity
    for _, bead := range pinned {
        fields := ParseAttachmentFields(bead)
        if fields != nil && fields.AttachedMolecule != "" {
            mol, err := d.beads.Show(fields.AttachedMolecule)
            if err != nil || mol == nil {
                results = append(results, DoctorResult{
                    Check:   "hook-attachment-valid",
                    Status:  "error",
                    Message: "Attached molecule not found",
                    // ...
                })
            }
        }
    }

    // ... other checks

    return results
}
```

---

## Terminology Decisions

Based on this analysis, proposed standard terminology:

| Term | Definition |
|------|------------|
| **Hook** | The pinned bead where work attaches (the attachment point) |
| **Pinned Bead** | A bead with `status: pinned` (never closes) |
| **Handoff Bead** | The specific pinned bead titled "{role} Handoff" |
| **Attachment** | The molecule/issue currently on a hook |
| **Sling** | The act of putting work on an agent's hook |
| **Lead Bead** | The root bead of an attached molecule (synonym: molecule root) |

**Recommendation**: Use "hook" in user-facing commands and docs, "handoff bead"
in implementation details.

---

## Open Questions

### 1. Should mail notifications include hook status?

```
📬 New work assigned!

From: witness/
Subject: Feature work assigned

Work: gt-xyz (Implement login)
Molecule: feature (4 steps)

🪝 Hooked to: polecat/Toast
   Status: Attached and ready

Run `gt mol status` to see details.
```

**Recommendation**: Yes. Mail should confirm hook attachment succeeded.

### 2. Should agents be able to self-detach?

```bash
# Polecat decides work is blocked and gives up
gt mol detach
```

**Recommendation**: Yes, but with audit trail. Detachment without completion
should be logged and possibly notify Witness.

### 3. Multiple rigs with same agent names?

```
gastown/polecat/Toast
otherrig/polecat/Toast
```

**Current**: Each rig has separate beads, so no collision.
**Future**: If cross-rig visibility needed, full addresses required.

### 4. Hook persistence during agent recreation?

When a polecat is killed and recreated:
- Hook bead persists (it's in rig beads, not agent's worktree)
- Old attachment may be stale
- New sling should `--force` or detach first

**Recommendation**: `gt polecat recreate` should clear hook.

---

## Implementation Roadmap

### Phase 1: Doctor Checks (immediate)
- [ ] Add `hook-singleton` check
- [ ] Add `hook-attachment-valid` check
- [ ] Add to default doctor run

### Phase 2: Dashboard Visibility
- [ ] Implement `gt hooks` command
- [ ] Add hook status to `gt status` output
- [ ] Consider `gt dashboard` for full view

### Phase 3: Protocol Enforcement
- [ ] Add self-pin from mail (`gt mol attach-from-mail`)
- [ ] Audit trail for detach operations
- [ ] Witness notification on abnormal detach

### Phase 4: Documentation
- [ ] Update role prompt templates with hook protocol
- [ ] Add troubleshooting guide for hook issues
- [ ] Document recovery procedures

---

## Summary

The pinned bead architecture provides:

1. **Universal hook per agent** - Every agent has exactly one handoff bead
2. **Title-based discovery** - `{role} Handoff` naming convention
3. **Attachment fields** - `attached_molecule` and `attached_at` in description
4. **Propulsion compliance** - One hook = no decision paralysis
5. **Mail as delivery** - Sling sends notification, attaches to hook
6. **Doctor validation** - Checks for singleton, validity, staleness

The key insight is that **hooks are authority, mail is notification**. If
they conflict, the hook wins. This maintains the Propulsion Principle:
"If you find something on your hook, YOU RUN IT."
