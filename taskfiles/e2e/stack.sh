#!/usr/bin/env bash
# Shared setup helpers for the e2e stack targets in taskfiles/e2e.yml
# (the `cell` runner, kafka-ingest, push-validate). Sourced — do not execute directly.
#
# Constitution §X: the license/admin-key/boot/readiness/battery blocks are defined
# once here rather than copy-pasted per target. The parametrized cell runner
# (e2e_boot_cell + e2e_readiness_gate + e2e_kafka_ready + e2e_run_battery) is the
# single source of truth for booting an E2E (edition, backend, suites) cell.

# e2e_mint_license <edition> <org> <expiry>
# Ensures the dev signing key exists, mints a dev license token for the given
# edition with the given expiry, and echoes the token. Must run BEFORE boot:
# edition-gated backends (e.g. MESSAGE_BACKEND=kafka) fail-fast at startup without
# the right edition, so `sukko license set` after boot is not usable. <expiry> is
# passed verbatim to `gentoken --expires` (e.g. +1y for a valid cell, -1d for the
# expired→Community degradation cell); it has no default here — the caller's Task
# var owns it (§I single-source).
e2e_mint_license() {
  local edition="$1" org="$2" expiry="$3"
  if [ ! -f "ws/internal/shared/license/keys/sukko.dev.key" ]; then
    (cd ws && go run ./internal/shared/license/genkeys) >&2
  fi
  (cd ws && go run ./internal/shared/license/gentoken \
    --key internal/shared/license/keys/sukko.dev.key \
    --edition "$edition" --org "$org" --expires "$expiry")
}

# e2e_gen_admin_key <abs_path>
# Generates the tester admin keypair: raw 64-byte private key written to
# <abs_path> (mounted into the tester by the compose override), base64 public
# key echoed (wired to provisioning as ADMIN_BOOTSTRAP_KEY).
e2e_gen_admin_key() {
  local path="$1"
  (cd ws && go run ./cmd/gen-admin-key "$path")
}

# e2e_readiness_gate <provisioning_url> <want_edition> <want_expired>
# Asserts provisioning /edition reports the expected edition AND expired flag before
# any suite runs. Fails CLOSED (§XV/§XVIII) — a stack running the wrong edition, or a
# valid-license cell silently running on an expired/degraded license, would skip or
# misvalidate edition-gated suites, and a silent skip is indistinguishable from green.
# Both fields are validated for presence AND type (string edition, boolean expired);
# an unreachable, non-JSON, or malformed /edition is a failure, never a defaulted pass.
# The expired→Community downgrade is resolved synchronously at license load and .expired
# is recomputed per request, so this asserts ONCE with no retry (a retry would mask a
# genuinely mis-configured `exp`).
e2e_readiness_gate() {
  local prov_url="$1" want_edition="$2" want_expired="$3" body edition expired
  if ! body=$(curl -sf "$prov_url/edition"); then
    echo "FAIL: /edition unreachable at $prov_url" >&2
    return 1
  fi
  if ! echo "$body" | jq empty 2>/dev/null; then
    echo "FAIL: /edition returned non-JSON body" >&2
    return 1
  fi
  # Fail closed if either field is absent or the wrong JSON type (never treat an absent
  # field as its zero value). jq -e sets exit status from the boolean result.
  if ! echo "$body" | jq -e '(.edition | type) == "string"' >/dev/null 2>&1; then
    echo "FAIL: /edition .edition absent or not a string" >&2
    return 1
  fi
  if ! echo "$body" | jq -e '(.expired | type) == "boolean"' >/dev/null 2>&1; then
    echo "FAIL: /edition .expired absent or not a boolean" >&2
    return 1
  fi
  edition=$(echo "$body" | jq -r '.edition')
  expired=$(echo "$body" | jq -r '.expired')
  if [ "$edition" != "$want_edition" ]; then
    echo "FAIL: expected edition=$want_edition, got $edition" >&2
    return 1
  fi
  if [ "$expired" != "$want_expired" ]; then
    echo "FAIL: expected expired=$want_expired, got $expired" >&2
    return 1
  fi
  echo "  edition: $edition, expired: $expired ✓"
}

