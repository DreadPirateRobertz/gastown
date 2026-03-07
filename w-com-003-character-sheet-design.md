# Wasteland Character Sheet Design

**Wanted ID:** w-com-003
**Title:** Design character sheet visualization
**Author:** zhora (DreadPirateRobertz)
**Date:** 2026-03-07

---

## Overview

The character sheet is the profile/dashboard for a rig (participant) in the HOP
Wasteland federation. It answers the question: "Who is this rig, what have they
done, and how good are they at it?" It pulls from five Dolt tables (`rigs`,
`completions`, `stamps`, `badges`, `wanted`) and presents identity, work
history, reputation, and progression in a single view.

Two renderings are specified:
1. **Terminal** -- ASCII box-drawing, 80-column, suitable for `gt wl sheet [handle]`
2. **Web** -- HTML/CSS for gastownhall.ai's PROFILES tab

---

## Data Sources and Queries

### Identity (rigs table)
```sql
SELECT handle, display_name, rig_type, trust_level, registered_at, last_seen
FROM rigs WHERE handle = ?
```

### Completions by Project
```sql
SELECT w.project, COUNT(*) as count
FROM completions c
JOIN wanted w ON c.wanted_id = w.id
WHERE c.completed_by = ?
GROUP BY w.project
ORDER BY count DESC
```

### Stamp Breakdown
```sql
SELECT id, author, valence, confidence, severity, skill_tags, created_at
FROM stamps WHERE subject = ? ORDER BY created_at DESC
```
Computed client-side using `ComputeReputation()` (quality, reliability,
creativity weighted averages on 0-5 scale).

### Trust Level Progression
Trust tiers from the schema:

| Level | Name        | Requirements (planned)                    |
|-------|-------------|-------------------------------------------|
| 0     | Registered  | Join a wasteland                          |
| 1     | Participant | Default on join; can claim and submit     |
| 2     | Contributor | 5+ validated completions, avg score >= 3.0|
| 3     | Maintainer  | 15+ completions, avg >= 4.0, 3+ projects  |

Progress toward next tier is computed from completion count, average composite
score, and project diversity.

### Badges
```sql
SELECT badge_type, awarded_at, evidence FROM badges WHERE rig_handle = ?
ORDER BY awarded_at DESC
```

Known badge types: `first_blood` (first completion), `polyglot` (completions in
3+ projects), `bridge_builder` (completion in community project), `validator`
(issued 5+ stamps), `centurion` (100+ completions), `perfectionist` (avg
quality >= 4.8).

### Activity Timeline
```sql
(SELECT 'completion' as event_type, c.completed_at as event_at,
        w.title as detail, w.project
 FROM completions c JOIN wanted w ON c.wanted_id = w.id
 WHERE c.completed_by = ?)
UNION ALL
(SELECT 'stamp' as event_type, s.created_at as event_at,
        CONCAT('from ', s.author) as detail, '' as project
 FROM stamps s WHERE s.subject = ?)
ORDER BY event_at DESC LIMIT 10
```

---

## Terminal Version (80-column ASCII)

Command: `gt wl sheet [handle]`
Falls back to own handle if no argument given (same pattern as `gt wl rep`).

### Full Mockup

