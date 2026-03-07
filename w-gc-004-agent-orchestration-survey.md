# Survey: Agent Orchestration Frameworks vs Gas City

**Wasteland item**: w-gc-004 — "Survey existing agent orchestration frameworks"
**Date**: 2026-03-07
**Author**: zhora (Gas Town crew)

---

## Executive Summary

This survey compares seven prominent multi-agent orchestration frameworks against Gas City's approach. Gas City occupies a unique position: it uses OS-level process isolation (tmux), declarative TOML configs, formula-driven task orchestration, and a real database (Dolt) as shared state. Most frameworks operate within a single Python process, define roles in code or prompts, and share state through in-memory objects. Gas City's approach is more operationally robust but less portable; the frameworks are more accessible but more fragile at scale.

---

## 1. AutoGen (Microsoft)

**Repository**: [microsoft/autogen](https://github.com/microsoft/autogen)
**Status**: Maintenance mode as of 2026; superseded by Microsoft Agent Framework

### Role Definition
Roles are defined **in code** via Python classes. The base abstraction is `ConversableAgent`, with specialized subclasses `AssistantAgent` (LLM-driven) and `UserProxyAgent` (human proxy with code execution). Role behavior is set through system prompts passed as constructor arguments and optional registered reply functions.

### Task Assignment & Coordination
Four conversation patterns handle coordination:
- **Two-agent chat**: Direct peer-to-peer dialogue
- **Sequential chat**: Chained pairwise conversations with summary carry-over
- **Nested chat**: Complex workflows encapsulated inside a single agent, enabling hierarchical decomposition
- **Group chat**: Centralized `GroupChatManager` (LLM-powered) selects next speaker via round-robin, random, manual, or auto (LLM-decided) strategies

### Context/Memory Sharing
Agents share context through **message histories** passed between conversations. Sequential chats carry summaries forward. Group chats maintain a shared message thread that all participants read and write to. No persistent cross-session memory by default; state lives in Python objects within a single process.

### Tool Access
Tools are Python functions registered on agents. An agent can call tools via LLM function-calling. `UserProxyAgent` can execute generated code in a sandboxed environment. Tools are agent-scoped — each agent declares what it can use.

### Strengths vs Gas City
- Rich conversation pattern library (group chat, nested, sequential) — Gas City's inter-agent communication is more ad-hoc (gt mail, gt nudge)
- LLM-driven speaker selection in group chat is elegant for collaborative reasoning
- Human-in-the-loop modes (NEVER/TERMINATE/ALWAYS) are well-designed

### Gaps vs Gas City
- **No process isolation**: All agents run in one Python process. A crash kills everything. Gas City's tmux isolation means one agent crashing doesn't affect others.
- **No persistent shared state**: Memory dies with the process. Gas City's Dolt database survives agent restarts, session deaths, even machine reboots.
- **No declarative role configs**: Roles are code, not config files. Gas City's TOML configs allow non-programmers to define roles.
- **No built-in work queue**: No equivalent to Gas City's beads/formula system for persistent task tracking.

---

## 2. CrewAI

**Repository**: [crewAIInc/crewAI](https://github.com/crewAIInc/crewAI)
**Status**: Active development; production-ready with CrewAI Flows

### Role Definition
Roles are defined **in code** with three key attributes: `role` (job title), `goal` (objective), and `backstory` (context/expertise). These are natural-language strings. Optional parameters include LLM selection, reasoning mode, memory enablement, and tool assignment.

```python
Agent(
    role="Senior Data Analyst",
    goal="Extract actionable insights from sales data",
    backstory="You have 15 years of experience in retail analytics..."
)
```

### Task Assignment & Coordination
Tasks are explicit objects with descriptions, expected outputs, and assigned agents. Two process types:
- **Sequential**: Tasks execute in order, each agent receiving the previous output
- **Hierarchical**: A manager agent dynamically delegates tasks to specialists and tracks outcomes

CrewAI supports **delegation** — an agent can autonomously decide to hand off part of its task to another agent it deems more qualified, creating sub-tasks dynamically.

### Context/Memory Sharing
CrewAI has the most sophisticated memory system among pure-Python frameworks:
- **Short-term memory**: ChromaDB-backed RAG for current session context
- **Long-term memory**: SQLite3 for persisting task results across sessions
- **Entity memory**: RAG-based capture of people, places, concepts
- **Memory slices**: Read access across multiple memory branches without write access to shared areas

After each task, the crew extracts discrete facts from outputs and stores them. Before each task, relevant context is recalled and injected into the prompt. All agents share crew memory unless overridden.

### Tool Access
Tools are assigned per-agent at definition time. CrewAI includes built-in tools and supports custom tools via a decorator pattern. Agents can also discover and use tools from other agents through delegation.

### Strengths vs Gas City
- **Memory architecture is impressive**: Short-term + long-term + entity memory with scoped slices is more structured than Gas City's approach (Dolt tables + MEMORY.md files)
- **Delegation model**: Agents autonomously deciding to delegate is a pattern Gas City lacks — Gas City delegation is human/formula-driven
- **Backstory concept**: Natural-language role context is more expressive than TOML fields

### Gaps vs Gas City
- **Single-process, single-machine**: No OS-level isolation. No equivalent to tmux windows.
- **No real database**: SQLite3 for long-term memory is fragile compared to Dolt with versioning, diffing, and merge semantics.
- **No work queue persistence**: Tasks exist only within a crew execution. Gas City's beads persist independently of any agent's lifecycle.
- **No git-like state history**: Dolt gives Gas City full version history of all shared state. CrewAI memory has no audit trail.

---

## 3. LangGraph (LangChain)

**Repository**: [langchain-ai/langgraph](https://github.com/langchain-ai/langgraph)
**Status**: Active development; production-focused with LangGraph Platform

### Role Definition
Agents are defined **as graph nodes** — Python functions that receive state, perform computation, and return updated state. There is no explicit "role" abstraction; each node's behavior is determined by its implementation. Roles emerge from the graph topology rather than from declarations.

### Task Assignment & Coordination
Coordination is modeled as a **directed graph** (DAG or cyclic):
- **Nodes**: Agent functions, tool calls, or decision points
- **Edges**: Static connections between nodes
- **Conditional edges**: Dynamic routing based on state values or agent outputs
- **Parallel execution**: Multiple nodes processing the same state simultaneously, with results merging downstream

This is the most explicit control-flow model of any framework — you draw the workflow as a graph. It excels at deterministic, repeatable processes but requires more upfront design.

### Context/Memory Sharing
State is centralized in a `StateGraph` object — a typed dictionary that all nodes read from and write to. Two memory layers:
- **Short-term (thread-level)**: Checkpointers save full graph state at each step. Resume conversations exactly where they left off. Backends: SQLite (dev), Redis (production), Postgres.
- **Long-term (cross-thread)**: Persistent stores scoped to custom namespaces, shared across all threads.

Checkpointing is a first-class concern — the framework was designed around it.

### Tool Access
Tools are typically LangChain `Tool` objects bound to nodes. A node can invoke tools via LLM function-calling or direct code. Tool results flow back through the state graph like any other data.

### Strengths vs Gas City
- **Explicit control flow**: The graph model makes workflows visible, debuggable, and reproducible. Gas City's formula system is similar in spirit but less formally structured.
- **Checkpointing**: First-class state persistence with pluggable backends. Gas City achieves this via Dolt, but LangGraph's integration is tighter.
- **Conditional routing**: Dynamic branching based on state is cleaner than Gas City's hook-based dispatch.
- **Parallel execution**: Native support for running nodes concurrently. Gas City achieves parallelism through multiple tmux panes, which is more heavyweight but more isolated.

### Gaps vs Gas City
- **No process isolation**: Graph executes in one process. No crash isolation.
- **No persistent task identity**: Work items don't have independent lifecycle. No equivalent to beads.
- **Code-only definition**: No declarative config format. Graphs must be programmed.
- **No operational tooling**: No equivalent to `gt dolt status`, `bd list`, `gt escalate` — the operational observability layer that Gas City provides.

---

## 4. OpenAI Swarm

**Repository**: [openai/swarm](https://github.com/openai/swarm)
**Status**: Educational/deprecated; superseded by OpenAI Agents SDK

### Role Definition
Agents are defined **in code** with a name, instructions (system prompt), functions (tools), and optional model override. The instructions string IS the role definition — pure natural language.

```python
Agent(
    name="Triage Agent",
    instructions="Route customer queries to the appropriate specialist.",
    functions=[transfer_to_sales, transfer_to_support]
)
```

### Task Assignment & Coordination
Two primitives:
- **Routines**: Natural-language instruction lists paired with tool functions. An agent follows its routine until it needs to hand off.
- **Handoffs**: An agent returns another Agent object to transfer the conversation. The new agent takes over with full conversation history. Like a phone transfer.

No centralized orchestrator. Coordination emerges from handoff chains. This is the most minimalist multi-agent model.

### Context/Memory Sharing
**Stateless by design.** No state is retained between API calls. The full conversation history is passed on each call. Context variables (a simple dict) can be threaded through handoffs for lightweight state.

### Tool Access
Tools are plain Python functions registered per-agent. The function signature becomes the tool schema automatically. Handoff functions are just tools that return a different Agent.

### Strengths vs Gas City
- **Radical simplicity**: Three concepts (agents, routines, handoffs) vs Gas City's larger surface area. Easier to understand and debug.
- **Handoff model**: Clean metaphor for transferring work between specialists. Gas City could borrow this for agent-to-agent task transfers.
- **Transparency**: Stateless = fully observable. Every call is self-contained.

### Gaps vs Gas City
- **No persistence whatsoever**: Stateless means nothing survives a crash. The antithesis of Gas City's durability-first design.
- **No parallel execution**: Strictly sequential, one-agent-at-a-time. Gas City runs many agents concurrently.
- **No task tracking**: No work queue, no issue lifecycle. Purely conversational.
- **OpenAI-only**: Locked to OpenAI models. Gas City is model-agnostic.
- **Educational, not production**: OpenAI explicitly positioned this as a teaching tool.

---

## 5. Semantic Kernel (Microsoft)

**Repository**: [microsoft/semantic-kernel](https://github.com/microsoft/semantic-kernel)
**Status**: Active; core of Microsoft Agent Framework alongside AutoGen

### Role Definition
Agents are defined **in code** with instructions (system prompt) and optional plugins. The Agent Framework layer provides `ChatCompletionAgent`, `OpenAIAssistantAgent`, and `AzureAIAgent` types. Roles are established through instruction strings, not config files.

Semantic Kernel also introduces the concept of **personas** — reusable agent configurations that can be shared and composed.

### Task Assignment & Coordination
The Agent Orchestration framework provides five pre-built patterns:
- **Sequential**: Agents execute in order
- **Concurrent**: Agents run in parallel
- **Handoff**: Agents transfer conversations (inspired by Swarm)
- **Group Chat**: Multi-agent discussion with speaker selection
- **Magentic**: Advanced orchestration for complex scenarios

These patterns are composable — you can nest a group chat inside a sequential flow.

### Context/Memory Sharing
Agents share context through the **Kernel** — a central object that holds services, plugins, and state. Chat history is passed through `AgentGroupChat` or thread objects. Semantic Kernel integrates with external memory stores but doesn't prescribe a specific persistence model.

### Tool Access
**Plugins** are the core tool abstraction — collections of functions (called "kernel functions") that can be native code or LLM prompts. Plugins are registered on the Kernel and made available to agents. Function calling follows OpenAI's pattern.

Planners (now deprecated in favor of direct function-calling) previously used LLM reasoning to select and sequence plugin calls.

### Strengths vs Gas City
- **Enterprise integration**: Deep Azure/M365 integration. Production-grade from day one.
- **Plugin ecosystem**: Rich, composable plugin model. Gas City's hook system is conceptually similar but less formalized.
- **Multi-language**: C#, Python, Java. Gas City is Go + shell scripts.
- **Composable orchestration patterns**: Pre-built patterns that can be nested are elegant.

### Gaps vs Gas City
- **No OS-level isolation**: In-process execution only.
- **Microsoft ecosystem coupling**: Heavy Azure dependency for production features. Gas City runs anywhere with tmux + Dolt.
- **No persistent work tracking**: No beads/issues equivalent.
- **No declarative role config**: Code-only agent definitions.

---

## 6. MetaGPT

**Repository**: [FoundationAgents/MetaGPT](https://github.com/FoundationAgents/MetaGPT)
**Status**: Active; launched MGX platform (Feb 2025)

### Role Definition
Roles are defined **in code as classes** inheriting from a `Role` base class. Each role specifies:
- Name and profile (e.g., "Product Manager", "Architect")
- Actions it can perform (class methods)
- What message types it watches for (subscription filters)
- What it publishes

MetaGPT is the most **SOP-driven** framework: roles mirror real software company positions with standardized procedures encoded into prompt sequences. The philosophy is "Code = SOP(Team)".

Five built-in roles: Product Manager, Architect, Project Manager, Engineer, QA Engineer. Each follows domain-specific SOPs (e.g., the PM produces PRDs, the Architect produces system designs).

### Task Assignment & Coordination
Coordination follows an **assembly line paradigm**:
1. A high-level task enters the system
2. The Product Manager decomposes it into requirements
3. Each subsequent role processes the structured output of the previous role
4. SOPs ensure each step produces well-defined artifacts

This is the most structured coordination model — it mirrors waterfall software development rather than allowing freeform agent interaction.

### Context/Memory Sharing
MetaGPT uses a **global shared message pool** with publish-subscribe semantics:
- All agents publish structured outputs (documents, diagrams, code) to the pool
- Agents subscribe to message types relevant to their role
- Any agent can retrieve information directly from the pool without asking other agents
- Subscription filters prevent information overload — each agent only sees what it subscribed to

The message pool acts as both communication channel and shared memory.

### Tool Access
Actions (tools) are defined as methods on Role classes. Each role has a fixed set of actions it can perform. Tools are tightly coupled to roles — an Engineer can code, a QA Engineer can test, but not vice versa. Less flexible than other frameworks but more disciplined.

### Strengths vs Gas City
- **SOP encoding**: Structured procedures reduce hallucination cascading. Gas City's formulas serve a similar purpose but are less granular.
- **Publish-subscribe message pool**: Efficient, decoupled communication. Gas City's gt mail is more like point-to-point messaging.
- **Structured artifacts**: Each role produces typed, validated outputs. Gas City's beads have structure but agent outputs are freeform.
- **Role subscription filters**: Agents only see relevant messages. Gas City agents currently see all mail sent to them regardless of type.

### Gaps vs Gas City
- **Rigid role structure**: Five fixed software-company roles. Gas City supports arbitrary role definitions via TOML.
- **No process isolation**: Single Python process.
- **No persistent state store**: Message pool is ephemeral. Gas City's Dolt persists everything.
- **Software-development focused**: Not generalizable to arbitrary multi-agent workflows without significant customization.
- **No operational tooling**: No health monitoring, crash recovery, or escalation protocols.

---

## 7. Agency Swarm

**Repository**: [VRSEN/agency-swarm](https://github.com/VRSEN/agency-swarm)
**Status**: Active development

### Role Definition
Agents are defined **in code** with instructions, tools, and optional model config. Built on top of OpenAI's Assistants API, so agents are persistent OpenAI Assistant objects with server-side state. Agent state is managed in a `settings.json` file.

### Task Assignment & Coordination
Communication flows are defined using a **directional operator** (`>`):
```python
agency = Agency([
    ceo,           # Entry point
    [ceo, dev],    # CEO can talk to Developer
    [ceo, va],     # CEO can talk to VA
    [dev, va]      # Developer can talk to VA
])
```

The `>` operator defines who can initiate communication with whom. This creates an explicit, directional communication graph — not all agents can talk to all others.

### Context/Memory Sharing
State is managed through OpenAI's Assistants API threads. Each conversation pair maintains its own thread with full history. Cross-agent state sharing happens through the `SendMessage` tool — agents explicitly pass information by messaging each other. A `settings.json` file tracks assistant IDs and thread state locally.

### Tool Access
Tools are the primary abstraction. Agents are essentially collections of tools. Communication itself is a tool (`SendMessage`). Custom tools are defined as Pydantic models. Agency Swarm supports both synchronous and asynchronous execution modes (`threading` and `tools_threading`).

### Strengths vs Gas City
- **Explicit communication topology**: The directional flow definition is clean and auditable. Gas City's communication is less formally constrained.
- **Tool-first philosophy**: Everything is a tool, including inter-agent communication. Unified mental model.
- **Persistent assistants**: OpenAI Assistants API provides server-side state persistence (within OpenAI's infrastructure).
- **Async modes**: Built-in threading for parallel agent and tool execution.

### Gaps vs Gas City
- **OpenAI lock-in**: Completely dependent on OpenAI Assistants API. Gas City is provider-agnostic.
- **No OS-level isolation**: All agents in one process.
- **No self-hosted state**: State lives on OpenAI's servers. Gas City owns its data in Dolt.
- **No work queue**: No persistent task lifecycle management.
- **No operational tooling**: No health checks, crash recovery, or escalation.

---

## Cross-Framework Comparison Matrix

| Dimension | AutoGen | CrewAI | LangGraph | Swarm | Semantic Kernel | MetaGPT | Agency Swarm | **Gas City** |
|---|---|---|---|---|---|---|---|---|
| **Role definition** | Code (classes) | Code (strings) | Code (functions) | Code (strings) | Code (classes) | Code (classes+SOP) | Code (classes) | **TOML configs** |
| **Task coordination** | Conversation patterns | Sequential/Hierarchical | Graph (DAG) | Handoff chains | 5 orchestration patterns | Assembly line + SOP | Directional flows | **Formula + hooks** |
| **Memory persistence** | None (in-process) | ChromaDB + SQLite | Checkpointers (Redis/SQL) | None (stateless) | External stores | Message pool (ephemeral) | OpenAI threads | **Dolt (versioned DB)** |
| **Process isolation** | None | None | None | None | None | None | None | **tmux (OS-level)** |
| **Crash recovery** | None | None | Checkpoint resume | None | None | None | None | **Daemon + backoff** |
| **Work queue** | None | None | None | None | None | None | None | **Beads (persistent)** |
| **State versioning** | None | None | None | None | None | None | None | **Dolt (git-like)** |
| **Tool model** | Registered functions | Agent-scoped + delegation | LangChain tools | Python functions | Plugins (kernel functions) | Role-bound actions | Pydantic models | **Shell commands + hooks** |
| **Multi-machine** | No | No | LangGraph Platform (cloud) | No | Azure (cloud) | No | No | **Yes (tmux + SSH)** |
| **Model agnostic** | Yes | Yes | Yes | No (OpenAI) | Partial (Azure-favored) | Partial | No (OpenAI) | **Yes** |
| **Operational tooling** | None | None | LangSmith (tracing) | None | Azure Monitor | None | None | **gt CLI suite** |

---

## Patterns Gas City Could Borrow

### 1. From CrewAI: Structured Memory Tiers
CrewAI's three-tier memory (short-term RAG, long-term SQL, entity memory) with scoped slices is more structured than Gas City's current approach of Dolt tables + MEMORY.md files. Gas City could formalize memory into tiers: session memory (ephemeral), work memory (bead-scoped), and institutional memory (cross-session, cross-agent). The scoped-slice pattern — giving agents read access to multiple memory branches without write access — maps well to Dolt's branch semantics.

### 2. From AutoGen: Conversation Patterns Library
AutoGen's four conversation patterns (two-agent, sequential, nested, group) are well-studied and cover most multi-agent interaction needs. Gas City's gt mail and gt nudge are primitives; building higher-level patterns on top (e.g., a "group review" pattern for the 5-agent PR review) would reduce boilerplate.

### 3. From MetaGPT: Publish-Subscribe Message Pool
MetaGPT's global message pool with subscription filters is more efficient than point-to-point mail for broadcast scenarios. Gas City agents currently need to be explicitly addressed. A pub-sub layer where agents subscribe to message types (e.g., "all PR events", "all escalations") would reduce coordination overhead.

### 4. From LangGraph: Explicit Workflow Graphs
LangGraph's graph model makes workflows visible and debuggable. Gas City's formula system is conceptually similar but not visualizable. Rendering formula-driven task flows as directed graphs would aid debugging and onboarding.

### 5. From Agency Swarm: Directional Communication Constraints
Agency Swarm's `>` operator for defining who can talk to whom is a simple, powerful constraint. Gas City currently allows any agent to mail any other. Defining explicit communication topologies per-rig could reduce noise and enforce organizational structure.

### 6. From OpenAI Swarm: Handoff Semantics
Swarm's clean handoff model — one agent transferring a conversation with full context to another — is a pattern Gas City could formalize. Currently, task handoffs between agents use ad-hoc mail. A first-class `gt handoff` with structured context transfer would be valuable.

### 7. From Semantic Kernel: Composable Orchestration Patterns
Semantic Kernel's five pre-built, nestable orchestration patterns (Sequential, Concurrent, Handoff, Group Chat, Magentic) could inspire a pattern library for Gas City formulas.

---

## Gaps Gas City Fills That Others Don't

### 1. OS-Level Process Isolation
**No other framework provides this.** Every framework runs agents in a single Python process. Gas City's tmux-based isolation means:
- One agent crashing doesn't take down others
- Agents can be independently restarted, inspected, or killed
- Resource usage per agent is visible via standard OS tools
- Agents can run different languages, runtimes, or even different LLM providers

This is Gas City's most distinctive architectural advantage.

### 2. Persistent, Versioned Shared State (Dolt)
Dolt provides git-like semantics for the shared database: branching, merging, diffing, and full history. No other framework has anything comparable. LangGraph's checkpointing is the closest, but it captures snapshots of a single graph execution, not a shared knowledge base with multi-writer merge semantics.

### 3. Persistent Work Queue with Independent Lifecycle
Beads (issues/tasks) persist independently of any agent's lifecycle. They survive session death, agent crashes, and even full system restarts. Every other framework's "tasks" exist only during execution. This is critical for long-running, multi-session work.

### 4. Crash Recovery with Backoff
Gas City's daemon auto-restarts crashed agents with exponential backoff. No other framework has built-in crash recovery — they assume the process stays alive.

### 5. Operational Tooling
The `gt` CLI provides health monitoring (`gt dolt status`), cleanup (`gt dolt cleanup`), escalation (`gt escalate`), communication (`gt mail`, `gt nudge`), and work management (`bd list`, `bd ready`, `bd close`). Other frameworks provide, at most, tracing/observability (LangSmith for LangGraph).

### 6. Declarative Role Configuration
TOML-based role configs allow role definitions to be version-controlled, diffed, and managed without code changes. Every other framework requires writing code to define agent roles.

### 7. Multi-Machine Distribution
Gas City agents can run across machines via tmux + SSH. Only cloud-hosted solutions (LangGraph Platform, Azure-based Semantic Kernel) offer multi-machine support, and those require vendor infrastructure.

---

## Conclusion

The multi-agent framework landscape optimizes for **developer accessibility** (easy to set up, Python-native, single-process) at the cost of **operational robustness** (no isolation, no persistent state, no crash recovery). Gas City makes the opposite trade: it's harder to set up but more durable, observable, and production-ready for long-running autonomous agent systems.

The most valuable patterns to borrow are CrewAI's structured memory tiers, MetaGPT's publish-subscribe messaging, LangGraph's explicit workflow graphs, and Agency Swarm's directional communication constraints. These could be layered onto Gas City's existing infrastructure without sacrificing its core advantages of process isolation, persistent versioned state, and operational tooling.

---

## Sources

- [AutoGen Multi-Agent Conversation Framework](https://microsoft.github.io/autogen/0.2/docs/Use-Cases/agent_chat/)
- [AutoGen Group Chat Design Pattern](https://microsoft.github.io/autogen/stable//user-guide/core-user-guide/design-patterns/group-chat.html)
- [AutoGen to Microsoft Agent Framework Migration](https://learn.microsoft.com/en-us/agent-framework/migration-guide/from-autogen/)
- [CrewAI Agents Documentation](https://docs.crewai.com/en/concepts/agents)
- [CrewAI Memory Documentation](https://docs.crewai.com/en/concepts/memory)
- [CrewAI Framework 2025 Review](https://latenode.com/blog/ai-frameworks-technical-infrastructure/crewai-framework/crewai-framework-2025-complete-review-of-the-open-source-multi-agent-ai-platform)
- [LangGraph Multi-Agent Orchestration Guide 2025](https://latenode.com/blog/ai-frameworks-technical-infrastructure/langgraph-multi-agent-orchestration/langgraph-multi-agent-orchestration-complete-framework-guide-architecture-analysis-2025)
- [LangGraph Memory Documentation](https://docs.langchain.com/oss/python/langgraph/add-memory)
- [LangGraph Checkpointing Best Practices 2025](https://sparkco.ai/blog/mastering-langgraph-checkpointing-best-practices-for-2025)
- [OpenAI Swarm Repository](https://github.com/openai/swarm)
- [OpenAI Orchestrating Agents Cookbook](https://cookbook.openai.com/examples/orchestrating_agents)
- [Semantic Kernel Agent Framework](https://learn.microsoft.com/en-us/semantic-kernel/frameworks/agent/)
- [Semantic Kernel Agent Architecture](https://learn.microsoft.com/en-us/semantic-kernel/frameworks/agent/agent-architecture)
- [MetaGPT Paper](https://arxiv.org/abs/2308.00352)
- [MetaGPT Repository](https://github.com/FoundationAgents/MetaGPT)
- [What is MetaGPT — IBM](https://www.ibm.com/think/topics/metagpt)
- [Agency Swarm Repository](https://github.com/VRSEN/agency-swarm)
- [Agency Swarm Communication Flows](https://vrsen.github.io/agency-swarm/advanced-usage/communication_flows/)
- [Open Source AI Agent Frameworks Compared 2026](https://openagents.org/blog/posts/2026-02-23-open-source-ai-agent-frameworks-compared)
- [Top 5 Agentic AI Frameworks 2026](https://futureagi.substack.com/p/top-5-agentic-ai-frameworks-to-watch)