# e2e_boot_cell <edition> <backend> <admin_key_abs_path> <expiry> <compose_cmd...>
# The single boot block for an E2E cell. Derives license from the edition
# (Community sets NO key — the no-license default; Pro/Enterprise mint an
# edition-scoped token BEFORE boot with the given <expiry>, since edition-gated
# backends fail-fast at startup), generates the tester admin key, and boots the
# build-from-source stack with the requested message backend. `<compose_cmd...>`
# is the full compose invocation (base + backend override [+ profiles]) passed as
# trailing words. The `--wait` is bounded by E2E_WAIT_TIMEOUT (default 300s) so a
# never-healthy dependency cannot stretch the job toward the CI limit — the
# boot-refusal helper caps it shorter.
e2e_boot_cell() {
  local edition="$1" backend="$2" admin_key_path="$3" expiry="$4"
  shift 4
  local token="" admin_key
  if [ "$edition" != "community" ]; then
    echo "=== Mint $edition token, expiry=$expiry (before boot) ===" >&2
    token=$(e2e_mint_license "$edition" "E2E ${edition}" "$expiry")
  else
    echo "=== Community cell — no license key ===" >&2
  fi
  admin_key=$(e2e_gen_admin_key "$admin_key_path")
  echo "=== Boot $backend-mode stack (build from source, edition=$edition) ===" >&2
  # SUKKO_LICENSE_KEY empty ⇒ Community. MESSAGE_BACKEND=direct equals the Go
  # default (a valid, non-empty value — safe through the compose bare-key passthrough).
  MESSAGE_BACKEND="$backend" \
  SUKKO_LICENSE_KEY="$token" \
  ADMIN_BOOTSTRAP_KEY="$admin_key" \
  CREDENTIALS_ENCRYPTION_KEY="$(openssl rand -hex 32)" \
  WEBHOOK_INTERNAL_TOKEN="$(openssl rand -hex 24)" \
  "$@" up -d --build --wait --wait-timeout "${E2E_WAIT_TIMEOUT:-300}"
}

# e2e_kafka_ready <compose_cmd...>
# Kafka-backend readiness gate (in addition to e2e_readiness_gate's /edition check):
# asserts Redpanda cluster health before any suite runs. This mirrors the proven,
# CI-green kafka-ingest readiness (§XVIII).
#
# NOTE: it deliberately does NOT gate on ws-server /ready. /ready is the #179 *control-plane*
# registry-snapshot gate (topic→tenant map applied) — it is NOT the *data-plane* Kafka consumer
# partition-assignment signal, so it never addressed the consumer-subscription-timing race it was
# first added for. That race is absorbed the same way kafka-ingest absorbs it: each validate suite
# provisions its own throwaway tenant and waits out delivery on the tester side. Gating on /ready
# here only added a failure mode (a cold build-from-source stack can take >60s to apply the first
# snapshot) without buying determinism.
e2e_kafka_ready() {
  echo "=== Kafka readiness: rpk cluster health ===" >&2
  "$@" exec -T redpanda rpk cluster health
}

# assert_boot_refused_verdict <ws_server_exit_code>   (ws-server logs on STDIN)
# Pure, stdin/arg-driven classifier for the boot-refusal (negative) cell — mirrors
# e2e_battery_verdict/push_validate_guard so its non-vacuous behavior is unit-testable
# with canned fixtures (no real refused boot needed). Passes IFF BOTH hold:
#   (a) the ws-server container exited with a NON-ZERO code (a numeric, non-zero exit —
#       empty/non-numeric means the container state could not be read → fail closed), AND
#   (b) the captured logs contain the edition-gate substring `requires pro edition`.
# A non-zero `up` alone is vacuous (a build error, a different service crash, or a --wait
# timeout also exits non-zero); only exit≠0 AND the gate error is a real gate refusal.
assert_boot_refused_verdict() {
  local exit_code="$1" logs gate="requires pro edition"
  logs=$(cat)
  # Fail closed unless exit_code is a non-zero integer. POSIX `case` (not [[ =~ ]]) so the
  # check is portable across the Task runner's shell interpreter as well as bash.
  case "$exit_code" in
    '' | *[!0-9]*) # empty or non-numeric — container state could not be read
      echo "FAIL: boot-refusal: ws-server exit code=${exit_code:-<none>} (want a non-zero exited container — the gate did not refuse boot)" >&2
      return 1 ;;
  esac
  if [ "$exit_code" -eq 0 ]; then
    echo "FAIL: boot-refusal: ws-server exit code=0 (want a non-zero exited container — the gate did not refuse boot)" >&2
    return 1
  fi
  if ! printf '%s' "$logs" | grep -q "$gate"; then
    echo "FAIL: boot-refusal: ws-server exited ($exit_code) but logs lack '$gate' (refused for the WRONG reason — vacuous negative)" >&2
    return 1
  fi
  echo "  boot-refusal: ws-server exited $exit_code with the edition-gate error ✓"
}