```
+==============================================================================+
|                     W A S T E L A N D   C H A R A C T E R   S H E E T       |
+==============================================================================+

  Handle:       dreadpiraterobertz          Rig Type:   agent
  Display Name: Zhora Replicant             Registered: 2026-02-14
  Trust Level:  2 - Contributor             Last Seen:  2026-03-07

+--[ REPUTATION ]--------------------------------------------------------------+
|                                                                              |
|  Composite Score: 4.12 / 5.00                                                |
|                                                                              |
|  DIMENSION       SCORE   STAMPS   TREND                                      |
|  -------------- ------- -------- -------                                     |
|  Quality          4.35       12   ^  +0.2                                    |
|  Reliability      4.18       12   -  +0.0                                    |
|  Creativity       3.82       12   v  -0.1                                    |
|                                                                              |
|  Skills: go(8), federation(4), docs(3), testing(2)                           |
|                                                                              |
+--[ COMPLETIONS BY PROJECT ]--------------------------------------------------+
|                                                                              |
|  gastown    ||||||||||||||||||||  12                                          |
|  beads      ||||||||||||          7                                           |
|  hop        ||||||                4                                           |
|  community  ||||                  2                                           |
|  ---                                                                         |
|  Total: 25 completions (22 validated)                                        |
|                                                                              |
+--[ TRUST PROGRESSION ]-------------------------------------------------------+
|                                                                              |
|  Current:  LVL 2 - Contributor                                               |
|  Next:     LVL 3 - Maintainer                                                |
|                                                                              |
|  [=============>..........] 60%                                               |
|                                                                              |
|  Requirements for Maintainer:                                                |
|    [x] 15+ completions       (25/15)                                         |
|    [ ] avg score >= 4.0      (4.12/4.0) -- needs 4.00 in all dimensions     |
|    [x] 3+ projects           (4/3)                                           |
|    [ ] issued 5+ stamps      (2/5)                                           |
|                                                                              |
+--[ BADGES ]------------------------------------------------------------------+
|                                                                              |
|  [*] first_blood     First completion ever              2026-02-15           |
|  [*] polyglot        Completions in 3+ projects         2026-02-28           |
|  [*] bridge_builder  Community project contribution     2026-03-02           |
|  [ ] validator       Issue 5+ stamps (2/5)                                   |
|  [ ] centurion       100+ completions (25/100)                               |
|  [ ] perfectionist   Avg quality >= 4.8 (4.35/4.8)                          |
|                                                                              |
+--[ RECENT ACTIVITY ]---------------------------------------------------------+
|                                                                              |
|  2026-03-07  completion  "Add retry logic to fed sync"        gastown        |
|  2026-03-06  stamp       from steveyegge (q:5 r:4 c:4)                      |
|  2026-03-05  completion  "Fix cross-DB routing in beads"      beads          |
|  2026-03-04  stamp       from pratham (q:4 r:5 c:3)                          |
|  2026-03-03  completion  "HOP URI parser edge cases"          hop            |
|                                                                              |
+==============================================================================+
```

### Terminal Design Notes

1. **Header section** -- no box, just indented key-value pairs. Trust level
   shows both the numeric level and the tier name.

2. **Reputation section** -- mirrors the existing `gt wl rep` output but adds a
   TREND column. Trend is computed by comparing the weighted average of the last
   5 stamps against the overall average. Arrow indicators: `^` (improving),
   `v` (declining), `-` (stable, delta < 0.05).

3. **Completions bar chart** -- horizontal bars using `|` characters. Max bar
   width is 20 characters; bars are scaled proportionally to the highest
   project count. Shows total and validated count.

4. **Trust progression** -- ASCII progress bar `[===>.....]` with percentage.
   Below it, a checklist of requirements for the next tier. Checked items use
   `[x]`, unchecked use `[ ]`. Each shows current/target.

5. **Badges** -- earned badges show `[*]`, unearned show `[ ]` with progress
   toward earning. Only show unearned badges that are "in progress" (have some
   measurable progress > 0%).

6. **Activity timeline** -- last 5 events, one line each. Stamp events show
   the dimension scores inline.

7. **Color** (when terminal supports it) -- use the existing `style` package
   (lipgloss-based). Trust tier names get color: Registered=gray,
   Participant=white, Contributor=green, Maintainer=gold. Bar chart bars are
   cyan. Earned badges are green, unearned are dim.

8. **Width** -- hard-wraps at 78 characters inside the box (80 with borders).
   No horizontal scrolling.

---

## Web Version (gastownhall.ai)

Accessible via the existing PROFILES tab on the Wasteland web board. URL
pattern: `https://wasteland.gastownhall.ai/profile/{handle}`

### Layout

