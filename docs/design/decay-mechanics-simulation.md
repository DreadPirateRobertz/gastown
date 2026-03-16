# Stamp Decay and Trust Progression Simulation

**Wasteland:** w-hop-006
**Date:** 2026-03-15
**Author:** gastown/crew/deckard
**Status:** Draft

## Overview

This document models how stamp confidence decays over time, how trust levels
progress based on accumulated stamps, and how the proposed 6 valence dimensions
interact in practice. The goal is to propose starting parameters that produce
healthy reputation dynamics — rewarding sustained contribution while allowing
recovery from poor performance.

---

## 1. The Six Valence Dimensions

The MVR schema stores stamp valence as a JSON object. We propose 6 dimensions
that capture the full spectrum of contribution quality:

| Dimension | Scale | What it measures | Who judges best |
|-----------|-------|-----------------|-----------------|
| **quality** | 1-5 | Correctness, completeness, polish of the work | Validator (domain expert) |
| **reliability** | 1-5 | Did the work match what was promised? On time? | Requester + system (deadline tracking) |
| **creativity** | 1-5 | Novel approach, elegant solution, insight | Validator |
| **thoroughness** | 1-5 | Edge cases, testing, documentation, follow-through | Validator |
| **collaboration** | 1-5 | Communication, responsiveness, constructive interaction | Requester + peers |
| **impact** | 1-5 | How much did this move the needle? Strategic value. | Maintainer (retrospective) |

### Dimension Interactions

Not all dimensions are independent. Expected correlations:

```
quality ←→ thoroughness     (strong positive: thorough work tends to be higher quality)
creativity ←→ impact        (moderate positive: novel solutions often have outsized impact)
reliability ←→ collaboration (moderate positive: reliable workers tend to communicate well)
creativity ←→ thoroughness   (weak negative: creative leaps sometimes skip edge cases)
```

These correlations are **observed, not enforced**. Validators score each
dimension independently. The correlations emerge from data and can be used
to detect outlier stamps (a 5/5 creativity with 1/5 quality should trigger
review).

### Dimensional Weighting

Not all dimensions contribute equally to trust progression. Default weights:

| Dimension | Weight | Rationale |
|-----------|--------|-----------|
| quality | 1.0 | Core signal — did the work meet standards? |
| reliability | 1.0 | Core signal — can you depend on this rig? |
| creativity | 0.5 | Nice to have but not required for trust |
| thoroughness | 0.8 | Important for trust — sloppy work erodes confidence |
| collaboration | 0.6 | Important for team dynamics, less for solo work |
| impact | 0.3 | Hard to judge, varies by context, retrospective bias |

Weighted score for a stamp:

```
weighted_score = Σ(dimension_value × dimension_weight) / Σ(dimension_weight)
               = (q×1.0 + r×1.0 + c×0.5 + t×0.8 + co×0.6 + i×0.3) / 4.2
```

On a 1-5 scale, the weighted score also ranges 1-5.

---

## 2. Confidence Decay Model

Stamp confidence should decay because:
- Old work is less representative of current capability
- Skills atrophy without practice
- Project context changes (a security fix from 2 years ago may not reflect
  current security practices)

### Exponential Decay

The simplest model: each stamp's effective confidence decreases exponentially
from its original value.

```
effective_confidence(t) = original_confidence × e^(-λt)
```

Where:
- `t` = time since stamp was issued (in days)
- `λ` = decay constant = ln(2) / half_life
- `original_confidence` = validator's confidence score (0.0-1.0)

### Proposed Half-Lives by Severity

| Severity | Half-life | Rationale |
|----------|-----------|-----------|
| **leaf** | 180 days | Individual tasks: recent work matters most |
| **branch** | 365 days | Project-level: slower decay, broader signal |
| **root** | 730 days (2yr) | Career-defining: these persist longest |

### Decay Simulation

For a rig with 20 leaf stamps over 2 years, all with original confidence 0.8:

```
Time      Stamps  Avg effective_confidence  Reputation score
Day 0     20      0.800                     Full strength
Day 90    20      0.567                     ~71% of peak
Day 180   20      0.400                     50% (half-life hit)
Day 365   20      0.200                     25%
Day 730   20      0.050                     ~6% (nearly gone)
```

**Implication**: A rig that stops contributing sees their reputation halve
every 6 months. This is intentionally aggressive for leaf stamps — it means
"what have you done lately?" dominates reputation.

### Activity Bonus (Anti-Decay)

Pure decay penalizes rigs who take a break. To soften this, new stamps
partially refresh the decay clock on related stamps:

```
When a new stamp arrives with tags T:
  For each existing stamp with overlapping tags:
    effective_age = actual_age × activity_discount
    where activity_discount = max(0.5, 1.0 - 0.1 × recent_completions_in_domain)
```