# e2e_assert_boot_refused <edition> <backend> <admin_key_abs_path> <expiry> <compose_cmd...>
# Asserts a cell EXPECTED to refuse startup (Community + kafka) fails to boot for the
# RIGHT reason. Reuses e2e_boot_cell (single boot source, §X) with a capped --wait so a
# never-healthy dependency (ws-gateway waits on ws-server health) cannot stretch the job;
# FAILs if the boot SUCCEEDS; otherwise captures the ws-server container's exit code and
# logs BEFORE teardown and delegates the verdict to assert_boot_refused_verdict. The caller
# is responsible for `defer`-ing teardown (down -v) — evidence capture must precede it.
e2e_assert_boot_refused() {
  local edition="$1" backend="$2" admin_key_path="$3" expiry="$4"
  shift 4
  local rc=0 cid exit_code="" logs
  echo "=== Boot-refusal cell: expect $edition/$backend to REFUSE startup ===" >&2
  # Capture the boot's exit WITHOUT aborting: `|| rc=$?` suppresses errexit for the tested
  # command (and inside the function it invokes) — no global `set +e`/`set -e` toggle, and no
  # `$-` inspection (the Task runner's shell interpreter does not expose `$-`, and reading it
  # under `set -u` aborts with "-: unbound variable"). Cap the wait so a never-healthy
  # dependency (ws-gateway waits on ws-server health) cannot stretch the job.
  E2E_WAIT_TIMEOUT="${E2E_REFUSAL_WAIT_TIMEOUT:-120}" \
    e2e_boot_cell "$edition" "$backend" "$admin_key_path" "$expiry" "$@" || rc=$?
  if [ "$rc" -eq 0 ]; then
    echo "FAIL: boot-refusal: stack came up (exit 0) — the edition gate did NOT refuse boot" >&2
    return 1
  fi
  # Capture ws-server exit code + logs BEFORE the caller's deferred `down -v`. Use `ps -aq`
  # so the exited container is still listed; inspect its State.ExitCode directly (not the
  # aggregate `up` exit, which conflates dependency failures).
  cid=$("$@" ps -aq ws-server 2>/dev/null | tail -1)
  if [ -n "$cid" ]; then
    exit_code=$(docker inspect -f '{{.State.ExitCode}}' "$cid" 2>/dev/null || true)
  fi
  logs=$("$@" logs ws-server 2>&1 || true)
  printf '%s' "$logs" | assert_boot_refused_verdict "${exit_code:-}"
}

# _e2e_suite_scoped_checks <list> <want_suite>
# Splits a ;-separated list of `<suite>:<check name>` entries and echoes (one per line)
# the <check name>s whose <suite> equals <want_suite>. Entry separator is `;` because
# check names contain spaces (e.g. "connection limit rejection"); suite and check names
# never contain `;`. Empty entries are ignored. Used to scope both the skip allow-list
# and the require-pass list to the suite currently being judged, so a name allowed for
# one suite is never silently tolerated in another.
_e2e_suite_scoped_checks() {
  local list="$1" want_suite="$2" entry esuite echeck
  local IFS=';'
  for entry in $list; do
    [ -n "$entry" ] || continue
    esuite="${entry%%:*}"
    echeck="${entry#*:}"
    if [ "$esuite" = "$want_suite" ]; then
      printf '%s\n' "$echeck"
    fi
  done
}

