+++
name = "crew-watchdog"
description = "Monitor crew and polecat sessions for idle workers, low context, dead agents, and thin queues"
version = 1

[gate]
type = "cooldown"
duration = "10m"

[tracking]
labels = ["plugin:crew-watchdog", "category:health-monitoring"]
digest = true

[execution]
timeout = "3m"
notify_on_failure = true
severity = "medium"
+++

# Crew Watchdog

Monitors crew and polecat session health across all rigs. Detects:
- Dead sessions (tmux session gone or agent process dead)
- Idle workers (sitting at prompt, not working)
- Low context workers (near context limit, need handoff)
- Thin bead queues (rigs running low on open work items)

Escalates findings and nudges witnesses for action.

## Step 1: Verify prerequisites

```bash
which tmux > /dev/null 2>&1
if [ $? -ne 0 ]; then
  echo "SKIP: tmux not available"
  exit 0
fi

TOWN_ROOT="$HOME/gt"
```

## Step 2: Enumerate rigs

```bash
RIG_JSON=$(gt rig list --json 2>/dev/null)
if [ $? -ne 0 ] || [ -z "$RIG_JSON" ]; then
  echo "SKIP: could not get rig list"
  exit 0
fi

RIG_NAMES=$(echo "$RIG_JSON" | jq -r '.[].name // empty' 2>/dev/null)
```

## Step 3: Check crew sessions

Use `gt crew status --json` per rig to check crew worker health.

```bash
IDLE=()
LOW_CTX=()
DEAD_CREW=()
HEALTHY_CREW=0

for RIG in $RIG_NAMES; do
  CREW_JSON=$(gt crew status --json --rig "$RIG" 2>/dev/null) || continue
  [ -z "$CREW_JSON" ] && continue

  # Parse each crew member
  COUNT=$(echo "$CREW_JSON" | jq 'length' 2>/dev/null)
  [ -z "$COUNT" ] || [ "$COUNT" = "0" ] && continue

  for i in $(seq 0 $((COUNT - 1))); do
    NAME=$(echo "$CREW_JSON" | jq -r ".[$i].name" 2>/dev/null)
    HAS_SESSION=$(echo "$CREW_JSON" | jq -r ".[$i].has_session" 2>/dev/null)
    SESSION_ID=$(echo "$CREW_JSON" | jq -r ".[$i].session_id // empty" 2>/dev/null)

    if [ "$HAS_SESSION" = "false" ]; then
      DEAD_CREW+=("$RIG/$NAME")
      continue
    fi

    # Check for idle (sitting at permission prompt)
    if [ -n "$SESSION_ID" ]; then
      OUTPUT=$(tmux capture-pane -t "$SESSION_ID" -p 2>/dev/null | tail -5)

      if echo "$OUTPUT" | grep -q "bypass permissions\|shift+tab" 2>/dev/null; then
        IDLE+=("$RIG/$NAME")
        continue
      fi

      # Check for low context
      if echo "$OUTPUT" | grep -q "Context left" 2>/dev/null; then
        CTX_PCT=$(echo "$OUTPUT" | grep -o 'Context left[^%]*%' | grep -o '[0-9]*%' | tr -d '%')
        if [ -n "$CTX_PCT" ] && [ "$CTX_PCT" -lt 15 ]; then
          LOW_CTX+=("$RIG/$NAME:${CTX_PCT}%")
          continue
        fi
      fi
    fi

    HEALTHY_CREW=$((HEALTHY_CREW + 1))
  done
done
```

## Step 4: Check polecat sessions

Enumerate polecats per rig and check tmux session liveness.

```bash
DEAD_PCATS=()
IDLE_PCATS=()
HEALTHY_PCATS=0

for RIG in $RIG_NAMES; do
  POLECAT_DIR="$TOWN_ROOT/$RIG/polecats"
  [ -d "$POLECAT_DIR" ] || continue

  for PCAT_PATH in "$POLECAT_DIR"/*/; do
    [ -d "$PCAT_PATH" ] || continue
    PCAT_NAME=$(basename "$PCAT_PATH")
    SESSION_NAME="${RIG}-polecat-${PCAT_NAME}"

    if ! tmux has-session -t "$SESSION_NAME" 2>/dev/null; then
      # Only flag as dead if it has hooked work
      HOOK_FILE="$PCAT_PATH/.hook.json"
      if [ -f "$HOOK_FILE" ]; then
        HOOK_BEAD=$(jq -r '.hook_bead // empty' "$HOOK_FILE" 2>/dev/null)
        if [ -n "$HOOK_BEAD" ]; then
          DEAD_PCATS+=("$RIG/$PCAT_NAME:$HOOK_BEAD")
        fi
      fi
      continue
    fi

    # Session alive — check for idle
    OUTPUT=$(tmux capture-pane -t "$SESSION_NAME" -p 2>/dev/null | tail -5)
    if echo "$OUTPUT" | grep -q "bypass permissions\|shift+tab" 2>/dev/null; then
      IDLE_PCATS+=("$RIG/$PCAT_NAME")
    else
      HEALTHY_PCATS=$((HEALTHY_PCATS + 1))
    fi
  done
done
```

