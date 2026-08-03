//go:build sukko_e2e

package license

import _ "embed"

// embeddedPublicKeyBytes under the `sukko_e2e` build tag is the regenerated dev/e2e
// public key (keys/sukko.dev.pub), whose private half (keys/sukko.dev.key) genkeys
// emits for the tester/gentoken signer. This lets the e2e harness validate license
// keys it mints at runtime while the committed production key (keys/sukko.pub) stays
// pristine for the FR-007 embedded-key proof — an explicit, mutually-exclusive
// compile-time embed selection, not a runtime fallback (FR-017, §XV).
//
// keys/sukko.dev.pub is gitignored and produced by `go run ./genkeys`; the e2e build
// runs genkeys before compiling (taskfiles/e2e), so it exists at build time.
//
//go:embed keys/sukko.dev.pub
var embeddedPublicKeyBytes []byte