# e2e_battery_verdict <suite_name> <allowed_skips> <require_pass>   (raw stream on STDIN)
# The generic anti-vacuous guard for one validate suite: extracts the final report JSON
# line (progress lines ignored) and returns 0 IFF ALL hold: the report .status is "pass",
# every skipped check is declared for THIS suite in <allowed_skips>, and every check named
# for THIS suite in <require_pass> is present AND "pass". Any undeclared skip, any
# fail/error, or a required check that is absent/skipped fails the cell. <allowed_skips>
# and <require_pass> are ;-separated `<suite>:<check>` lists (default empty). Prints a
# one-line verdict. Kept stdin-driven so it is testable with canned fixtures
# (battery_guard_test.sh) without booting a stack.
e2e_battery_verdict() {
  local suite="$1" allowed_skips="${2:-}" require_pass="${3:-}"
  local report status fails errs allowed required skip_names undeclared="" rq st missing=""
  report=$(grep '"test_type"' | tail -1)
  if [ -z "$report" ]; then
    echo "    $suite: NO REPORT (suite emitted no test_type report)" >&2
    return 1
  fi
  if ! echo "$report" | jq empty 2>/dev/null; then
    echo "    $suite: report is not valid JSON" >&2
    return 1
  fi
  status=$(echo "$report" | jq -r '.status // "missing"')
  # Include each failed check's .error — the compose stack is torn down with -v, so this
  # line is the only surviving evidence of WHY a check failed.
  fails=$(echo "$report" | jq -r '[.checks[]? | select(.status=="fail") | .name + (if (.error // "") != "" then " (" + .error + ")" else "" end)] | join(", ")')
  # status=="error" reports (setup/dispatch failure before any check ran) carry the cause
  # in the top-level .errors array, not in .checks — print it or the run is undiagnosable.
  errs=$(echo "$report" | jq -r '(.errors // []) | join("; ")')
  # Fails on any fail/error (status != pass), regardless of the allow-list.
  if [ "$status" != "pass" ]; then
    echo "    $suite: $status (failed checks: ${fails:-<none listed>}${errs:+; errors: $errs})" >&2
    return 1
  fi
  # Skips: tolerate ONLY those whose <suite>:<check> is declared for this suite.
  allowed=$(_e2e_suite_scoped_checks "$allowed_skips" "$suite")
  skip_names=$(echo "$report" | jq -r '.checks[]? | select(.status=="skip") | .name')
  while IFS= read -r sk; do
    [ -n "$sk" ] || continue
    if ! printf '%s\n' "$allowed" | grep -Fxq "$sk"; then
      undeclared="$undeclared${undeclared:+, }$sk"
    fi
  done <<< "$skip_names"
  if [ -n "$undeclared" ]; then
    echo "    $suite: UNDECLARED SKIPPED CHECKS: $undeclared" >&2
    return 1
  fi
  # Require-pass: each check named for this suite MUST be present AND pass (present-and-pass,
  # mirrors push_validate_guard). Guards against a vacuous green where a required boundary
  # check is legitimately absent on some editions (so it can't live in the shared verdict).
  required=$(_e2e_suite_scoped_checks "$require_pass" "$suite")
  while IFS= read -r rq; do
    [ -n "$rq" ] || continue
    st=$(echo "$report" | jq -r --arg n "$rq" 'first(.checks[]? | select(.name==$n) | .status) // "absent"')
    if [ "$st" != "pass" ]; then
      missing="$missing${missing:+, }$rq=$st"
    fi
  done <<< "$required"
  if [ -n "$missing" ]; then
    echo "    $suite: REQUIRED-PASS CHECKS not present-and-pass: $missing" >&2
    return 1
  fi
  echo "    $suite: pass ✓"
}