## Step 5: Check bead queue health

Flag rigs with fewer than 3 open beads as thin queues.

```bash
THIN_QUEUES=()

for RIG in $RIG_NAMES; do
  RIG_DIR="$TOWN_ROOT/$RIG"
  [ -d "$RIG_DIR/.beads" ] || continue

  OPEN_COUNT=$(cd "$RIG_DIR" && bd list --json 2>/dev/null \
    | jq '[.[] | select(.status == "open")] | length' 2>/dev/null || echo "0")

  if [ "$OPEN_COUNT" -lt 3 ]; then
    THIN_QUEUES+=("$RIG:${OPEN_COUNT}_open")
  fi
done
```

## Step 6: Report findings

```bash
TOTAL_DEAD=$((${#DEAD_CREW[@]} + ${#DEAD_PCATS[@]}))
TOTAL_IDLE=$((${#IDLE[@]} + ${#IDLE_PCATS[@]}))
TOTAL_HEALTHY=$((HEALTHY_CREW + HEALTHY_PCATS))

SUMMARY="${TOTAL_IDLE} idle, ${#LOW_CTX[@]} low-ctx, ${TOTAL_DEAD} dead, ${#THIN_QUEUES[@]} thin queues"

echo "crew-watchdog: $SUMMARY"
echo "  Healthy: $TOTAL_HEALTHY agents"
[ ${#IDLE[@]} -gt 0 ] && echo "  Idle crew: ${IDLE[*]}"
[ ${#IDLE_PCATS[@]} -gt 0 ] && echo "  Idle polecats: ${IDLE_PCATS[*]}"
[ ${#LOW_CTX[@]} -gt 0 ] && echo "  Low context: ${LOW_CTX[*]}"
[ ${#DEAD_CREW[@]} -gt 0 ] && echo "  Dead crew: ${DEAD_CREW[*]}"
[ ${#DEAD_PCATS[@]} -gt 0 ] && echo "  Dead polecats: ${DEAD_PCATS[*]}"
[ ${#THIN_QUEUES[@]} -gt 0 ] && echo "  Thin queues: ${THIN_QUEUES[*]}"

# Nudge witnesses about dead polecats (they handle restarts)
for ENTRY in "${DEAD_PCATS[@]}"; do
  RIG_PCAT="${ENTRY%%:*}"
  RIG="${RIG_PCAT%%/*}"
  gt nudge "$RIG/witness" "crew-watchdog: dead polecat detected: $ENTRY" 2>/dev/null || true
done
```

## Record Result

```bash
if [ "$TOTAL_DEAD" -eq 0 ] && [ ${#THIN_QUEUES[@]} -eq 0 ]; then
  bd create "crew-watchdog: $SUMMARY" -t chore --ephemeral \
    -l type:plugin-run,plugin:crew-watchdog,result:success \
    -d "crew-watchdog: $SUMMARY. $TOTAL_HEALTHY healthy agents." --silent 2>/dev/null || true
else
  BODY="crew-watchdog: $SUMMARY"
  [ ${#DEAD_CREW[@]} -gt 0 ] && BODY="$BODY | Dead crew: ${DEAD_CREW[*]}"
  [ ${#DEAD_PCATS[@]} -gt 0 ] && BODY="$BODY | Dead polecats: ${DEAD_PCATS[*]}"
  [ ${#THIN_QUEUES[@]} -gt 0 ] && BODY="$BODY | Thin queues: ${THIN_QUEUES[*]}"

  bd create "crew-watchdog: ISSUES DETECTED" -t chore --ephemeral \
    -l type:plugin-run,plugin:crew-watchdog,result:warning \
    -d "$BODY" --silent 2>/dev/null || true
fi
```

On failure:

```bash
bd create "crew-watchdog: FAILED" -t chore --ephemeral \
  -l type:plugin-run,plugin:crew-watchdog,result:failure \
  -d "Crew watchdog monitoring failed: $ERROR" --silent 2>/dev/null || true

gt escalate "Plugin FAILED: crew-watchdog" \
  --severity medium \
  --reason "$ERROR" 2>/dev/null || true
```