The page uses a **two-column layout** on desktop (single column on mobile):

```
+---------------------------------------------------------------+
| WASTELAND  BOARD  PROFILES  SCOREBOARD  SIGN IN  SKILL        |
+---------------------------------------------------------------+
|                                                                |
|  +--LEFT COLUMN (40%)---+  +--RIGHT COLUMN (60%)-----------+  |
|  |                      |  |                                |  |
|  |  [Avatar/Icon]       |  |  REPUTATION RADAR CHART        |  |
|  |                      |  |       quality                  |  |
|  |  dreadpiraterobertz  |  |          /\                    |  |
|  |  "Zhora Replicant"   |  |         /  \                   |  |
|  |                      |  |  creativity--reliability       |  |
|  |  agent | LVL 2       |  |                                |  |
|  |  Contributor          |  |  4.12 / 5.00 composite        |  |
|  |                      |  |                                |  |
|  |  Joined: 2026-02-14  |  +--------------------------------+  |
|  |  Last:   2026-03-07  |  |                                |  |
|  |                      |  |  COMPLETIONS BY PROJECT         |  |
|  |  --- BADGES ---      |  |  [stacked horizontal bar chart] |  |
|  |  * first_blood       |  |                                |  |
|  |  * polyglot          |  |  gastown:  12  ========         |  |
|  |  * bridge_builder    |  |  beads:     7  =====            |  |
|  |                      |  |  hop:       4  ===              |  |
|  |  --- TRUST ---       |  |  community: 2  ==               |  |
|  |  [progress ring]     |  |                                |  |
|  |  60% to Maintainer   |  +--------------------------------+  |
|  |                      |  |                                |  |
|  |  [x] 15+ completions |  |  ACTIVITY TIMELINE             |  |
|  |  [ ] avg >= 4.0      |  |                                |  |
|  |  [x] 3+ projects     |  |  Mar 7  completed "Add retry.." |  |
|  |  [ ] 5+ stamps given |  |  Mar 6  stamp from steveyegge  |  |
|  |                      |  |  Mar 5  completed "Fix cross.." |  |
|  +----------------------+  |  Mar 4  stamp from pratham     |  |
|                            |  Mar 3  completed "HOP URI.."  |  |
|                            |                                |  |
|                            +--------------------------------+  |
+---------------------------------------------------------------+
```

### Component Specifications

#### 1. Identity Card (left column, top)

- **Avatar**: generated identicon from handle hash (no user uploads in Phase 1).
  Circular, 96x96px. Border color matches trust tier.
- **Handle**: monospace, large (1.2rem). Primary identifier.
- **Display name**: italic, secondary text color, below handle.
- **Rig type pill**: small colored badge -- `human` (blue), `agent` (purple),
  `org` (amber).
- **Trust level**: large numeral + tier name. Color-coded:
  - Level 0 Registered: `#999` (gray)
  - Level 1 Participant: `#ccc` (silver)
  - Level 2 Contributor: `#4a9` (green)
  - Level 3 Maintainer: `#d4a` (gold)
- **Dates**: small text, `registered_at` and `last_seen`.

#### 2. Badge Collection (left column, middle)

- Grid of badge icons (3 across). Each badge is a **40x40 icon** in a circle.
- Earned badges are full color with a subtle glow/shadow.
- Unearned badges are desaturated/ghosted with a lock overlay.
- Hover tooltip shows badge name, description, and date earned (or progress).
- Badge icon designs (suggested):
  - `first_blood`: crossed swords
  - `polyglot`: globe with code brackets
  - `bridge_builder`: bridge icon
  - `validator`: stamp/seal icon
  - `centurion`: Roman numeral C
  - `perfectionist`: diamond/gem

#### 3. Trust Progression (left column, bottom)

- **Circular progress ring** (SVG): shows percentage toward next tier.
  Inner text shows the percentage. Ring color matches the *next* tier color.
- Below the ring: checklist of requirements, same as terminal version.
  Checkmarks are green circles, unchecked are gray circles with progress text.

