package configstore

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ParseFile reads and parses a YAML config file from the given path.
func ParseFile(path string) (*ConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file %s: %w", path, err)
	}
	return ParseBytes(data)
}

// ParseBytes parses YAML bytes into a ConfigFile.
func ParseBytes(data []byte) (*ConfigFile, error) {
	var cfg ConfigFile
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	return &cfg, nil
}
