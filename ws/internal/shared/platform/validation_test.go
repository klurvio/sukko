package platform

import (
	"strings"
	"testing"
)

// =============================================================================
// ValidLogLevels Map Tests
// =============================================================================

func TestValidLogLevels_Contents(t *testing.T) {
	t.Parallel()
	expectedLevels := []string{"debug", "info", "warn", "error"}

	for _, level := range expectedLevels {
		if !ValidLogLevels[level] {
			t.Errorf("ValidLogLevels[%q] = false, want true", level)
		}
	}

	// Verify map size
	if len(ValidLogLevels) != len(expectedLevels) {
		t.Errorf("len(ValidLogLevels) = %d, want %d", len(ValidLogLevels), len(expectedLevels))
	}
}

func TestValidLogLevels_InvalidLevels(t *testing.T) {
	t.Parallel()
	invalidLevels := []string{"DEBUG", "INFO", "WARN", "ERROR", "trace", "fatal", ""}

	for _, level := range invalidLevels {
		if ValidLogLevels[level] {
			t.Errorf("ValidLogLevels[%q] = true, want false", level)
		}
	}
}

// =============================================================================
// ValidLogFormats Map Tests
// =============================================================================

func TestValidLogFormats_Contents(t *testing.T) {
	t.Parallel()
	expectedFormats := []string{"json", "text", "pretty"}

	for _, format := range expectedFormats {
		if !ValidLogFormats[format] {
			t.Errorf("ValidLogFormats[%q] = false, want true", format)
		}
	}

	// Verify map size
	if len(ValidLogFormats) != len(expectedFormats) {
		t.Errorf("len(ValidLogFormats) = %d, want %d", len(ValidLogFormats), len(expectedFormats))
	}
}

func TestValidLogFormats_InvalidFormats(t *testing.T) {
	t.Parallel()
	invalidFormats := []string{"JSON", "TEXT", "PRETTY", "xml", "console", ""}

	for _, format := range invalidFormats {
		if ValidLogFormats[format] {
			t.Errorf("ValidLogFormats[%q] = true, want false", format)
		}
	}
}

// =============================================================================
// ValidKafkaSASLMechanisms Map Tests
// =============================================================================

func TestValidKafkaSASLMechanisms_Contents(t *testing.T) {
	t.Parallel()
	expectedMechanisms := []string{"scram-sha-256", "scram-sha-512"}

	for _, mechanism := range expectedMechanisms {
		if !ValidKafkaSASLMechanisms[mechanism] {
			t.Errorf("ValidKafkaSASLMechanisms[%q] = false, want true", mechanism)
		}
	}

	// Verify map size
	if len(ValidKafkaSASLMechanisms) != len(expectedMechanisms) {
		t.Errorf("len(ValidKafkaSASLMechanisms) = %d, want %d", len(ValidKafkaSASLMechanisms), len(expectedMechanisms))
	}
}

func TestValidKafkaSASLMechanisms_InvalidMechanisms(t *testing.T) {
	t.Parallel()
	invalidMechanisms := []string{"SCRAM-SHA-256", "SCRAM-SHA-512", "plain", "PLAIN", "oauthbearer", ""}

	for _, mechanism := range invalidMechanisms {
		if ValidKafkaSASLMechanisms[mechanism] {
			t.Errorf("ValidKafkaSASLMechanisms[%q] = true, want false", mechanism)
		}
	}
}

// =============================================================================
// ValidateLogLevel Tests
// =============================================================================

func TestValidateLogLevel_ValidLevels(t *testing.T) {
	t.Parallel()
	validLevels := []string{"debug", "info", "warn", "error"}

	for _, level := range validLevels {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			err := ValidateLogLevel(level)
			if err != nil {
				t.Errorf("ValidateLogLevel(%q) = %v, want nil", level, err)
			}
		})
	}
}

func TestValidateLogLevel_InvalidLevels(t *testing.T) {
	t.Parallel()
	invalidLevels := []string{"DEBUG", "trace", "fatal", "invalid", "", "Info", "WARNING"}

	for _, level := range invalidLevels {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			err := ValidateLogLevel(level)
			if err == nil {
				t.Errorf("ValidateLogLevel(%q) = nil, want error", level)
			}
		})
	}
}

func TestValidateLogLevel_ErrorMessage(t *testing.T) {
	t.Parallel()
	err := ValidateLogLevel("invalid_level")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	errMsg := err.Error()
	// Check that the error message contains useful information
	if !strings.Contains(errMsg, "LOG_LEVEL") {
		t.Errorf("Error message should contain 'LOG_LEVEL', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "invalid_level") {
		t.Errorf("Error message should contain the invalid value, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "debug") {
		t.Errorf("Error message should list valid options, got: %s", errMsg)
	}
}

// =============================================================================
// ValidateLogFormat Tests
// =============================================================================

func TestValidateLogFormat_ValidFormats(t *testing.T) {
	t.Parallel()
	validFormats := []string{"json", "text", "pretty"}

	for _, format := range validFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			err := ValidateLogFormat(format)
			if err != nil {
				t.Errorf("ValidateLogFormat(%q) = %v, want nil", format, err)
			}
		})
	}
}

