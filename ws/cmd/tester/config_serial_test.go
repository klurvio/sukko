// Serial tests — uses os.Unsetenv which modifies global process state. Do NOT add t.Parallel() to any test in this file.
package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/caarlos0/env/v11"

	"github.com/klurvio/sukko/internal/shared/platform"
)

// TestTesterConfig_MessageBackendBase_EnvTag asserts that platform.MessageBackendBase
// carries the MESSAGE_BACKEND env tag — guards against accidental field rename or tag removal.
func TestTesterConfig_MessageBackendBase_EnvTag(t *testing.T) {
	field, ok := reflect.TypeFor[platform.MessageBackendBase]().FieldByName("MessageBackend")
	if !ok {
		t.Fatal("platform.MessageBackendBase has no field MessageBackend")
	}
	if got := field.Tag.Get("env"); got != "MESSAGE_BACKEND" {
		t.Errorf("MessageBackend env tag = %q, want %q", got, "MESSAGE_BACKEND")
	}
}

// TestTesterConfig_KafkaBrokers_EnvDefault asserts the KAFKA_BROKERS envDefault is "localhost:19092"
// — guards against drift between MessageBackendBase.KafkaBrokers and the documented default.
func TestTesterConfig_KafkaBrokers_EnvDefault(t *testing.T) {
	field, ok := reflect.TypeFor[platform.MessageBackendBase]().FieldByName("KafkaBrokers")
	if !ok {
		t.Fatal("platform.MessageBackendBase has no field KafkaBrokers")
	}
	if got := field.Tag.Get("envDefault"); got != "localhost:19092" {
		t.Errorf("KafkaBrokers envDefault = %q, want %q", got, "localhost:19092")
	}
}

// TestTesterConfig_KafkaBrokers_Default verifies that when KAFKA_BROKERS is unset,
// the parsed TesterConfig.KafkaBrokers equals the envDefault "localhost:19092".
func TestTesterConfig_KafkaBrokers_Default(t *testing.T) {
	os.Unsetenv("KAFKA_BROKERS")
	var base platform.MessageBackendBase
	if err := env.Parse(&base); err != nil {
		t.Fatalf("env.Parse failed: %v", err)
	}
	if base.KafkaBrokers != "localhost:19092" {
		t.Errorf("KafkaBrokers default = %q, want %q", base.KafkaBrokers, "localhost:19092")
	}
}
