package protocol

import (
	"errors"
	"testing"
)

// =============================================================================
// Error Code Constants Tests
// =============================================================================

func TestPublishErrorCode_Values(t *testing.T) {
	t.Parallel()
	expectedCodes := map[PublishErrorCode]string{
		ErrCodeNotAvailable:        "not_available",
		ErrCodeInvalidRequest:      "invalid_request",
		ErrCodeInvalidChannel:      "invalid_channel",
		ErrCodeMessageTooLarge:     "message_too_large",
		ErrCodeRateLimited:         "rate_limited",
		ErrCodePublishFailed:       "publish_failed",
		ErrCodeForbidden:           "forbidden",
		ErrCodeTopicNotProvisioned: "topic_not_provisioned",
		ErrCodeServiceUnavailable:  "service_unavailable",
	}

	for code, expected := range expectedCodes {
		if string(code) != expected {
			t.Errorf("Error code = %q, want %q", string(code), expected)
		}
	}
}

func TestPublishErrorCode_UniqueValues(t *testing.T) {
	t.Parallel()
	codes := []PublishErrorCode{
		ErrCodeNotAvailable,
		ErrCodeInvalidRequest,
		ErrCodeInvalidChannel,
		ErrCodeMessageTooLarge,
		ErrCodeRateLimited,
		ErrCodePublishFailed,
		ErrCodeForbidden,
		ErrCodeTopicNotProvisioned,
		ErrCodeServiceUnavailable,
	}

	seen := make(map[PublishErrorCode]bool)
	for _, code := range codes {
		if seen[code] {
			t.Errorf("Duplicate error code: %q", code)
		}
		seen[code] = true
	}
}

func TestPublishErrorCode_TypeConversion(t *testing.T) {
	t.Parallel()
	// Verify that PublishErrorCode can be converted to/from string
	code := ErrCodeRateLimited
	strVal := string(code)

	if strVal != "rate_limited" {
		t.Errorf("string(ErrCodeRateLimited) = %q, want %q", strVal, "rate_limited")
	}

	// Convert back
	converted := PublishErrorCode(strVal)
	if converted != ErrCodeRateLimited {
		t.Errorf("PublishErrorCode(%q) = %q, want %q", strVal, converted, ErrCodeRateLimited)
	}
}

// =============================================================================
// Error Messages Map Tests
// =============================================================================

func TestPublishErrorMessages_AllCodesHaveMessages(t *testing.T) {
	t.Parallel()
	codes := []PublishErrorCode{
		ErrCodeNotAvailable,
		ErrCodeInvalidRequest,
		ErrCodeInvalidChannel,
		ErrCodeMessageTooLarge,
		ErrCodeRateLimited,
		ErrCodePublishFailed,
		ErrCodeForbidden,
		ErrCodeTopicNotProvisioned,
		ErrCodeServiceUnavailable,
	}

	for _, code := range codes {
		msg, exists := PublishErrorMessages[code]
		if !exists {
			t.Errorf("No message for error code %q", code)
			continue
		}
		if msg == "" {
			t.Errorf("Empty message for error code %q", code)
		}
	}
}

func TestPublishErrorMessages_NoEmptyMessages(t *testing.T) {
	t.Parallel()
	for code, msg := range PublishErrorMessages {
		if msg == "" {
			t.Errorf("Empty message for error code %q", code)
		}
	}
}

func TestPublishErrorMessages_Specific(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		code    PublishErrorCode
		message string
	}{
		{ErrCodeNotAvailable, "Publishing is not enabled on this server"},
		{ErrCodeInvalidRequest, "Invalid publish request format"},
		{ErrCodeInvalidChannel, "Channel must have format: identifier.category"},
		{ErrCodeMessageTooLarge, "Message exceeds maximum size limit"},
		{ErrCodeRateLimited, "Publish rate limit exceeded"},
		{ErrCodePublishFailed, "Failed to publish message"},
		{ErrCodeForbidden, "Not authorized to publish to this channel"},
		{ErrCodeTopicNotProvisioned, "Category is not provisioned for your tenant"},
		{ErrCodeServiceUnavailable, "Service temporarily unavailable, please retry"},
	}

	for _, tc := range testCases {
		t.Run(string(tc.code), func(t *testing.T) {
			t.Parallel()
			msg := PublishErrorMessages[tc.code]
			if msg != tc.message {
				t.Errorf("PublishErrorMessages[%q] = %q, want %q", tc.code, msg, tc.message)
			}
		})
	}
}

