#!/usr/bin/env bash
# Fixture tests for the PURE sentinel-cell verdicts in taskfiles/e2e/stack.sh — the three
# decisions the cell:community-direct-sentinel runner delegates to (recovery, sentinel-role,
# mode=sentinel log), plus two lockstep greps. All run OUTSIDE the Docker stack (canned streams
# on stdin, exit-code assertions), so the verdict contracts have durable regression coverage,
# not a one-time manual proof (Constitution §VIII / NFR-005).
#
# Coverage:
#   • e2e_recovery_verdict — signal-first-by-presence + the SC-004 headroom gate + every
#     fail-closed path (no promotion, promotion==killed, delivery-before-promotion, no delivery,
#     over-headroom). NOTE on "deadline-tie": a pure stream-consuming verdict has no select
#     race (that lives in the LIVE loop, which appends the in-flight outcome before breaking —
#     e2e_sentinel_failover). The verdict's guarantee is that a `delivered` line is credited by
#     PRESENCE even when it follows earlier `nodelivery` lines (case "signal after nodelivery").
#     A delivery at/after the deadline is > headroom → reds (SC-004), so it is a fail case, not
#     an exit-0 case.
#   • e2e_sentinel_role_verdict — genuine sentinel master record vs empty / data-node reply.
#   • e2e_sentinel_mode_log_verdict — present / absent / wrong-mode (direct) classifier.
#   • Lockstep greps — (i) override sentinel knobs == stack.sh constants; (ii) the cell's
#     REQUIRE_PASS uses <suite>:<value> where <value> is the tester's deliveryCheck() name arg.
#
# Run: bash taskfiles/e2e/sentinel_guard_test.sh   (wired into CI before the stack boots).
set -uo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO=$(cd "$SCRIPT_DIR/../.." && pwd)
# shellcheck source=./stack.sh
. "$SCRIPT_DIR/stack.sh"

fails=0

# run_recovery <want_exit> <name> <killed> <deadline> <headroom> <stream>
run_recovery() {
  local want="$1" name="$2" killed="$3" deadline="$4" headroom="$5" stream="$6" got
  printf '%s\n' "$stream" | e2e_recovery_verdict "$killed" "$deadline" "$headroom" >/dev/null 2>&1
  got=$?
  if [ "$got" -eq "$want" ]; then echo "ok   - $name (exit $got)"; else
    echo "FAIL - $name: exit $got, want $want"; fails=$((fails + 1)); fi
}

# run_role <want_exit> <name> <reply>
run_role() {
  local want="$1" name="$2" reply="$3" got
  printf '%s' "$reply" | e2e_sentinel_role_verdict >/dev/null 2>&1
  got=$?
  if [ "$got" -eq "$want" ]; then echo "ok   - $name (exit $got)"; else
    echo "FAIL - $name: exit $got, want $want"; fails=$((fails + 1)); fi
}

# run_modelog <want_exit> <name> <logs>
run_modelog() {
  local want="$1" name="$2" logs="$3" got
  printf '%s\n' "$logs" | e2e_sentinel_mode_log_verdict >/dev/null 2>&1
  got=$?
  if [ "$got" -eq "$want" ]; then echo "ok   - $name (exit $got)"; else
    echo "FAIL - $name: exit $got, want $want"; fails=$((fails + 1)); fi
}

# ---------------------------------------------------------------------------
# e2e_recovery_verdict (killed=valkey, deadline=90s, headroom=66% → limit=59s)
# ---------------------------------------------------------------------------
run_recovery 0 "recovery: delivered in headroom after promotion" \
  valkey 90 66 "promoted valkey-replica 3
delivered 10"
run_recovery 0 "recovery: delivered is credited by PRESENCE after nodelivery ticks (signal-first)" \
  valkey 90 66 "promoted valkey-replica 3
nodelivery 20 probe-failed
nodelivery 40 probe-failed
delivered 50"
run_recovery 1 "recovery: over-headroom delivery reds (SC-004 flake-tight)" \
  valkey 90 66 "promoted valkey-replica 3
delivered 75"
run_recovery 1 "recovery: at/after deadline reds (> headroom)" \
  valkey 90 66 "promoted valkey-replica 3
delivered 95"
run_recovery 1 "recovery: no promotion ever (deadline expired, no delivery)" \
  valkey 90 66 "nodelivery 20 probe-failed
nodelivery 40 probe-failed"
run_recovery 1 "recovery: promotion present but never delivered" \
  valkey 90 66 "promoted valkey-replica 3
nodelivery 20 probe-failed
nodelivery 60 probe-failed"
run_recovery 1 "recovery: delivery BEFORE promotion → old-master false-positive not credited" \
  valkey 90 66 "delivered 5
promoted valkey-replica 10"
run_recovery 1 "recovery: promoted master == killed master → not a real promotion" \
  valkey 90 66 "promoted valkey 10
