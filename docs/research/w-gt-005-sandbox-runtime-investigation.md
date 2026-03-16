# Sandbox Runtime Investigation for Wasteland Commercial Work

**Wasteland Item:** w-gt-005
**Type:** Research
**Priority:** P1
**Author:** gastown/crew/zhora (dreadpiraterobertz)
**Date:** 2026-03-15

## Executive Summary

Gas Town needs pluggable sandbox isolation for untrusted Wasteland work before
Phase 2 commercial tasks can proceed. This investigation evaluates five runtime
candidates — Firecracker, gVisor, Wasm/WASI, macOS sandbox-exec, and Daytona —
against Gas Town's specific requirements.

**Recommendation:** A two-tier strategy:
1. **Local development (macOS):** `sandbox-exec` with SBPL profiles (already
   prototyped in the codebase) — deploy immediately
2. **Production/CI (Linux):** Daytona sandboxes with Docker microVMs — best
   agent-native integration, sub-200ms provisioning, proven at scale

Firecracker and gVisor are strong Linux-only options but require more
integration work. Wasm/WASI is not viable for running Node.js CLI tools today.

## Requirements

| Requirement | Description | Priority |
|-------------|-------------|----------|
| **R1: Filesystem isolation** | Read-only base, writable project dir only | Must-have |
| **R2: Network restriction** | Loopback-only or proxy-only egress via mTLS | Must-have |
| **R3: Process isolation** | Can't see/signal other agents' processes | Must-have |
| **R4: Fast startup** | <5s provisioning, ideally <1s | Must-have |
| **R5: macOS support** | Primary dev environment (Apple Silicon) | Must-have |
| **R6: Linux support** | Production and CI environments | Must-have |
| **R7: Node.js compatibility** | Claude Code is a Node.js CLI tool | Must-have |
| **R8: Git/Dolt compatibility** | Agents need git and dolt CLI tools | Must-have |
| **R9: Pluggable backends** | Swap isolation strategy without code changes | Nice-to-have |

## Existing Infrastructure

The gastown codebase already has significant sandbox infrastructure:

### mTLS Proxy Server (`internal/proxy/`)
- **Fully implemented and tested** — exec relay, git smart-HTTP, CA management
- Per-client rate limiting, exec semaphore, command allowlisting
- Per-polecat leaf certificates (24h TTL default)
- Denylist for certificate revocation on session end
- Hardcoded command allowlist: `gt:prime,hook,done,mail,nudge,...; bd:create,update,close,...`

### ExecWrapper (`internal/config/types.go`)
- `RuntimeConfig.ExecWrapper []string` field ready for use
- Example: `["exitbox", "run", "--profile=gastown-polecat", "--"]`
- Inserts between env vars and agent binary in startup command

### Sandbox Integration Tests (`internal/config/sandbox_integration_test.go`)
- Full SBPL policy enforcement tests
- Validates: file write restrictions, loopback-only network, DNS exfiltration blocking

### Design Document (`docs/design/sandboxed-polecat-execution.md`)
- Comprehensive exitbox + daytona dual-backend architecture
- Implementation plan with G1-G7 change groups
- Status: Proposal (not yet wired into session manager)

### Wanted Table Schema (`internal/doltserver/wl_commons.go`)
- `sandbox_required TINYINT(1)` — boolean gate
- `sandbox_scope JSON` — permissions (e.g., `{"fs": ["read"], "net": ["none"]}`)
- `sandbox_min_tier VARCHAR(32)` — minimum trust level
- **Currently unpopulated** — no dispatch enforcement yet

## Candidate Analysis

### 1. Firecracker (AWS microVM)

**What it is:** Lightweight virtual machine monitor (VMM) built on KVM, written
in Rust. Creates microVMs with minimal device model. Used by AWS Lambda and
Fargate.

**Strengths:**
- Hardware-level isolation (KVM-backed)
- Sub-125ms startup, <5 MiB memory footprint
- Battle-tested at massive scale (AWS Lambda)
- Minimal attack surface (reduced device model vs QEMU)

**Weaknesses:**
- **Linux-only** — requires KVM, no native macOS support
- On macOS: must run inside a Linux VM (nested virtualization on Apple Silicon)
- Requires custom rootfs/kernel image management
- No native container image support (must build from scratch or use tools like
  ignite/firectl)

**Feasibility for Gas Town:**

