#!/usr/bin/env bash
# Fixture test for e2e_battery_verdict (taskfiles/e2e/stack.sh) — the generic anti-vacuous
# guard the cell runner applies to every validate suite.
#
# Runs OUTSIDE the Docker stack: feeds canned raw `sukko test validate` streams to the guard
# on stdin and asserts its exit code, so the guard's anti-vacuous contract has durable
# regression coverage — not a one-time manual proof (Constitution §VIII). Each non-pass fixture
# isolates ONE of the guard's two load-bearing assertions, so a guard that silently drops an
# assertion is caught here:
#   (a) report .status == "pass"          → status-fail   (the ONLY (b)✓ (a)✗ shape)
#   (b) NO check has status == "skip"      → check-skip    (the ONLY (a)✓ (b)✗ shape)
# plus the structural fail-closed paths: no report line, and a non-JSON report line.
#
# Run: bash taskfiles/e2e/battery_guard_test.sh   (wired into CI before the stack boots).
set -uo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=./stack.sh
. "$SCRIPT_DIR/stack.sh"

fails=0

# run_case <want_exit> <name> <raw-stream>
# Pipes the stream to the guard (suite name "channels"), compares the exit code to <want_exit>.
run_case() {
  local want="$1" name="$2" stream="$3" got
  printf '%s\n' "$stream" | e2e_battery_verdict "channels" >/dev/null 2>&1
  got=$?
  if [ "$got" -eq "$want" ]; then
    echo "ok   - $name (exit $got)"
  else
    echo "FAIL - $name: exit $got, want $want"
    fails=$((fails + 1))
  fi
}

# A leading progress line (no "test_type") verifies the guard's grep|tail -1 extraction
# selects the report line, not stray output.
PROGRESS='{"event":"progress","msg":"running channels suite"}'

# 1. Fully-green run → guard passes (exit 0).
run_case 0 "pass" "$PROGRESS
{\"test_type\":\"validate\",\"suite\":\"channels\",\"status\":\"pass\",\"checks\":[{\"name\":\"subscribe\",\"status\":\"pass\"},{\"name\":\"unsubscribe\",\"status\":\"pass\"}]}"

# 2. A check failed → report status=fail. ISOLATES assertion (a): without it a failed suite
#    would ship green.
run_case 1 "status-fail" "$PROGRESS
{\"test_type\":\"validate\",\"suite\":\"channels\",\"status\":\"fail\",\"checks\":[{\"name\":\"subscribe\",\"status\":\"pass\"},{\"name\":\"unsubscribe\",\"status\":\"fail\"}]}"

# 3. A check skipped but report status still pass (a skip leaves .status=pass) → ISOLATES
#    assertion (b): the only shape where (a)✓ but (b)✗. Without it, silently vanished coverage
#    reads as green.
run_case 1 "check-skip" "$PROGRESS
{\"test_type\":\"validate\",\"suite\":\"channels\",\"status\":\"pass\",\"checks\":[{\"name\":\"subscribe\",\"status\":\"pass\"},{\"name\":\"unsubscribe\",\"status\":\"skip\"}]}"

# 4. No report line at all (suite aborted / connect failure before emitting a report) → guard
#    fails closed.
run_case 1 "no-report" "$PROGRESS
{\"event\":\"progress\",\"msg\":\"still connecting\"}"

# 5. Report line present but not valid JSON (truncated/garbled stream) → guard fails closed
#    rather than treating an unparseable report as a pass.
run_case 1 "invalid-json" "$PROGRESS
{\"test_type\":\"validate\",\"suite\":\"channels\",\"status\":\"pass\",\"checks\":[ THIS IS NOT JSON"

echo
if [ "$fails" -eq 0 ]; then
  echo "battery_guard_test: all cases passed ✓"
  exit 0
fi
echo "battery_guard_test: $fails case(s) failed ✗"
exit 1
