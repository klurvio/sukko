#!/usr/bin/env bash
# Shared setup helpers for the e2e stack targets in taskfiles/e2e.yml
# (kafka-ingest, validate-battery). Sourced — do not execute directly.
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