delivered 20"

# ---------------------------------------------------------------------------
# e2e_sentinel_role_verdict — genuine sentinel record vs empty / data-node
# ---------------------------------------------------------------------------
VALID_MASTER=$'name\nmymaster\nip\n172.20.0.5\nport\n6379\nflags\nmaster\nnum-other-sentinels\n2\nquorum\n2'
run_role 0 "role: valid mymaster sentinel record passes" "$VALID_MASTER"
run_role 1 "role: empty reply fails closed" ""
run_role 1 "role: data-node error reply fails closed" "ERR unknown command 'SENTINEL'"
run_role 1 "role: wrong master name fails" $'name\nothermaster\nflags\nmaster\nnum-other-sentinels\n2\nquorum\n2'
run_role 1 "role: too-few sentinels fails" $'name\nmymaster\nflags\nmaster\nnum-other-sentinels\n0\nquorum\n2'

# ---------------------------------------------------------------------------
# e2e_sentinel_mode_log_verdict — present / absent / wrong-mode
# ---------------------------------------------------------------------------
run_modelog 0 "mode-log: sentinel line present passes" \
  '{"level":"info","mode":"sentinel","message":"Connecting to Valkey Sentinel"}'
run_modelog 1 "mode-log: no mode line fails" \
  '{"level":"info","message":"some other log"}'
run_modelog 1 "mode-log: direct-mode line fails (wrong mode)" \
  '{"level":"info","mode":"direct","message":"Connecting to Valkey (direct mode)"}'
run_modelog 1 "mode-log: direct present even if sentinel also present fails (mixed → red)" \
  '{"mode":"sentinel"}
{"mode":"direct"}'

# ---------------------------------------------------------------------------
# Lockstep (i): the pinned sentinel knobs in the override MUST equal the stack.sh constants.
# Extract each directive's numeric literal from the override and compare to the E2E_SENTINEL_*
# value (anchored on the directive name, not a bare number — avoids false-positive matches).
# ---------------------------------------------------------------------------
OVERRIDE="$REPO/taskfiles/e2e/valkey-sentinel.override.yml"
check_knob() {
  local directive="$1" want="$2" got
  got=$(grep -E "sentinel ${directive} mymaster " "$OVERRIDE" | grep -oE '[0-9]+$' | head -1)
  if [ "$got" = "$want" ]; then
    echo "ok   - lockstep: override ${directive}=${got} == stack.sh ${want}"
  else
    echo "FAIL - lockstep: override ${directive}='${got}' != stack.sh '${want}' (knob drift)"
    fails=$((fails + 1))
  fi
}
check_knob "down-after-milliseconds" "$E2E_SENTINEL_DOWN_AFTER_MS"
check_knob "failover-timeout" "$E2E_SENTINEL_FAILOVER_TIMEOUT_MS"

# ---------------------------------------------------------------------------
# Lockstep (ii): the cell's REQUIRE_PASS uses <suite>:<value> where <value> is the exact
# deliveryCheck() name arg in the tester source (the pubsub:/reconnect: prefix is the e2e.yml
# scoping convention that _e2e_suite_scoped_checks strips). Extract the Go value, assert the
# composed <suite>:<value> appears in taskfiles/e2e.yml — FR-003 name-lockstep. (Mirrors the
# battery_guard_test.sh:218-233 extract-then-assert precedent.)
# ---------------------------------------------------------------------------
E2E_YML="$REPO/taskfiles/e2e.yml"
check_require_pass() {
  local gofile="$1" suite="$2" want_hint="$3" val
  val=$(grep -oE "deliveryCheck\(\"${want_hint}\"" "$REPO/ws/cmd/tester/runner/$gofile" \
    | sed -E 's/.*deliveryCheck\("([^"]*)".*/\1/' | head -1)
  if [ -z "$val" ]; then
    echo "FAIL - name-lockstep: deliveryCheck(\"${want_hint}\") not found in $gofile (check renamed)"
    fails=$((fails + 1))
  elif grep -qF "${suite}:${val}" "$E2E_YML"; then
    echo "ok   - name-lockstep: e2e.yml REQUIRE_PASS uses ${suite}:${val} (matches $gofile)"
  else
    echo "FAIL - name-lockstep: no '${suite}:${val}' in e2e.yml REQUIRE_PASS ($gofile check renamed without updating the cell)"
    fails=$((fails + 1))
  fi
}
check_require_pass "validate_pubsub.go" "pubsub" "public round-trip"
check_require_pass "validate_reconnect.go" "reconnect" "post-reconnect delivery"

echo
if [ "$fails" -eq 0 ]; then
  echo "sentinel_guard_test: all cases passed ✓"
  exit 0
fi
echo "sentinel_guard_test: $fails case(s) failed ✗"
exit 1
