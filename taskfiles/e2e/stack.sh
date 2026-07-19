#!/usr/bin/env bash
# Shared setup helpers for the e2e stack targets in taskfiles/e2e.yml
# (the `cell` runner, kafka-ingest, push-validate). Sourced — do not execute directly.
#
# Constitution §X: the license/admin-key/boot/readiness/battery blocks are defined
# once here rather than copy-pasted per target. The parametrized cell runner
# (e2e_boot_cell + e2e_readiness_gate + e2e_kafka_ready + e2e_run_battery) is the
# single source of truth for booting an E2E (edition, backend, suites) cell.

# e2e_mint_license <edition> <org>
# Ensures the dev signing key exists, mints a dev license token for the given
# edition, and echoes the token. Must run BEFORE boot: edition-gated backends
# (e.g. MESSAGE_BACKEND=kafka) fail-fast at startup without the right edition,
# so `sukko license set` after boot is not usable.
e2e_mint_license() {
  local edition="$1" org="$2"
  if [ ! -f "ws/internal/shared/license/keys/sukko.dev.key" ]; then
    (cd ws && go run ./internal/shared/license/genkeys) >&2
  fi
  (cd ws && go run ./internal/shared/license/gentoken \
    --key internal/shared/license/keys/sukko.dev.key \
    --edition "$edition" --org "$org" --expires +1y)
}

# e2e_gen_admin_key <abs_path>
# Generates the tester admin keypair: raw 64-byte private key written to
# <abs_path> (mounted into the tester by the compose override), base64 public
# key echoed (wired to provisioning as ADMIN_BOOTSTRAP_KEY).
e2e_gen_admin_key() {
  local path="$1"
  (cd ws && go run ./cmd/gen-admin-key "$path")
}

# e2e_readiness_gate <provisioning_url> <want_edition>
# Asserts provisioning reports the expected edition. Fails loudly on mismatch —
# a stack running the wrong edition would skip or misvalidate edition-gated
# suites, and a silent skip is indistinguishable from green.
e2e_readiness_gate() {
  local prov_url="$1" want="$2" edition
  edition=$(curl -sf "$prov_url/edition" | jq -r '.edition')
  if [ "$edition" != "$want" ]; then
    echo "FAIL: expected edition=$want, got ${edition:-<empty>}" >&2
    return 1
  fi
  echo "  edition: $edition ✓"
}

# e2e_boot_cell <edition> <backend> <admin_key_abs_path> <compose_cmd...>
# The single boot block for an E2E cell. Derives license from the edition
# (Community sets NO key — the no-license default; Pro/Enterprise mint an
# edition-scoped token BEFORE boot, since edition-gated backends fail-fast at
# startup), generates the tester admin key, and boots the build-from-source stack
# with the requested message backend. `<compose_cmd...>` is the full compose
# invocation (base + backend override [+ profiles]) passed as trailing words.
e2e_boot_cell() {
  local edition="$1" backend="$2" admin_key_path="$3"
  shift 3
  local token="" admin_key
  if [ "$edition" != "community" ]; then
    echo "=== Mint $edition token (before boot) ===" >&2
    token=$(e2e_mint_license "$edition" "E2E ${edition}")
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
  "$@" up -d --build --wait
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

# e2e_battery_verdict <suite_name>   (raw `sukko test validate` stream on STDIN)
# The generic anti-vacuous guard for one validate suite: extracts the final report
# JSON line (progress lines ignored) and returns 0 IFF the report .status is "pass"
# AND no check has status "skip" (empty skip allow-list — a skip in CI means coverage
# silently vanished). Prints a one-line verdict. Kept stdin-driven so it is testable
# with canned fixtures (battery_guard_test.sh) without booting a stack.
e2e_battery_verdict() {
  local suite="$1" report status skips fails errs
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
  skips=$(echo "$report" | jq -r '[.checks[]? | select(.status=="skip") | .name] | join(",")')
  # Include each failed check's .error — the compose stack is torn down with -v, so this
  # line is the only surviving evidence of WHY a check failed.
  fails=$(echo "$report" | jq -r '[.checks[]? | select(.status=="fail") | .name + (if (.error // "") != "" then " (" + .error + ")" else "" end)] | join(", ")')
  # status=="error" reports (setup/dispatch failure before any check ran) carry the cause
  # in the top-level .errors array, not in .checks — print it or the run is undiagnosable.
  errs=$(echo "$report" | jq -r '(.errors // []) | join("; ")')
  if [ "$status" != "pass" ]; then
    echo "    $suite: $status (failed checks: ${fails:-<none listed>}${errs:+; errors: $errs})" >&2
    return 1
  fi
  if [ -n "$skips" ]; then
    echo "    $suite: SKIPPED CHECKS (empty allow-list): $skips" >&2
    return 1
  fi
  echo "    $suite: pass ✓"
}

# e2e_run_battery <tester_token> <suite...>
# Runs each suite hermetically (isolated HOME/XDG_CONFIG_HOME so no ambient CLI
# context leaks in and suppresses the throwaway-tenant auto-create) and applies
# e2e_battery_verdict to each. All suites run even after one fails; returns non-zero
# listing the failed suites. The report is parsed rather than trusting the CLI exit
# code — the extraction pipe masks it.
e2e_run_battery() {
  local tester_token="$1"
  shift
  local suites=("$@")
  local tmpcfg failed="" suite rc
  tmpcfg=$(mktemp -d)
  for suite in "${suites[@]}"; do
    echo "--- suite: $suite ---"
    rc=0
    HOME="$tmpcfg" XDG_CONFIG_HOME="$tmpcfg/.config" SUKKO_TESTER_TOKEN="$tester_token" \
      sukko test validate --suite "$suite" --follow --output json 2>/dev/null \
      | e2e_battery_verdict "$suite" || rc=$?
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
