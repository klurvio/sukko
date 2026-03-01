package configstore

import (
	"fmt"
	"sync/atomic"

	"github.com/rs/zerolog"
)

// Loader manages loading and hot-reloading a YAML config file into in-memory stores.
type Loader struct {
	path   string
	stores atomic.Pointer[ConfigStores]
	logger zerolog.Logger
}

// NewLoader creates a new config file loader for the given path.
func NewLoader(path string, logger zerolog.Logger) *Loader {
	return &Loader{
		path:   path,
		logger: logger.With().Str("component", "config_loader").Logger(),
	}
}

// Load performs the initial load of the config file. Must be called before Stores().
func (l *Loader) Load() error {
	cfg, err := ParseFile(l.path)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := Validate(cfg); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	stores := BuildStores(cfg, l.logger)
	l.stores.Store(stores)

	l.logger.Info().
		Str("path", l.path).
		Int("tenants", len(cfg.Tenants)).
		Msg("config file loaded")

	return nil
}

// Reload re-reads the config file and atomically swaps the stores.
// On failure, the previous stores remain active.
func (l *Loader) Reload() error {
	cfg, err := ParseFile(l.path)
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}

	if err := Validate(cfg); err != nil {
		return fmt.Errorf("validate config on reload: %w", err)
	}

	stores := BuildStores(cfg, l.logger)
	l.stores.Store(stores)

	l.logger.Info().
		Str("path", l.path).
		Int("tenants", len(cfg.Tenants)).
		Msg("config file reloaded")

	return nil
}

// Stores returns the current in-memory stores. Returns nil if Load() has not been called.
func (l *Loader) Stores() *ConfigStores {
	return l.stores.Load()
}
