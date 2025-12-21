# Gas Town Vision

> Work is fractal. Every piece of work can contain other work, recursively.
> The machine that processes this work must be equally fractal.

## The Big Picture

Gas Town is an **enterprise-grade cognitive processing machine**. It takes structured work in the form of molecules - arbitrarily complex guardrails that AI workers follow to completion - and processes that work with full auditability, crash recovery, and guaranteed progress.

Think of it as the **IDE of 2026**: not a text editor with AI autocomplete, but a complete execution environment where AI agents are first-class workers with proper lifecycle management, coordination protocols, and quality gates.

## The Steam Engine

Gas Town is an engine. Engines do work and generate steam.

| Component | Role | Metaphor |
|-----------|------|----------|
| **Proto molecules** | Workflow templates | Fuel |
| **Wisps** | Transient execution traces | Steam |
| **Digests** | Compressed work records | Distillate |

**Protos** define how work should be done. **Wisps** are what happens during execution - transient, observable, but ultimately disposable. **Digests** are what remains: condensed permanent records of what was accomplished.

Just as steam can dissipate or be condensed into useful output, wisps can be:
- **Burned**: Discarded without trace (routine work, no audit needed)
- **Squashed**: Compressed into a digest (preserve the outcome, discard the trace)

This vocabulary runs through all of Gas Town: bond (instantiate), burn (discard), squash (compress), wisp (transient). It's a steam engine for work.

## Core Insights

### 1. Molecules Crystallize Workflows

Every organization has processes that humans follow: code review checklists, release procedures, onboarding steps. These exist as tribal knowledge, wiki pages, or forgotten documents.

**Molecules make these executable.** A molecule is a structured workflow template that:
- Defines exactly what steps must happen
- Encodes dependencies between steps
- Specifies quality gates that must pass
- Enables any worker to continue where another left off

```markdown
## Molecule: engineer-in-box
Full workflow from design to merge.

## Step: design
Think carefully about architecture. Write a brief design summary.

## Step: implement
Write the code. Follow codebase conventions.
Needs: design

## Step: test
Write and run tests. Cover edge cases.
Needs: implement

## Step: submit
Submit for merge via refinery.
Needs: test
```

This isn't just documentation - it's a **contract** that Gas Town enforces.

### 2. Nondeterministic Idempotence

The key property that enables autonomous operation:

- **Deterministic structure**: Molecule defines exactly what steps exist
- **Nondeterministic execution**: Any worker can execute any ready step
- **Idempotent progress**: Completed steps stay completed

**Why this matters:**
```
Worker A picks up "design" step
Worker A completes "design"
Worker A crashes mid-"implement"
Worker B restarts, queries ready work
Worker B sees "implement" is ready (design done, implement pending)
Worker B continues from exactly where A left off
```

No work is lost. No state is in memory. Any worker can continue any molecule. This is what makes 24/7 autonomous operation possible.

### 3. Beads: The Universal Data Plane

Gas Town uses **Beads** as both control plane and data plane. Everything flows through Beads:

| Data Type | Beads Representation |
|-----------|---------------------|
| Work items | Issues (tasks, bugs, features) |
| Workflows | Molecules (type=molecule) |
| Messages | Mail beads (type=message) |
| Merge requests | Queue entries (type=merge-request) |
| Agent state | Status on assigned issues |

**Key architectural insight**: The control state IS data in Beads. Molecule steps, dependencies, and status ARE the control plane. Agents read Beads to know what to do next.

This provides:
- **Fault tolerance**: Control state survives agent crashes
- **Observability**: `bd list` shows the full system state
- **Decentralization**: Each agent reads its own state from Beads
- **Recovery**: Restart = re-read Beads = continue from where you left off

There is no separate orchestrator maintaining workflow state. Beads IS the orchestrator.

### 4. The OS Metaphor

Gas Town is an operating system for AI work:

| OS Concept | Gas Town |
|------------|----------|
| Kernel | Daemon |
| Process scheduler | Ready work + dependencies |
| Timer interrupts | Timed beads |
| Semaphores | Resource beads |
| Background services | Pinned beads |
| Process templates | Proto molecules |
| Running processes | Wisp molecules |
| Process termination | Burn (discard) or squash (save state) |
| IPC | Mail beads |

