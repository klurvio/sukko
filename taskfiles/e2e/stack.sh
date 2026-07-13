#!/usr/bin/env bash
# Shared setup helpers for the e2e stack targets in taskfiles/e2e.yml
# (kafka-ingest, validate-battery, push-validate). Sourced — do not execute directly.
#
# Constitution §X: the license/admin-key/readiness boot blocks are defined once
# here rather than copy-pasted per target.

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
  echo "$report" | jq '{status, checks:[.checks[]?|{name,status}]}'
  status=$(echo "$report" | jq -r '.status // "missing"')
  delivery=$(echo "$report" | jq -r '.checks[]? | select(.name=="push delivery") | .status')
  skips=$(echo "$report" | jq -r '[.checks[]? | select(.status=="skip") | .name] | join(",")')
  fails=$(echo "$report" | jq -r '[.checks[]? | select(.status=="fail") | .name] | join(",")')
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