func TestValidateLogFormat_InvalidFormats(t *testing.T) {
	t.Parallel()
	invalidFormats := []string{"JSON", "xml", "console", "", "Text", "PRETTY"}

	for _, format := range invalidFormats {
		t.Run(format, func(t *testing.T) {
			t.Parallel()
			err := ValidateLogFormat(format)
			if err == nil {
				t.Errorf("ValidateLogFormat(%q) = nil, want error", format)
			}
		})
	}
}

func TestValidateLogFormat_ErrorMessage(t *testing.T) {
	t.Parallel()
	err := ValidateLogFormat("xml")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	errMsg := err.Error()
	// Check that the error message contains useful information
	if !strings.Contains(errMsg, "LOG_FORMAT") {
		t.Errorf("Error message should contain 'LOG_FORMAT', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "xml") {
		t.Errorf("Error message should contain the invalid value, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "json") {
		t.Errorf("Error message should list valid options, got: %s", errMsg)
	}
}

// =============================================================================
// ValidateKafkaSASLMechanism Tests
// =============================================================================

func TestValidateKafkaSASLMechanism_ValidMechanisms(t *testing.T) {
	t.Parallel()
	validMechanisms := []string{"scram-sha-256", "scram-sha-512"}

	for _, mechanism := range validMechanisms {
		t.Run(mechanism, func(t *testing.T) {
			t.Parallel()
			err := ValidateKafkaSASLMechanism(mechanism)
			if err != nil {
				t.Errorf("ValidateKafkaSASLMechanism(%q) = %v, want nil", mechanism, err)
			}
		})
	}
}

func TestValidateKafkaSASLMechanism_InvalidMechanisms(t *testing.T) {
	t.Parallel()
	invalidMechanisms := []string{"SCRAM-SHA-256", "plain", "PLAIN", "oauthbearer", "", "scram-sha-384"}

	for _, mechanism := range invalidMechanisms {
		t.Run(mechanism, func(t *testing.T) {
			t.Parallel()
			err := ValidateKafkaSASLMechanism(mechanism)
			if err == nil {
				t.Errorf("ValidateKafkaSASLMechanism(%q) = nil, want error", mechanism)
			}
		})
	}
}

func TestValidateKafkaSASLMechanism_ErrorMessage(t *testing.T) {
	t.Parallel()
	err := ValidateKafkaSASLMechanism("plain")
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	errMsg := err.Error()
	// Check that the error message contains useful information
	if !strings.Contains(errMsg, "KAFKA_SASL_MECHANISM") {
		t.Errorf("Error message should contain 'KAFKA_SASL_MECHANISM', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "plain") {
		t.Errorf("Error message should contain the invalid value, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "scram-sha-256") {
		t.Errorf("Error message should list valid options, got: %s", errMsg)
	}
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestValidation_WhitespaceInputs(t *testing.T) {
	t.Parallel()
	whitespaceInputs := []string{" ", "  ", "\t", "\n", " debug", "debug "}

	for _, input := range whitespaceInputs {
		t.Run("level_"+input, func(t *testing.T) {
			t.Parallel()
			err := ValidateLogLevel(input)
			if err == nil {
				t.Errorf("ValidateLogLevel(%q) should reject whitespace input", input)
			}
		})
	}
}

func TestValidation_CaseSensitivity(t *testing.T) {
	t.Parallel()
	// All validation is case-sensitive (lowercase only)
	testCases := []struct {
		name     string
		validate func(string) error
		valid    string
		invalid  string
	}{
		{"LogLevel", ValidateLogLevel, "debug", "DEBUG"},
		{"LogFormat", ValidateLogFormat, "json", "JSON"},
		{"KafkaSASL", ValidateKafkaSASLMechanism, "scram-sha-256", "SCRAM-SHA-256"},
	}

	for _, tc := range testCases {
		t.Run(tc.name+"_valid", func(t *testing.T) {
			t.Parallel()
			if err := tc.validate(tc.valid); err != nil {
				t.Errorf("%s(%q) = %v, want nil", tc.name, tc.valid, err)
			}
		})
		t.Run(tc.name+"_invalid", func(t *testing.T) {
			t.Parallel()
			if err := tc.validate(tc.invalid); err == nil {
				t.Errorf("%s(%q) = nil, want error (case-sensitive)", tc.name, tc.invalid)
			}
		})
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkValidateLogLevel_Valid(b *testing.B) {
	for b.Loop() {
		_ = ValidateLogLevel("info")
	}
}

func BenchmarkValidateLogLevel_Invalid(b *testing.B) {
	for b.Loop() {
		_ = ValidateLogLevel("invalid")
	}
}

func BenchmarkValidateLogFormat_Valid(b *testing.B) {
	for b.Loop() {
		_ = ValidateLogFormat("json")
	}
}

func BenchmarkValidateKafkaSASLMechanism_Valid(b *testing.B) {
	for b.Loop() {
		_ = ValidateKafkaSASLMechanism("scram-sha-256")
	}
}

func BenchmarkMapLookup_Direct(b *testing.B) {
	for b.Loop() {
		_ = ValidLogLevels["info"]
	}
}
