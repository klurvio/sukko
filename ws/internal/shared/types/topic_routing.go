package types

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// TopicRoutingRule maps a channel suffix pattern to a Kafka topic suffix.
// Rules are evaluated in order; first match wins.
type TopicRoutingRule struct {
	// Pattern is a channel suffix pattern (e.g., "crypto.*.trade").
	// Supports * wildcards but NOT {placeholder} tokens.
	// Wildcard (*) matches any sequence of characters (including dots),
	// so "crypto.*.trade" matches both "crypto.BTC.trade" and "crypto.nft.BTC.trade".
	Pattern string `json:"pattern"`

	// TopicSuffix is the literal Kafka topic suffix (e.g., "crypto.trade").
	// No wildcards allowed.
	TopicSuffix string `json:"topic_suffix"`
}

const (
	// MaxRoutingPatternLength is the maximum length of a routing rule pattern.
	MaxRoutingPatternLength = 256

	// MaxTopicSuffixLength is the maximum length of a Kafka topic suffix.
	MaxTopicSuffixLength = 128
)

// routingPatternRegex validates routing rule patterns.
// Separate from channelPatternRegex — routing patterns MUST NOT contain {} placeholders
// since MatchWildcard doesn't resolve them.
var routingPatternRegex = regexp.MustCompile(fmt.Sprintf(`^[a-zA-Z0-9.*_-]{1,%d}$`, MaxRoutingPatternLength))

// topicSuffixRegex validates topic suffixes. No wildcards — suffixes are literal.
var topicSuffixRegex = regexp.MustCompile(fmt.Sprintf(`^[a-zA-Z0-9._-]{1,%d}$`, MaxTopicSuffixLength))

// Sentinel errors for topic routing.
var (
	// ErrNoRoutingRules indicates no routing rules are configured for the tenant.
	ErrNoRoutingRules = errors.New("no routing rules configured")

	// ErrNoMatchingRoute indicates the channel suffix matched no routing rule.
	ErrNoMatchingRoute = errors.New("no matching routing rule")

	// ErrRoutingRulesNotFound indicates routing rules could not be found in the registry.
	ErrRoutingRulesNotFound = errors.New("routing rules not found")

	// Validation sentinel errors for ValidateRoutingRules.
	ErrEmptyRoutingRules        = errors.New("routing rules cannot be empty")
	ErrEmptyRoutingPattern      = errors.New("pattern cannot be empty")
	ErrEmptyTopicSuffix         = errors.New("topic_suffix cannot be empty")
	ErrDuplicateRoutingPattern  = errors.New("duplicate pattern")
	ErrRoutingPlaceholderForbid = errors.New("pattern must not contain placeholders")
	ErrInvalidRoutingPattern    = errors.New("invalid pattern syntax")
	ErrInvalidTopicSuffix       = errors.New("invalid topic suffix syntax")
	ErrTooManyRoutingRules      = errors.New("too many routing rules")
)

// ValidateRoutingRules validates a slice of topic routing rules.
// Checks: non-empty patterns/suffixes, valid pattern syntax, no wildcards in suffix,
// and no placeholder tokens ({...}) in patterns.
// Count limits are enforced at the service layer via configurable MaxRoutingRules.
func ValidateRoutingRules(rules []TopicRoutingRule) error {
	if len(rules) == 0 {
		return ErrEmptyRoutingRules
	}

	seen := make(map[string]struct{}, len(rules))

	for i, rule := range rules {
		if rule.Pattern == "" {
			return fmt.Errorf("routing rule %d: %w", i, ErrEmptyRoutingPattern)
		}
		if rule.TopicSuffix == "" {
			return fmt.Errorf("routing rule %d: %w", i, ErrEmptyTopicSuffix)
		}

		// Reject duplicate patterns — second match is unreachable (first-match-wins).
		if _, ok := seen[rule.Pattern]; ok {
			return fmt.Errorf("routing rule %d: %w: %s", i, ErrDuplicateRoutingPattern, rule.Pattern)
		}
		seen[rule.Pattern] = struct{}{}

		// Reject patterns containing placeholder tokens — MatchWildcard doesn't resolve them.
		if strings.ContainsAny(rule.Pattern, "{}") {
			return fmt.Errorf("routing rule %d: %w: %s", i, ErrRoutingPlaceholderForbid, rule.Pattern)
		}

		if !routingPatternRegex.MatchString(rule.Pattern) {
			return fmt.Errorf("routing rule %d: %w: %s", i, ErrInvalidRoutingPattern, rule.Pattern)
		}

		if !topicSuffixRegex.MatchString(rule.TopicSuffix) {
			return fmt.Errorf("routing rule %d: %w: %s", i, ErrInvalidTopicSuffix, rule.TopicSuffix)
		}
	}

	return nil
}

// UniqueTopicSuffixes extracts deduplicated topic suffixes from routing rules.
// Used to derive the set of Kafka topics that need to exist for a tenant.
func UniqueTopicSuffixes(rules []TopicRoutingRule) []string {
	seen := make(map[string]struct{}, len(rules))
	suffixes := make([]string, 0, len(rules))
	for _, rule := range rules {
		if _, ok := seen[rule.TopicSuffix]; !ok {
			seen[rule.TopicSuffix] = struct{}{}
			suffixes = append(suffixes, rule.TopicSuffix)
		}
	}
	return suffixes
}

// ErrEmptyChannelSuffix indicates the channel suffix is empty.
var ErrEmptyChannelSuffix = errors.New("channel suffix cannot be empty")

// ErrNilMatchFunc indicates the match function is nil.
var ErrNilMatchFunc = errors.New("match function cannot be nil")

// ResolveTopicSuffix evaluates routing rules in order and returns the topic suffix
// for the first matching rule. The matchFn parameter avoids importing auth package
// (types is a leaf package); callers pass auth.MatchWildcard.
//
// Returns ErrNoRoutingRules if rules is empty, ErrNoMatchingRoute if no rule matches.
func ResolveTopicSuffix(rules []TopicRoutingRule, channelSuffix string, matchFn func(pattern, value string) bool) (string, error) {
	if len(rules) == 0 {
		return "", ErrNoRoutingRules
	}
	if channelSuffix == "" {
		return "", ErrEmptyChannelSuffix
	}
	if matchFn == nil {
		return "", ErrNilMatchFunc
	}

	for _, rule := range rules {
		if matchFn(rule.Pattern, channelSuffix) {
			return rule.TopicSuffix, nil
		}
	}

	return "", ErrNoMatchingRoute
}