// =============================================================================
// Sentinel Errors Tests
// =============================================================================

func TestSentinelErrors_NotNil(t *testing.T) {
	t.Parallel()
	sentinelErrors := []error{
		ErrInvalidChannel,
		ErrTopicNotProvisioned,
		ErrServiceUnavailable,
		ErrProducerClosed,
	}

	for _, err := range sentinelErrors {
		if err == nil {
			t.Error("Sentinel error should not be nil")
		}
	}
}

func TestSentinelErrors_UniqueMessages(t *testing.T) {
	t.Parallel()
	sentinelErrors := []error{
		ErrInvalidChannel,
		ErrTopicNotProvisioned,
		ErrServiceUnavailable,
		ErrProducerClosed,
	}

	seen := make(map[string]bool)
	for _, err := range sentinelErrors {
		msg := err.Error()
		if seen[msg] {
			t.Errorf("Duplicate error message: %q", msg)
		}
		seen[msg] = true
	}
}

func TestSentinelErrors_ErrorInterface(t *testing.T) {
	t.Parallel()
	// Verify all sentinel errors implement the error interface
	var _ = ErrInvalidChannel
	var _ = ErrTopicNotProvisioned
	var _ = ErrServiceUnavailable
	var _ = ErrProducerClosed

	// Verify Error() returns non-empty strings
	testCases := []struct {
		name string
		err  error
	}{
		{"ErrInvalidChannel", ErrInvalidChannel},
		{"ErrTopicNotProvisioned", ErrTopicNotProvisioned},
		{"ErrServiceUnavailable", ErrServiceUnavailable},
		{"ErrProducerClosed", ErrProducerClosed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			msg := tc.err.Error()
			if msg == "" {
				t.Errorf("%s.Error() returned empty string", tc.name)
			}
		})
	}
}

func TestSentinelErrors_SpecificMessages(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		err     error
		message string
	}{
		{ErrInvalidChannel, "invalid channel format"},
		{ErrTopicNotProvisioned, "topic not provisioned"},
		{ErrServiceUnavailable, "service unavailable"},
		{ErrProducerClosed, "producer is closed"},
	}

	for _, tc := range testCases {
		t.Run(tc.message, func(t *testing.T) {
			t.Parallel()
			if tc.err.Error() != tc.message {
				t.Errorf("Error message = %q, want %q", tc.err.Error(), tc.message)
			}
		})
	}
}

func TestSentinelErrors_ErrorsIs(t *testing.T) {
	t.Parallel()
	// Test that errors.Is works correctly with sentinel errors
	testCases := []struct {
		name   string
		target error
	}{
		{"ErrInvalidChannel", ErrInvalidChannel},
		{"ErrTopicNotProvisioned", ErrTopicNotProvisioned},
		{"ErrServiceUnavailable", ErrServiceUnavailable},
		{"ErrProducerClosed", ErrProducerClosed},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if !errors.Is(tc.target, tc.target) {
				t.Errorf("errors.Is(%s, %s) = false, want true", tc.name, tc.name)
			}
		})
	}
}

func TestSentinelErrors_NotEqual(t *testing.T) {
	t.Parallel()
	// Verify that different sentinel errors are not equal
	allErrors := []error{
		ErrInvalidChannel,
		ErrTopicNotProvisioned,
		ErrServiceUnavailable,
		ErrProducerClosed,
	}

	for i, err1 := range allErrors {
		for j, err2 := range allErrors {
			if i != j && errors.Is(err1, err2) {
				t.Errorf("errors.Is returned true for different errors: %v and %v", err1, err2)
			}
		}
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkPublishErrorMessages_Lookup(b *testing.B) {
	for b.Loop() {
		_ = PublishErrorMessages[ErrCodeRateLimited]
	}
}

func BenchmarkErrorCode_StringConversion(b *testing.B) {
	code := ErrCodeTopicNotProvisioned
	for b.Loop() {
		_ = string(code)
	}
}
