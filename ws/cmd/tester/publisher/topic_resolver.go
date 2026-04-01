package publisher

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/klurvio/sukko/internal/shared/kafka"
)

// RoutingRule maps a channel pattern to a Kafka topic suffix.
// Pattern supports `*` wildcard matching.
type RoutingRule struct {
	Pattern     string
	TopicSuffix string
}

// TopicResolver resolves channel names to fully qualified Kafka topic names
// using tenant routing rules and the platform topic naming convention.
// Cached per test run — routing rules are fetched once at setup.
type TopicResolver struct {
	namespace string
	tenantID  string
	rules     []RoutingRule
}

// NewTopicResolver creates a resolver with the given namespace, tenant, and rules.
func NewTopicResolver(namespace, tenantID string, rules []RoutingRule) *TopicResolver {
	return &TopicResolver{
		namespace: namespace,
		tenantID:  tenantID,
		rules:     rules,
	}
}

// Resolve maps a channel name to a fully qualified Kafka topic name.
// Evaluates routing rules in order (first match wins) and builds the topic
// using kafka.BuildTopicName(namespace, tenantID, topicSuffix).
func (r *TopicResolver) Resolve(channel string) (string, error) {
	if len(r.rules) == 0 {
		return "", errors.New("topic resolver: no routing rules configured")
	}

	for _, rule := range r.rules {
		if matchWildcard(rule.Pattern, channel) {
			return kafka.BuildTopicName(r.namespace, r.tenantID, rule.TopicSuffix), nil
		}
	}

	return "", fmt.Errorf("topic resolver: no routing rule matches channel %q", channel)
}

// matchWildcard checks if a channel matches a pattern with `*` wildcards.
// `*` matches any sequence of characters (including dots).
// Examples: "*.trade" matches "BTC.trade", "crypto.BTC.trade"
//
//	"*.*" matches any channel with at least one dot
func matchWildcard(pattern, channel string) bool {
	// Simple recursive matching — patterns are short, no performance concern for a test tool.
	return matchWildcardRecursive(pattern, channel)
}

func matchWildcardRecursive(pattern, str string) bool {
	for pattern != "" {
		if pattern[0] == '*' {
			// Skip consecutive wildcards
			for pattern != "" && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if pattern == "" {
				return true // trailing * matches everything
			}
			// Try matching remainder at every position
			for i := range len(str) + 1 {
				if matchWildcardRecursive(pattern, str[i:]) {
					return true
				}
			}
			return false
		}

		if str == "" || pattern[0] != str[0] {
			return false
		}
		pattern = pattern[1:]
		str = str[1:]
	}
	return str == ""
}

// ParseRoutingRules converts the provisioning API routing rules response
// into RoutingRule structs. Expects the wrapper format: {"rules": [...]}.
func ParseRoutingRules(rulesJSON []byte) ([]RoutingRule, error) {
	type ruleItem struct {
		Pattern     string `json:"pattern"`
		TopicSuffix string `json:"topic_suffix"`
	}
	type wrapper struct {
		Rules []ruleItem `json:"rules"`
	}

	var w wrapper
	if err := json.Unmarshal(rulesJSON, &w); err != nil {
		return nil, fmt.Errorf("parse routing rules: %w", err)
	}

	rules := make([]RoutingRule, len(w.Rules))
	for i, r := range w.Rules {
		rules[i] = RoutingRule(r)
	}
	return rules, nil
}