# e2e_run_battery <tester_token> <allowed_skips> <require_pass> <suite...>
# Runs each suite hermetically (isolated HOME/XDG_CONFIG_HOME so no ambient CLI
# context leaks in and suppresses the throwaway-tenant auto-create) and applies
# e2e_battery_verdict to each. <allowed_skips> and <require_pass> are ;-separated
# `<suite>:<check>` lists (default empty), passed as dedicated NAMED leading args so
# they are unambiguously separated from the variadic suite list; each verdict scopes
# them to its own suite. All suites run even after one fails; returns non-zero listing
# the failed suites. The report is parsed rather than trusting the CLI exit code — the
# extraction pipe masks it.
e2e_run_battery() {
  local tester_token="$1" allowed_skips="$2" require_pass="$3"
  shift 3
  local suites=("$@")
  local tmpcfg failed="" suite rc
  tmpcfg=$(mktemp -d)
  for suite in "${suites[@]}"; do
    echo "--- suite: $suite ---"
    rc=0
    HOME="$tmpcfg" XDG_CONFIG_HOME="$tmpcfg/.config" SUKKO_TESTER_TOKEN="$tester_token" \
      sukko test validate --suite "$suite" --follow --output json 2>/dev/null \
      | e2e_battery_verdict "$suite" "$allowed_skips" "$require_pass" || rc=$?
    [ "$rc" -eq 0 ] || failed="$failed $suite"
  done
  rm -rf "$tmpcfg"
  if [ -n "$failed" ]; then
    echo "=== battery FAILED:$failed ===" >&2
    return 1
  fi
  echo "=== battery PASSED (${suites[*]}) ==="
}

# push_validate_guard
# Anti-vacuous-green guard for the `push` delivery suite. Reads the raw `sukko test
# validate` stream on STDIN, extracts the final report JSON line (progress lines are
# ignored), and asserts ALL THREE conditions — omitting any one lets a real regression
# ship green:
#   (a) the "push delivery" check is present AND status=="pass". Catches a receiver-host
#       skip (emitted status:"skip" → not "pass") AND a pre-delivery abort / early
#       "push available" 503/403 skip (the check is absent — the suite returns before
#       appending it).
#   (b) NO check has status=="skip" (empty allow-list). Defensive backstop for any future
#       check that skips WITHOUT early-returning; in the current suite (a) already catches
#       every skip path.
#   (c) the report .status field is "pass". The load-bearing backstop for the append-only
#       back half of the suite (credential/channel-config CRUD, subscribe, multiprovider),
#       which only pass/fail: a back-half failure leaves "push delivery":pass + no skip but
#       report .status:"fail" (validate.go computes .status=fail iff any check failed; a
#       skip leaves it "pass"). Without (c) the guard exits 0 on a failed back-half check.
# Exits non-zero with a clear message (naming any failing/skipped checks) on any breach.
push_validate_guard() {
  local report status delivery skips fails
  report=$(grep '"test_type"' | tail -1)
  if [ -z "$report" ]; then
    echo "FAIL: push-validate guard: no report line (suite emitted no test_type report — pre-report abort)" >&2
    return 1
  fi
  if ! echo "$report" | jq empty 2>/dev/null; then
    echo "FAIL: push-validate guard: report is not valid JSON" >&2
    return 1
  fi
  # Keep .error in the printed summary — dropping it makes a red run undiagnosable from CI
  # logs alone (the stack is torn down with -v, so this line is the only surviving evidence).
  echo "$report" | jq '{status, checks:[.checks[]? | {name,status} + (if (.error // "") != "" then {error:.error} else {} end)]}'
  status=$(echo "$report" | jq -r '.status // "missing"')
  delivery=$(echo "$report" | jq -r '.checks[]? | select(.name=="push delivery") | .status')
  skips=$(echo "$report" | jq -r '[.checks[]? | select(.status=="skip") | .name] | join(",")')
  fails=$(echo "$report" | jq -r '[.checks[]? | select(.status=="fail") | .name + (if (.error // "") != "" then " (" + .error + ")" else "" end)] | join(", ")')
  # (a) delivery present AND pass
  if [ "$delivery" != "pass" ]; then
    echo "FAIL: push-validate guard: 'push delivery' status=${delivery:-<absent>} (want pass)" >&2
    return 1
  fi
  # (b) no skipped checks (empty allow-list)
  if [ -n "$skips" ]; then
    echo "FAIL: push-validate guard: skipped checks (empty allow-list): $skips" >&2
    return 1
  fi
  # (c) report status pass (backstops the append-only back-half checks)
  if [ "$status" != "pass" ]; then
    echo "FAIL: push-validate guard: report status=$status (failed checks: ${fails:-<none listed>})" >&2
    return 1
  fi
  echo "  push-validate guard: delivery=pass, no skips, report=pass ✓"
}