| Requirement | Met? | Notes |
|-------------|------|-------|
| R1: FS isolation | Yes | Full VM boundary |
| R2: Network | Yes | Virtual network device, configurable |
| R3: Process | Yes | VM boundary = complete isolation |
| R4: Startup | Yes | <125ms |
| R5: macOS | **No** | Requires Linux VM wrapper |
| R6: Linux | Yes | Native KVM |
| R7: Node.js | Yes | Full Linux guest |
| R8: Git/Dolt | Yes | Full Linux guest |
| R9: Pluggable | Partial | Significant integration needed |

**Verdict:** Excellent isolation on Linux but the macOS gap is a dealbreaker for
local development. Would require maintaining two completely different isolation
stacks.

### 2. gVisor (Google Application Kernel)

**What it is:** User-space kernel that intercepts and reimplements Linux system
calls. Runs as an OCI-compatible container runtime (`runsc`). Used by Google
Cloud Run and GKE Sandbox.

**Strengths:**
- Syscall-level isolation without VM overhead
- OCI-compatible — works with Docker/containerd
- No hardware virtualization required
- Root filesystem overlay improves I/O performance
- Battle-tested at Google scale

**Weaknesses:**
- **Linux-only** (x86_64 and ARM64) — no macOS support
- Incomplete syscall coverage can break some applications
- Performance overhead on filesystem-heavy workloads (relevant for git)
- Not all node_modules work perfectly under gVisor's syscall emulation

**Feasibility for Gas Town:**

| Requirement | Met? | Notes |
|-------------|------|-------|
| R1: FS isolation | Yes | Container + OCI runtime |
| R2: Network | Yes | Network namespace isolation |
| R3: Process | Yes | PID namespace + syscall filter |
| R4: Startup | Yes | Container startup speed |
| R5: macOS | **No** | Linux-only |
| R6: Linux | Yes | Native |
| R7: Node.js | Mostly | Some edge cases with syscall gaps |
| R8: Git/Dolt | Mostly | Filesystem overhead may impact git perf |
| R9: Pluggable | Yes | Standard OCI runtime swap |

**Verdict:** Strong Linux option with lighter overhead than Firecracker, but
same macOS gap. Syscall emulation gaps could cause subtle issues with Node.js
ecosystem.

### 3. Wasm/WASI (WebAssembly System Interface)

**What it is:** Portable binary format with a capability-based security model.
WASI provides standardized system interfaces (filesystem, network, clocks).
Runtimes: Wasmtime, Wasmer, WasmEdge.

**Strengths:**
- Cross-platform by design (macOS, Linux, Windows)
- Capability-based security — grant only what you explicitly allow
- Near-native startup times
- WASI 0.2 stable, WASI 0.3 experimental (expected 1.0 by late 2026/early 2027)
- Used in production by Cloudflare Workers, Shopify

**Weaknesses:**
- **Cannot run Node.js CLI tools** — Claude Code, git, and dolt are native
  binaries, not Wasm modules
- WASI targets compiled-to-Wasm code (Rust, C, Go), not arbitrary executables
- Node.js has WASI *hosting* support (run Wasm modules in Node) but Node.js
  itself cannot run *inside* Wasm
- Ecosystem immaturity for complex CLI toolchains

**Feasibility for Gas Town:**

| Requirement | Met? | Notes |
|-------------|------|-------|
| R1: FS isolation | Yes | Capability-based preopens |
| R2: Network | Yes | Capability-based sockets |
| R3: Process | Yes | No process spawning by default |
| R4: Startup | Yes | Near-native |
| R5: macOS | Yes | Cross-platform |
| R6: Linux | Yes | Cross-platform |
| R7: Node.js | **No** | Cannot run Node.js inside Wasm |
| R8: Git/Dolt | **No** | Cannot run native binaries |
| R9: Pluggable | N/A | Fundamental incompatibility |

**Verdict:** Not viable. The fundamental issue is that Wasm cannot run arbitrary
native binaries. Claude Code (Node.js), git, and dolt must run as native
processes. Wasm would only work if the entire agent stack were rewritten as Wasm
modules, which is not practical.

### 4. macOS sandbox-exec (Seatbelt/SBPL)

**What it is:** macOS kernel-enforced sandbox using Scheme-based policy
language (SBPL). Used in production by Claude Code (Anthropic), OpenAI Codex,
Bazel, and Chromium. Despite Apple's deprecation warnings, fully functional
on macOS Sequoia 15.x.