This means: if you've done 5 recent completions in the same domain, your older
stamps in that domain age at half speed. The floor of 0.5 prevents complete
decay immunity.

---

## 3. Trust Level Progression

### Proposed Thresholds

Trust progression depends on accumulated weighted reputation score (after decay):

| Level | Name | Threshold | What it means |
|-------|------|-----------|---------------|
| 0 | Outsider | 0 | Registered, no completions |
| 1 | Participant | 0 (default) | Can browse, claim, submit |
| 2 | Contributor | 15.0 | ~5 completions with avg 3.0 weighted score |
| 3 | Maintainer | 50.0 | ~15 completions with avg 3.3 weighted score, or fewer high-quality ones |

### Score Accumulation

Each validated completion contributes to the trust score:

```
completion_score = weighted_stamp_score × stamp_confidence × severity_multiplier
```

Severity multipliers:

| Severity | Multiplier | Rationale |
|----------|-----------|-----------|
| leaf | 1.0 | Standard contribution |
| branch | 2.5 | Project-level work is harder and rarer |
| root | 5.0 | Career-defining contributions |

**Example**: A leaf stamp with quality=4, reliability=4, creativity=3,
thoroughness=4, collaboration=3, impact=3, confidence=0.9:

```
weighted_score = (4×1.0 + 4×1.0 + 3×0.5 + 4×0.8 + 3×0.6 + 3×0.3) / 4.2
               = (4 + 4 + 1.5 + 3.2 + 1.8 + 0.9) / 4.2
               = 15.4 / 4.2
               = 3.67

completion_score = 3.67 × 0.9 × 1.0 = 3.30
```

### Trust Score with Decay

The total trust score at time T is:

```
trust_score(T) = Σ completion_score_i × e^(-λ_severity × (T - t_i))
```

This means trust level can **decrease** as stamps decay. A rig that was
Contributor (score 15.0) but hasn't contributed in a year may drop back to
Participant as their stamps decay below threshold.

### Simulation: Path to Contributor

Assuming consistent leaf-level work with average stamp scores:

```
Completions  Avg weighted  Confidence  Trust score  Level
1            3.5           0.8         2.80         Participant
3            3.5           0.8         8.40         Participant
5            3.5           0.8         14.00        Participant
6            3.5           0.8         16.80        Contributor ✓
```

**~6 solid completions** to reach Contributor. This feels right — enough to
demonstrate sustained capability, not so many that the barrier feels arbitrary.

### Simulation: Path to Maintainer

```
Completions  Avg weighted  Confidence  Trust score  Level
10           3.5           0.8         28.00        Contributor
15           3.5           0.8         42.00        Contributor
18           3.5           0.8         50.40        Maintainer ✓
```

**~18 solid completions** to reach Maintainer with average scores. A rig that
does exceptional work (avg 4.5 weighted, 1.0 confidence) can get there in ~12.

With branch-level work mixed in:

```
10 leaf (3.5 × 0.8 × 1.0) = 28.00
3 branch (3.5 × 0.8 × 2.5) = 21.00
Total = 49.00 → almost Maintainer
```

**Branch work accelerates trust dramatically** — this is intentional. Project-
level contributions should be weighted higher.

### Decay Effect on Trust

A rig at Maintainer (score 50.0) who stops contributing:

```
Day 0:    50.0  → Maintainer
Day 90:   35.4  → Contributor (dropped!)
Day 180:  25.0  → Contributor
Day 365:  12.5  → Participant (dropped again!)
Day 540:   6.2  → Participant
Day 730:   3.1  → Participant
```

**A Maintainer who stops contributing loses the title in ~3 months** (with
all-leaf stamps). If they have branch/root stamps, decay is slower (365d/730d
half-lives).

This is probably too aggressive. Options:
1. Increase leaf half-life to 365 days (matches branch)
2. Add a "trust floor" — once you reach a level, you don't drop below the
   previous level for N days (grace period)
3. Separate "peak trust" from "current trust" — display both

**Recommendation**: Option 2 — add a 180-day grace period. A Maintainer
who stops contributing stays Maintainer for 180 days, then drops to
Contributor (not all the way to Participant). Another 180 days of inactivity
drops them to Participant.

---

## 4. Negative Stamps and Recovery

### Negative Stamp Effects

Stamps with low valence (1-2 on any dimension) reduce trust score:

```
If weighted_score < 2.0:
  completion_score is NEGATIVE
  completion_score = (weighted_score - 2.5) × confidence × severity_multiplier
```

Example: A stamp with quality=1, reliability=2, all others=2, confidence=0.9:

```
weighted_score = (1×1.0 + 2×1.0 + 2×0.5 + 2×0.8 + 2×0.6 + 2×0.3) / 4.2
               = (1 + 2 + 1 + 1.6 + 1.2 + 0.6) / 4.2
               = 7.4 / 4.2
               = 1.76

completion_score = (1.76 - 2.5) × 0.9 × 1.0 = -0.67
```

One bad stamp costs ~0.67 points. It takes ~1 good stamp to recover.
This is intentional — reputation should be **easy to damage, hard to build**
(asymmetric, like trust in real life).

### Recovery Mechanics

A rig recovering from a bad completion needs to:
1. Accept the negative stamp (no appeals in Phase 1)
2. Do good work to accumulate positive score
3. Wait for the negative stamp to decay (180 days for leaf)

The negative stamp also decays, which is important — a mistake from a year
ago shouldn't permanently define a rig.

---

## 5. Spider Protocol Integration

The stamp decay model feeds into fraud detection (w-hop-003). Red flags that
the Spider Protocol should check:

| Signal | Detection | Action |
|--------|-----------|--------|
| Stamp inflation | Validator consistently gives 5/5 on all dimensions | Discount stamps from this validator |
| Reciprocal stamping | A stamps B, B stamps A repeatedly | Flag for review |
| Dimension inconsistency | 5/5 creativity + 1/5 quality (negative correlation) | Request re-review |
| Velocity anomaly | 20 completions in one day | Flag for review |
| Trust farming | Rig claims only trivial items to accumulate score | Weight trivial items lower |

---

## 6. Proposed Starting Parameters

For Phase 1 (wild-west mode), these are recommendations. Enforcement comes
in Phase 2.

```toml
[decay]
leaf_halflife = "180d"
branch_halflife = "365d"
root_halflife = "730d"
activity_discount_enabled = true
activity_discount_floor = 0.5
activity_discount_per_completion = 0.1

[trust_thresholds]
outsider = 0
participant = 0      # default on registration
contributor = 15.0
maintainer = 50.0
grace_period = "180d" # don't drop more than 1 level per grace period

[severity_multipliers]
leaf = 1.0
branch = 2.5
root = 5.0

[dimension_weights]
quality = 1.0
reliability = 1.0
creativity = 0.5
thoroughness = 0.8
collaboration = 0.6
impact = 0.3

[negative_stamps]
threshold = 2.0      # weighted_score below this is negative
anchor = 2.5         # score is (weighted - anchor) × confidence × severity
recovery_ratio = 1.5  # ~1.5 good stamps needed to offset 1 bad stamp

[fraud_detection]
inflation_threshold = 4.8  # avg valence above this triggers review
reciprocal_window = "30d"  # window for detecting mutual stamping
velocity_limit = 5         # max completions per day before flag
```

---

## 7. Visualization

For the character sheet (w-com-003), the reputation display should show:

```
  ╭─────────────────────────────────────╮
  │  DreadPirateRobertz                 │
  │  Trust: Contributor (score: 22.4)   │
  │  Completions: 8 (3 decayed)         │
  │                                     │
  │  Dimensions:                        │
  │  quality       ████░ 4.1            │
  │  reliability   ████░ 3.8            │
  │  creativity    ███░░ 3.2            │
  │  thoroughness  ████░ 3.9            │
  │  collaboration ███░░ 3.0            │
  │  impact        ██░░░ 2.5            │
  │                                     │
  │  Recent stamps:                     │
  │  ★ branch  w-gc-001  4.2  (2d ago)  │
  │  ★ leaf    w-hop-002 3.8  (1d ago)  │
  │  ☆ leaf    w-bd-003  2.1  (45d ago) │
  ╰─────────────────────────────────────╯
```

The dimension bars show the **weighted average across all non-decayed stamps**.
Decayed stamps (effective confidence < 0.1) are counted as "decayed" and
excluded from the average.

---

## Open Questions

1. **Should trust demotion notify the rig?** If a Maintainer drops to
   Contributor due to decay, they should probably get a notification
   ("Your trust level has decreased — submit new work to restore it").

2. **Cross-chain stamp weight** — The constitution parameters (w-hop-002)
   include `stamp_import_discount`. How does decay interact with imported
   stamps? Should imported stamps decay faster (1.5× rate)?

3. **Validator reputation** — Should validators' own trust score affect the
   weight of stamps they issue? A Maintainer's stamp counting more than a
   Contributor's stamp creates a hierarchy that reinforces itself. This could
   be good (quality signal) or bad (gatekeeping).

4. **Dimension set evolution** — If a chain adds or removes dimensions, how
   do existing stamps translate? The JSON valence format is flexible, but
   weighted scoring needs to handle missing dimensions gracefully (use
   chain-wide average as default).

5. **The 6th dimension** — "Impact" is the most subjective and hardest to
   judge at completion time. It's often only visible retrospectively. Should
   it be scored at validation time, or added later as a "retrospective stamp"?