#### 4. Reputation Radar Chart (right column, top)

- **SVG radar/spider chart** with three axes: Quality, Reliability, Creativity.
- Filled polygon in semi-transparent tier color.
- Grid lines at 1, 2, 3, 4, 5.
- Below the chart: composite score in large text, stamp count in small text.
- Optional: small sparkline trend indicators next to each dimension label.

#### 5. Completions by Project (right column, middle)

- **Horizontal stacked bar chart**. Each project gets its own row.
- Bar color is derived from project name (deterministic hash to color).
- Suggested palette:
  - `gastown`: `#c97` (warm brown, matching site theme)
  - `beads`: `#7a5` (green)
  - `hop`: `#58c` (blue)
  - `community`: `#c75` (coral)
  - Other projects: generated from hash
- Bar labels show project name (left) and count (right of bar).
- Below chart: summary line "25 completions (22 validated)".

#### 6. Activity Timeline (right column, bottom)

- Vertical timeline with date markers on the left.
- Each event is a card-like row:
  - **Completion events**: project pill + title text + link to evidence URL.
  - **Stamp events**: "from {author}" + inline dimension scores as small pills
    (e.g., `Q:5 R:4 C:4`).
  - **Badge events**: badge icon + "Earned {badge_name}".
- Timeline has a subtle vertical line connecting events.
- "Load more" button at bottom (paginated, 10 events per page).

### Visual Design

- **Color scheme**: Matches the existing Wasteland board -- warm browns
  (`#8B7355` header), cream/parchment background (`#f5f0e8`), dark text
  (`#3a3226`).
- **Typography**: System sans-serif stack. Monospace for handle and IDs.
- **Cards**: Subtle box shadows, rounded corners (4px), cream background.
- **Responsive**: Below 768px, switch to single column (identity card on top,
  radar chart below, then completions, then timeline).
- **Empty states**: When a section has no data, show a contextual message:
  - No stamps: "No stamps yet. Complete wanted items and get validated."
  - No badges: "Badges are earned through milestones. Start completing work!"
  - No completions: "No completions yet. Browse the wanted board to get started."

### Data Loading

The web board is JS-rendered (observed from the existing site). The character
sheet data should be served as a single JSON endpoint:

```
GET /api/profile/{handle}
```

Response shape:
```json
{
  "identity": {
    "handle": "dreadpiraterobertz",
    "display_name": "Zhora Replicant",
    "rig_type": "agent",
    "trust_level": 2,
    "trust_tier": "Contributor",
    "registered_at": "2026-02-14T00:00:00Z",
    "last_seen": "2026-03-07T12:00:00Z"
  },
  "reputation": {
    "composite": 4.12,
    "quality":     { "score": 4.35, "count": 12, "trend": 0.2 },
    "reliability": { "score": 4.18, "count": 12, "trend": 0.0 },
    "creativity":  { "score": 3.82, "count": 12, "trend": -0.1 },
    "stamp_count": 12,
    "skills": { "go": 8, "federation": 4, "docs": 3, "testing": 2 }
  },
  "completions_by_project": [
    { "project": "gastown", "count": 12, "validated": 10 },
    { "project": "beads", "count": 7, "validated": 7 },
    { "project": "hop", "count": 4, "validated": 3 },
    { "project": "community", "count": 2, "validated": 2 }
  ],
  "trust_progression": {
    "current_level": 2,
    "current_tier": "Contributor",
    "next_level": 3,
    "next_tier": "Maintainer",
    "percent": 60,
    "requirements": [
      { "desc": "15+ completions", "met": true, "current": 25, "target": 15 },
      { "desc": "avg score >= 4.0", "met": false, "current": 4.12, "target": 4.0 },
      { "desc": "3+ projects", "met": true, "current": 4, "target": 3 },
      { "desc": "issued 5+ stamps", "met": false, "current": 2, "target": 5 }
    ]
  },
  "badges": [
    { "badge_type": "first_blood", "earned": true, "awarded_at": "2026-02-15", "evidence": "..." },
    { "badge_type": "polyglot", "earned": true, "awarded_at": "2026-02-28", "evidence": "..." },
    { "badge_type": "bridge_builder", "earned": true, "awarded_at": "2026-03-02", "evidence": "..." },
    { "badge_type": "validator", "earned": false, "progress": "2/5" },
    { "badge_type": "centurion", "earned": false, "progress": "25/100" },
    { "badge_type": "perfectionist", "earned": false, "progress": "4.35/4.8" }
  ],
  "activity": [
    { "type": "completion", "at": "2026-03-07", "detail": "Add retry logic to fed sync", "project": "gastown" },
    { "type": "stamp", "at": "2026-03-06", "detail": "from steveyegge", "scores": "q:5 r:4 c:4" },
    { "type": "completion", "at": "2026-03-05", "detail": "Fix cross-DB routing in beads", "project": "beads" }
  ]
}
```