Just as Unix made computer resources manageable through a consistent process model, Gas Town makes AI agent work manageable through a consistent work model.

### 5. Hierarchical Auditability

All work is tracked in a permanent hierarchical ledger:

```
Epic: Implement authentication
├── Task: Design auth flow
│   └── completed by polecat-nux, 2h
├── Task: Implement OAuth provider
│   └── completed by polecat-slit, 4h
├── Task: Add session management
│   └── completed by polecat-nux, 3h
└── Task: Write integration tests
    └── in_progress, polecat-capable
```

This enables:
- **Audit trails**: Who did what, when, and how long it took
- **Observability**: Real-time visibility into swarm progress
- **Attribution**: Clear accountability for work quality
- **Analytics**: Understand where time goes, identify bottlenecks

### 6. Scalable Architecture

Gas Town scales through three mechanisms:

**Federation**: Multiple rigs across machines, coordinated through Beads sync
```
Town (global coordinator)
├── Rig: project-alpha (16 polecats, local)
├── Rig: project-beta (8 polecats, cloud VM)
└── Rig: project-gamma (32 polecats, cluster)
```

**Tiering**: Hot work in active Beads, cold history archived
- Active issues: instant queries
- Recent history: fast retrieval
- Archive: compressed cold storage

**Temporal decay**: Wisps rise and dissipate, digests remain
- Wisp molecules: transient execution traces (the steam)
- Digests: permanent condensed records (the distillate)
- Burn or squash: cleanup happens automatically, leaving clean ledger

## The Agent Hierarchy

### Overseer (Human)
- Sets strategy and priorities
- Reviews and approves output
- Handles escalations
- Operates the system

### Mayor (AI - Town-wide)
- Dispatches work across rigs
- Coordinates cross-project dependencies
- Handles strategic decisions

### Witness (AI - Per-rig)
- Manages polecat lifecycle
- Detects stuck workers
- Handles session cycling

### Refinery (AI - Per-rig)
- Processes merge queue
- Reviews and integrates code
- Maintains branch hygiene

### Polecat (AI - Workers)
- Executes wisp molecules (not just "assigned work")
- Generates summaries and squashes to digest on completion
- Files discovered issues
- Ephemeral like the wisps they execute - spawn, work, disappear

## Quality Through Structure

Gas Town enforces quality through molecules, not prompts:

**Without molecules:**
- Agent is prompted with instructions
- Works from memory
- Loses state on restart
- Quality depends on the prompt

**With molecules:**
- Agent follows persistent workflow
- State survives restarts
- Quality gates are enforced
- Any worker can continue

The difference is like giving someone verbal instructions vs. giving them a checklist. Checklists win.

## Why "IDE of 2026"?

The IDE evolved from text editor → syntax highlighting → autocomplete → AI suggestions.

The next evolution isn't better suggestions - it's **AI as worker, not assistant**.

| 2024 IDE | 2026 IDE (Gas Town) |
|----------|---------------------|
| AI suggests code | AI writes code |
| Human reviews suggestions | Human reviews pull requests |
| AI helps with tasks | AI completes tasks |
| Single agent | Coordinated swarm |
| Context in memory | Context in Beads |
| Manual quality checks | Molecule-enforced gates |

Gas Town is what happens when you treat AI agents as employees, not tools.

## Design Principles

1. **Work is data**: All work state lives in Beads, not agent memory
2. **Molecules over prompts**: Structured workflows beat clever instructions
3. **Crash-resistant by design**: Any agent can continue any work
4. **Hierarchical coordination**: Mayor → Witness → Refinery → Polecat
5. **Quality through structure**: Gates and checks built into molecules
6. **Observable by default**: `bd list` shows the full picture

## Where This Goes

### Now: Coding Agent Orchestrator
- Multi-polecat swarms on software projects
- Molecule-based quality workflows
- Merge queue processing
- Full audit trail

### Next: Knowledge Work Platform
- Support for non-code work (documents, research, analysis)
- Custom molecule libraries
- Enterprise integrations

### Future: Enterprise Cognitive Infrastructure
- Cross-team coordination
- Organization-wide work visibility
- Compliance and governance tooling

---

*"The best tool is invisible. It doesn't help you work - it works."*
