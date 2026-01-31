package platform

import "fmt"

// ValidLogLevels are the supported log levels.
var ValidLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// ValidLogFormats are the supported log formats.
var ValidLogFormats = map[string]bool{
	"json":   true,
	"text":   true,
	"pretty": true,
}

// ValidKafkaSASLMechanisms are the supported SASL mechanisms.
var ValidKafkaSASLMechanisms = map[string]bool{
	"scram-sha-256": true,
	"scram-sha-512": true,
}

// ValidateLogLevel checks if the log level is valid.
func ValidateLogLevel(level string) error {
	if !ValidLogLevels[level] {
		return fmt.Errorf("LOG_LEVEL must be one of: debug, info, warn, error (got: %s)", level)
	}
	return nil
}

// ValidateLogFormat checks if the log format is valid.
func ValidateLogFormat(format string) error {
	if !ValidLogFormats[format] {
		return fmt.Errorf("LOG_FORMAT must be one of: json, text, pretty (got: %s)", format)
	}
	return nil
}

// ValidateKafkaSASLMechanism checks if the SASL mechanism is valid.
func ValidateKafkaSASLMechanism(mechanism string) error {
	if !ValidKafkaSASLMechanisms[mechanism] {
		return fmt.Errorf("KAFKA_SASL_MECHANISM must be 'scram-sha-256' or 'scram-sha-512', got: %s", mechanism)
	}
	return nil
}