**Strengths:**
- **Already prototyped in the codebase** — integration tests pass
- Kernel-level enforcement (cannot bypass from user space)
- Zero startup overhead (process-level, no VM)
- Fine-grained policies: filesystem paths, network (loopback-only), IPC, mach services
- Proven in production by major AI coding tools
- Native Apple Silicon support

**Weaknesses:**
- **macOS-only** — no Linux equivalent
- Deprecated by Apple (no new features, but not removed)
- SBPL is undocumented by Apple (community-maintained knowledge)
- No OCI container integration

**Feasibility for Gas Town:**

| Requirement | Met? | Notes |
|-------------|------|-------|
| R1: FS isolation | Yes | Literal/subpath/regex file rules |
| R2: Network | Yes | Loopback-only enforcement |
| R3: Process | Partial | Process exec restrictions but not full PID isolation |
| R4: Startup | Yes | Zero overhead |
| R5: macOS | Yes | Native |
| R6: Linux | **No** | macOS-only |
| R7: Node.js | Yes | Proven by Claude Code itself |
| R8: Git/Dolt | Yes | Standard process execution |
| R9: Pluggable | Yes | ExecWrapper is ready |

**Verdict:** Best option for local macOS development. Already integrated into
the codebase via ExecWrapper and tested. Deploy immediately as the local tier.

### 5. Daytona (Cloud Sandbox Platform)

**What it is:** Purpose-built infrastructure for running AI-generated code in
isolated environments. Pivoted in February 2025 from dev environments to AI
agent sandboxing. Provides SDK-driven sandbox lifecycle with Docker containers
or microVMs.

**Strengths:**
- **Sub-200ms provisioning** (sub-90ms in optimized configs)
- Built specifically for AI agent workflows
- Declarative image builder — agents define deps, Daytona builds Docker image
- Full API for sandbox lifecycle (create, start, stop, delete)
- MCP integration (Model Context Protocol)
- Supports Kata Containers and Sysbox for enhanced isolation
- Claude Code is a supported agent runtime
- **Already has proxy integration in gastown codebase** (DaytonaConfig designed)

**Weaknesses:**
- Cloud-hosted — requires network connectivity to Daytona service
- Cost per sandbox for commercial workloads
- Default isolation is standard Docker (not microVM unless configured)
- Vendor dependency
- Self-hosted option exists but requires infrastructure

**Feasibility for Gas Town:**

| Requirement | Met? | Notes |
|-------------|------|-------|
| R1: FS isolation | Yes | Container boundary + volumes |
| R2: Network | Yes | Network policies, proxy support |
| R3: Process | Yes | Container PID namespace |
| R4: Startup | Yes | Sub-200ms |
| R5: macOS | Yes* | Cloud-hosted, client runs anywhere |
| R6: Linux | Yes | Native container host |
| R7: Node.js | Yes | Full container environment |
| R8: Git/Dolt | Yes | Full container + mTLS proxy |
| R9: Pluggable | Yes | DaytonaConfig already designed |

**Verdict:** Best option for production. Agent-native design, proven at scale,
and already partially integrated into the codebase. The mTLS proxy server in
`internal/proxy/` was designed for this exact use case.

### 6. Docker Sandboxes (Bonus — New Entrant)

**What it is:** Docker's native sandbox feature (shipped late 2025/early 2026)
for running AI coding agents in isolated microVMs with private Docker daemons.

**Strengths:**
- Lightweight microVM isolation (not just containers)
- Native Docker ecosystem integration
- Supports Claude Code, Codex, Copilot, Gemini out of the box
- Part of Agentic AI Foundation (with Anthropic MCP)
- Local execution — no cloud dependency

**Weaknesses:**
- Requires Docker Desktop
- Relatively new (less battle-tested than Daytona at scale)
- macOS: runs via Docker Desktop's Linux VM backend

**Feasibility:** Strong contender for local Linux development and CI. Worth
monitoring as it matures, especially given Anthropic's involvement in the
Agentic AI Foundation.

## Comparison Matrix

