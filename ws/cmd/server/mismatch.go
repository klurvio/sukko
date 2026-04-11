package main

import "github.com/klurvio/sukko/internal/shared/license"

// checkBackendMismatch returns true if the configured message backend
// is not supported by the current edition. Uses the same feature gate
// logic as startup validation in server_config.go.
func checkBackendMismatch(backend string, edition license.Edition) bool {
	switch backend {
	case "kafka":
		return !license.EditionHasFeature(edition, license.KafkaBackend)
	case "nats":
		return !license.EditionHasFeature(edition, license.NATSJetStreamBackend)
	default: // "direct" — always compatible
		return false
	}
}