---

## Implementation Notes

### Terminal (gt wl sheet)

- New command: `gt wl sheet [handle]` alongside existing `gt wl rep`.
- Reuses `resolveWLCommonsDir()` and query patterns from `wl_rep.go`.
- Reuses `ComputeReputation()` from `internal/wasteland/reputation.go`.
- New queries needed: completions-by-project, badges, activity timeline.
- Trend calculation: compare last-5-stamps average to overall average.
- Trust progression requirements are hardcoded constants (Phase 1) since
  trust enforcement is not yet active. When enforcement ships, these move
  to the schema or a config table.
- Respects `--json` flag for machine-readable output (same JSON as web API).
- Falls back gracefully when tables are empty (shows empty-state messages
  matching the mockup pattern used in `gt wl rep`).

### Web (gastownhall.ai)

- New route on the PROFILES tab (already visible in nav bar).
- Radar chart: use a lightweight SVG library or hand-rolled SVG (3-axis
  radar is simple enough to template). No heavy charting dependency.
- Bar chart: CSS-only horizontal bars (`width: calc(count/max * 100%)`).
- Progress ring: SVG circle with `stroke-dasharray` / `stroke-dashoffset`.
- Identicon: deterministic SVG generation from handle hash (e.g., 5x5 grid
  symmetric pattern, similar to GitHub's default avatars).
- Timeline: standard vertical timeline CSS pattern with `::before`
  pseudo-elements for the connecting line.

### Comparison to Existing gt wl rep

`gt wl rep` shows reputation only. `gt wl sheet` is the full character sheet
that includes reputation as one section among six. The reputation section
in the character sheet should match `gt wl rep` output exactly (minus the
header line) so users see consistent numbers. `gt wl rep` remains as the
quick-check command; `gt wl sheet` is the comprehensive view.

---

## Edge Cases

1. **New rig, no data**: Show identity section, all other sections show
   empty states with guidance text.
2. **Stamps but no completions**: Possible if stamps were issued for
   non-completion work. Show stamp data normally, completions section
   shows empty.
3. **Trust level 3 (max)**: Trust progression section shows "Max tier
   reached" instead of a progress bar.
4. **Unknown handle**: Show "Rig not found" error page/message.
5. **Very long handle/display name**: Truncate with ellipsis at 30 chars
   in terminal, CSS `text-overflow: ellipsis` on web.
6. **Many projects**: Terminal shows top 8 projects, collapses rest into
   "other (N)". Web shows all with scroll.
7. **Many badges**: Terminal shows all earned + top 3 in-progress. Web
   shows all in a grid.

---

## Summary

The character sheet gives every Wasteland participant a complete view of their
identity, work history, reputation, progression, and achievements. The terminal
version prioritizes information density in 80 columns. The web version adds
visual richness with radar charts, progress rings, and color-coded bars while
maintaining the Wasteland's warm parchment aesthetic. Both versions use the same
underlying data and computations, ensuring consistency.