| Criterion | Firecracker | gVisor | Wasm/WASI | sandbox-exec | Daytona | Docker Sandbox |
|-----------|-------------|--------|-----------|--------------|---------|----------------|
| **FS Isolation** | Full VM | Container | Capabilities | Kernel SBPL | Container | microVM |
| **Network** | Full VM | Namespace | Capabilities | Kernel | Policy | microVM |
| **Process** | Full VM | PID NS | No spawn | Partial | PID NS | microVM |
| **Startup** | <125ms | ~1s | ~10ms | ~0ms | <200ms | ~2s |
| **macOS** | No | No | Yes | **Yes** | Yes* | Via Docker |
| **Linux** | **Yes** | **Yes** | Yes | No | **Yes** | **Yes** |
| **Node.js** | Yes | Mostly | **No** | Yes | Yes | Yes |
| **Git/Dolt** | Yes | Mostly | **No** | Yes | Yes | Yes |
| **Maturity** | High | High | Medium | High | Medium | Low |
| **Integration** | Heavy | Medium | N/A | **Done** | **Designed** | Medium |

## Recommendation: Two-Tier Strategy

### Tier 1: Local Development (macOS) — sandbox-exec

**Deploy immediately.** The infrastructure exists:
- ExecWrapper in RuntimeConfig is ready
- SBPL policy integration tests pass
- Claude Code itself uses sandbox-exec in production

**Implementation path:**
1. Create SBPL profile for gastown polecats (S1 from existing design doc)
2. Wire ExecWrapper into session manager (G5 from existing design doc)
3. Add `GT_PROCESS_NAMES` for wrapper detection
4. Test with actual polecat workflows

**Estimated effort:** Small-medium. Most code exists.

### Tier 2: Production/CI (Linux) — Daytona

**Integrate next.** The proxy server is built for this:
- DaytonaConfig struct designed in config/types.go
- mTLS proxy handles all control-plane and git operations
- Proxy client binary exists (`cmd/gt-proxy-client/`)

**Implementation path:**
1. Wire DaytonaConfig into session manager (G6 from existing design doc)
2. Implement workspace lifecycle (create/start/stop/delete)
3. Certificate injection into containers
4. E2E smoke test with actual Daytona provisioning

**Estimated effort:** Medium-large. Design exists but wiring is non-trivial.

### Future Consideration: Docker Sandboxes

Monitor Docker's sandbox feature as it matures. If it achieves Daytona-level
agent integration with the advantage of no cloud dependency, it could replace
or complement Daytona for self-hosted production deployments.

### Not Recommended

- **Firecracker:** Excellent isolation but Linux-only, heavy integration
- **gVisor:** Good option but Linux-only, syscall gaps risk
- **Wasm/WASI:** Fundamentally incompatible with native CLI toolchains

## Wanted Table Enforcement Gap

The `sandbox_required`, `sandbox_scope`, and `sandbox_min_tier` fields in the
wanted table are defined but never populated or enforced. Before Phase 2
commercial work:

1. **Populate fields** on wanted items that require sandboxing
2. **Enforce at dispatch** — sling should refuse to dispatch sandbox-required
   work to unsandboxed polecats
3. **Map scope to policy** — translate `sandbox_scope` JSON to SBPL profiles
   (local) or Daytona container policies (remote)

## Implementation Priority

| Step | Description | Depends On | Effort |
|------|-------------|------------|--------|
| 1 | SBPL polecat profile | Nothing | Small |
| 2 | Wire ExecWrapper into session manager | Step 1 | Medium |
| 3 | Process detection fix (GT_PROCESS_NAMES) | Step 2 | Small |
| 4 | Sandbox field enforcement in dispatch | Step 2 | Medium |
| 5 | DaytonaConfig wiring | Steps 1-3 proven | Large |
| 6 | Workspace lifecycle management | Step 5 | Large |
| 7 | E2E Daytona smoke test | Step 6 | Medium |

Steps 1-3 can ship independently and provide immediate value for local
development security. Steps 5-7 are the production path.

## Sources

- [Firecracker](https://firecracker-microvm.github.io/)
- [gVisor](https://gvisor.dev/docs/)
- [WASI and the WebAssembly Component Model](https://eunomia.dev/blog/2025/02/16/wasi-and-the-webassembly-component-model-current-status/)
- [Daytona — Secure Infrastructure for AI-Generated Code](https://www.daytona.io/)
- [Docker Sandboxes](https://docs.docker.com/ai/sandboxes/)
- [How to sandbox AI agents in 2026](https://northflank.com/blog/how-to-sandbox-ai-agents)
- [4 ways to sandbox untrusted code in 2026](https://dev.to/mohameddiallo/4-ways-to-sandbox-untrusted-code-in-2026-1ffb)
- Existing gastown design: `docs/design/sandboxed-polecat-execution.md`
- Existing gastown research: `docs/research/macos-sandbox-exec.md`
