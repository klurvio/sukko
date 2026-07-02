package runner

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	kafkashared "github.com/klurvio/sukko/internal/shared/kafka"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/rs/zerolog"
)

// Sentinel errors for expected conditions.
var (
	ErrTestNotFound      = errors.New("test not found")
	ErrTestAlreadyExists = errors.New("test already exists")
	ErrInvalidConfig     = errors.New("invalid config")
)

// TestType identifies the kind of test to run.
type TestType string

// TestSmoke and related constants enumerate the supported test types.
const (
	TestSmoke    TestType = "smoke"
	TestLoad     TestType = "load"
	TestStress   TestType = "stress"
	TestSoak     TestType = "soak"
	TestValidate TestType = "validate"
)

// Test channel names for each test type.
const (
	smokeTestChannel  = "sukko.smoke.test"
	loadTestChannel   = "sukko.load.test"
	stressTestChannel = "sukko.stress.test"
	soakTestChannel   = "sukko.soak.test"
)

// defaultRampRate is the fallback connections-per-second rate when not configured.
const defaultRampRate = 50

// TestContext holds deployment context passed from the CLI.
// When present, all core fields are required (all-or-nothing).
type TestContext struct {
	GatewayURL         string `json:"gateway_url"`
	ProvisioningURL    string `json:"provisioning_url"`
	Environment        string `json:"environment"`
	MessageBackendURLs string `json:"message_backend_urls,omitempty"`
}

// TestConfig holds the parameters for a test run.
type TestConfig struct {
	Type                   TestType      `json:"type"`
	GatewayURL             string        `json:"gateway_url"`
	ProvisioningURL        string        `json:"provisioning_url,omitempty"`
	APIKey                 string        `json:"-"` // per-request API key; tagged json:"-" — credentials must never appear in API responses (§IX)
	AuthMode               AuthMode      `json:"auth_mode,omitempty"`
	AuthMixRatio           float64       `json:"auth_mix_ratio,omitzero"`
	MessageBackend         string        `json:"message_backend,omitempty"`
	KafkaBrokers           string        `json:"kafka_brokers,omitempty"`
	Connections            int           `json:"connections,omitzero"`
	Duration               string        `json:"duration,omitempty"`
	PublishRate            int           `json:"publish_rate,omitzero"`
	RampRate               int           `json:"ramp_rate,omitzero"`
	Suite                  string        `json:"suite,omitempty"`       // for validate type
	ChannelMode            bool          `json:"channel_mode,omitzero"` // for load: distribute across public/user/group channels
	TenantID               string        `json:"tenant_id,omitempty"`
	GatewayMetricsURL      string        `json:"gateway_metrics_url,omitempty"`
	GatewayMetricsInterval time.Duration `json:"-"` // never serialized — config-level only
	RevocationsPerCycle    int           `json:"revocations_per_cycle,omitzero"`
	SigningKeyFile         string        `json:"signing_key_file,omitempty"` // Ed25519 private key path (env var fallback)
	SigningKeyBytes        []byte        `json:"-"`                          // Ed25519 private key bytes (API passthrough, never serialized)
	AdminKeyBytes          []byte        `json:"-"`                          // Ed25519 private key bytes from per-request admin_key (never serialized)
	AdminKeyID             string        `json:"-"`                          // effective kid for per-request key (overridden by admin_key_id body field)
	Context                *TestContext  `json:"context,omitzero"`
}

// TestStatus represents the current state of a test run.
type TestStatus string

// StatusPending and related constants enumerate test run states.
const (
	StatusPending  TestStatus = "pending"
	StatusRunning  TestStatus = "running"
	StatusComplete TestStatus = "complete"
	StatusFailed   TestStatus = "failed"
	StatusStopped  TestStatus = "stopped"
)

// TestRun tracks the state and results of an individual test execution.
type TestRun struct {
	ID               string             `json:"id"`
	Config           TestConfig         `json:"config"`
	Status           TestStatus         `json:"status"`
	Collector        *metrics.Collector `json:"-"`
	Report           *metrics.Report    `json:"report,omitempty"`
	mu               sync.RWMutex       `json:"-"`
	cancel           context.CancelFunc
	authResult       *auth.SetupResult `json:"-"`
	jwtLifetime      time.Duration     `json:"-"`
	jwtRefreshBefore time.Duration     `json:"-"`
	// Auth mode fields set by execute() before dispatching to test functions.
	apiKey             string        `json:"-"` // effective API key for this run
	authUpgradeTimeout time.Duration `json:"-"` // timeout for auth_ack in upgrade flow
	// Webhook suite fields set by execute().
	webhookBaseURL         string        `json:"-"`
	webhookDeliveryTimeout time.Duration `json:"-"`
	webhookRetryTimeout    time.Duration `json:"-"`
	webhookStore           *WebhookStore `json:"-"`
}

// StatusSnapshot returns the current Status and Report under a read lock.
func (t *TestRun) StatusSnapshot() (TestStatus, *metrics.Report) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status, t.Report
}

// ConfigSnapshot returns a copy of Config under a read lock, safe for concurrent access.
func (t *TestRun) ConfigSnapshot() TestConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Config
}

// Runner manages concurrent test executions.
type Runner struct {
	mu           sync.RWMutex
	tests        map[string]*TestRun
	wg           sync.WaitGroup
	logger       zerolog.Logger
	cfg          Config
	webhookStore *WebhookStore
}

// Config holds default settings applied to all test runs.
type Config struct {
	GatewayURL       string
	ProvisioningURL  string
	MessageBackend   string
	KafkaBrokers     string
	KafkaSASL        *kafkashared.SASLConfig // nil = no SASL auth; carrier field for future Kafka publisher wiring
	KafkaTLS         *kafkashared.TLSConfig  // nil = plaintext; carrier field for future Kafka publisher wiring
	JWTLifetime      time.Duration
	JWTRefreshBefore time.Duration
	KeyExpiry        time.Duration
	SigningKeyFile   string // Ed25519 private key file for license-reload suite signing
	AdminKeyFile     string // path to raw Ed25519 private key file (remote mode; empty = local dev mode)
	AdminKeyID       string // kid embedded in admin JWTs; defaults to BootstrapAdminKeyID
	// Auth mode fields (TESTER_AUTH_MODE and related)
	AuthMode           AuthMode
	APIKey             string // TESTER_API_KEY; stored here only, never in TestConfig (§IX)
	AuthMixRatio       float64
	AuthUpgradeTimeout time.Duration
	// Gateway metrics scraping for revocation load suites.
	GatewayMetricsURL      string
	GatewayMetricsInterval time.Duration
	// Webhook suite configuration.
	WebhookBaseURL         string
	WebhookDeliveryTimeout time.Duration
	WebhookRetryTimeout    time.Duration
}

// New creates a Runner with the given configuration and logger.
func New(cfg Config, logger zerolog.Logger) *Runner {
	return &Runner{
		tests:        make(map[string]*TestRun),
		logger:       logger,
		cfg:          cfg,
		webhookStore: newWebhookStore(),
	}
}

// Start launches a new test run with the given ID and configuration.
func (r *Runner) Start(id string, cfg TestConfig) (*TestRun, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.tests[id]; exists {
		return nil, fmt.Errorf("test %s: %w", id, ErrTestAlreadyExists)
	}

	// Fill in defaults from runner config
	if cfg.GatewayURL == "" {
		cfg.GatewayURL = r.cfg.GatewayURL
	}
	if cfg.ProvisioningURL == "" {
		cfg.ProvisioningURL = r.cfg.ProvisioningURL
	}
	if cfg.MessageBackend == "" {
		cfg.MessageBackend = r.cfg.MessageBackend
	}
	if cfg.KafkaBrokers == "" {
		cfg.KafkaBrokers = r.cfg.KafkaBrokers
	}
	if cfg.SigningKeyFile == "" {
		cfg.SigningKeyFile = r.cfg.SigningKeyFile
	}
	if cfg.AuthMixRatio == 0 && r.cfg.AuthMixRatio != 0 {
		cfg.AuthMixRatio = r.cfg.AuthMixRatio
	}
	// GatewayMetricsURL: per-run override takes precedence; fall back to service-level.
	if cfg.GatewayMetricsURL == "" {
		cfg.GatewayMetricsURL = r.cfg.GatewayMetricsURL
	}
	// GatewayMetricsInterval: always use service-level value — no per-run override.
	cfg.GatewayMetricsInterval = r.cfg.GatewayMetricsInterval

	// §II defense-in-depth: validate auth mode and mode/type combinations.
	// The handler also validates, but the runner validates again for programmatic callers.
	if cfg.AuthMode == "" {
		cfg.AuthMode = AuthModeJWT // backward compat: missing auth_mode defaults to jwt
	}
	switch cfg.AuthMode {
	case AuthModeJWT, AuthModeAPIKey, AuthModeUpgrade, AuthModeMixed:
		// valid
	default:
		return nil, fmt.Errorf("unknown auth_mode %q: %w", cfg.AuthMode, ErrInvalidConfig)
	}
	if cfg.AuthMixRatio < AuthMixRatioMin || cfg.AuthMixRatio > AuthMixRatioMax {
		return nil, fmt.Errorf("auth_mix_ratio %.2f out of range [%.1f, %.1f]: %w", cfg.AuthMixRatio, AuthMixRatioMin, AuthMixRatioMax, ErrInvalidConfig)
	}
	switch cfg.AuthMode {
	case AuthModeJWT:
		// no restrictions
	case AuthModeMixed:
		switch cfg.Type {
		case TestLoad, TestSoak:
			// valid for mixed mode
		case TestValidate, TestSmoke, TestStress:
			return nil, fmt.Errorf("auth_mode=mixed is not valid for type=%s: %w", cfg.Type, ErrInvalidConfig)
		}
	case AuthModeAPIKey:
		if cfg.Type != TestValidate {
			return nil, fmt.Errorf("auth_mode=api-key is only valid for type=validate, got type=%s: %w", cfg.Type, ErrInvalidConfig)
		}
		if cfg.Suite != "" && cfg.Suite != SuiteAPIKey && cfg.Suite != SuiteRestPublish {
			return nil, fmt.Errorf("auth_mode=api-key only supports suite=%s or suite=%s, got suite=%s: %w", SuiteAPIKey, SuiteRestPublish, cfg.Suite, ErrInvalidConfig)
		}
		if cfg.TenantID == "" {
			return nil, fmt.Errorf("auth_mode=api-key requires tenant_id: %w", ErrInvalidConfig)
		}
	case AuthModeUpgrade:
		if cfg.Type != TestValidate {
			return nil, fmt.Errorf("auth_mode=upgrade is only valid for type=validate, got type=%s: %w", cfg.Type, ErrInvalidConfig)
		}
		if cfg.Suite != "" && cfg.Suite != SuiteUpgrade {
			return nil, fmt.Errorf("auth_mode=upgrade only supports suite=%s, got suite=%s: %w", SuiteUpgrade, cfg.Suite, ErrInvalidConfig)
		}
		if cfg.TenantID == "" {
			return nil, fmt.Errorf("auth_mode=upgrade requires tenant_id: %w", ErrInvalidConfig)
		}
	}

	// suite=revocation is only valid for stress and soak test types.
	if cfg.Suite == SuiteRevocation && cfg.Type != TestStress && cfg.Type != TestSoak {
		return nil, fmt.Errorf("suite=%s is only valid for type=stress or type=soak, got type=%s: %w", SuiteRevocation, cfg.Type, ErrInvalidConfig)
	}

	ctx, cancel := context.WithCancel(context.Background())

	run := &TestRun{
		ID:        id,
		Config:    cfg,
		Status:    StatusRunning,
		Collector: metrics.NewCollector(),
		cancel:    cancel,
	}

	r.tests[id] = run

	r.wg.Go(func() {
		defer logging.RecoverPanic(r.logger, "test-runner-dispatch", map[string]any{"test_id": id})
		r.execute(ctx, run)
	})

	return run, nil
}

// Wait blocks until all test execution goroutines have completed.
func (r *Runner) Wait() {
	r.wg.Wait()
}

// Stop cancels a running test by ID.
func (r *Runner) Stop(id string) error {
	r.mu.RLock()
	run, ok := r.tests[id]
	r.mu.RUnlock()

	if !ok {
		return fmt.Errorf("test %s: %w", id, ErrTestNotFound)
	}

	run.cancel()
	return nil
}

// StopAll cancels all running tests. Used during graceful shutdown.
func (r *Runner) StopAll() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, run := range r.tests {
		run.cancel()
	}
}

// Get retrieves a test run by ID.
func (r *Runner) Get(id string) (*TestRun, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	run, ok := r.tests[id]
	if !ok {
		return nil, fmt.Errorf("test %s: %w", id, ErrTestNotFound)
	}
	return run, nil
}

// resolveAdminProvider returns the admin auth provider for a test run using a
// priority chain: (1) per-request bytes, (2) config file, (3) nil (ephemeral).
// Returning nil signals auth.Setup to generate an ephemeral keypair (local dev mode).
func resolveAdminProvider(cfg Config, testCfg TestConfig) (auth.Provider, error) {
	if len(testCfg.AdminKeyBytes) > 0 {
		key := ed25519.PrivateKey(testCfg.AdminKeyBytes)
		if err := auth.ValidateEd25519Key(key); err != nil {
			return nil, fmt.Errorf("resolveAdminProvider: per-request admin key: %w", err)
		}
		keyID := testCfg.AdminKeyID
		if keyID == "" {
			keyID = cfg.AdminKeyID
		}
		return auth.NewKeypairAuthProvider(key, keyID, auth.AdminKeyName), nil
	}
	if cfg.AdminKeyFile != "" {
		key, err := auth.LoadEd25519PrivateKey(cfg.AdminKeyFile)
		if err != nil {
			return nil, fmt.Errorf("resolveAdminProvider: %w", err)
		}
		return auth.NewKeypairAuthProvider(key, cfg.AdminKeyID, auth.AdminKeyName), nil
	}
	return nil, nil // nil → auth.Setup generates ephemeral (local dev mode)
}

func (r *Runner) execute(ctx context.Context, run *TestRun) {
	logger := r.logger.With().Str("test_id", run.ID).Str("type", string(run.Config.Type)).Logger()

	defer logging.RecoverPanic(logger, "test-execution", map[string]any{"test_id": run.ID})

	logger.Info().Str("auth_mode", string(run.Config.AuthMode)).Msg("starting test")

	adminProvider, providerErr := resolveAdminProvider(r.cfg, run.Config)
	if providerErr != nil {
		r.failRun(run, fmt.Sprintf("resolve admin provider: %v", providerErr))
		logger.Error().Err(providerErr).Msg("admin provider resolution failed")
		return
	}

	// Auth setup: branch on auth mode.
	// api-key mode: skip JWT keypair registration; Minter and TokenFunc will be nil.
	// All other modes: full JWT setup (register keypair, create minter).
	var authResult *auth.SetupResult
	var authErr error

	if run.Config.AuthMode == AuthModeAPIKey {
		authResult, authErr = auth.SetupAPIKeyOnly(ctx, auth.SetupConfig{
			TestID:               run.ID,
			TenantID:             run.Config.TenantID,
			ProvisioningURL:      run.Config.ProvisioningURL,
			Logger:               logger,
			AdminProvider:        adminProvider,
			RequireAdminProvider: r.cfg.AdminKeyFile != "",
		})
	} else {
		authResult, authErr = auth.Setup(ctx, auth.SetupConfig{
			TestID:               run.ID,
			TenantID:             run.Config.TenantID,
			ProvisioningURL:      run.Config.ProvisioningURL,
			JWTLifetime:          r.cfg.JWTLifetime,
			KeyExpiry:            r.cfg.KeyExpiry,
			Logger:               logger,
			AdminProvider:        adminProvider,
			RequireAdminProvider: r.cfg.AdminKeyFile != "",
		})
	}
	if authErr != nil {
		r.failRun(run, fmt.Sprintf("auth setup: %v", authErr))
		logger.Error().Err(authErr).Msg("auth setup failed")
		return
	}
	defer authResult.Cleanup(context.Background()) //nolint:contextcheck // NFR-002: cleanup must survive parent cancellation

	// Populate auth state on run for test functions.
	// Config.TenantID is written under mu; readers use ConfigSnapshot() which also acquires mu.
	// The unexported fields are only accessed from this goroutine chain.
	run.mu.Lock()
	run.Config.TenantID = authResult.TenantID
	run.mu.Unlock()
	run.authResult = authResult
	run.jwtLifetime = r.cfg.JWTLifetime
	run.jwtRefreshBefore = r.cfg.JWTRefreshBefore
	run.authUpgradeTimeout = r.cfg.AuthUpgradeTimeout
	run.webhookBaseURL = r.cfg.WebhookBaseURL
	run.webhookDeliveryTimeout = r.cfg.WebhookDeliveryTimeout
	run.webhookRetryTimeout = r.cfg.WebhookRetryTimeout
	run.webhookStore = r.webhookStore

	// Resolve effective API key: per-request key overrides runner-level config key.
	effectiveAPIKey := r.cfg.APIKey
	if run.Config.APIKey != "" {
		effectiveAPIKey = run.Config.APIKey
	}
	run.apiKey = effectiveAPIKey

	// Mixed mode with no static API key: create one dynamically at test start.
	// The key is revoked at teardown (best-effort) using a fresh context (§III, §IV).
	if run.Config.AuthMode == AuthModeMixed && run.apiKey == "" {
		keyBody, keyErr := authResult.ProvClient.CreateAPIKey(ctx, authResult.TenantID, "tester-mixed-"+run.ID)
		if keyErr != nil {
			r.failRun(run, fmt.Sprintf("create mixed-mode api key: %v", keyErr))
			logger.Error().Err(keyErr).Msg("failed to create mixed-mode api key")
			return
		}
		// Register revoke defer immediately after CreateAPIKey succeeds — before any parse
		// check — so teardown always runs. Guard on run.apiKey: if parse fails the key_id is
		// unknown and revoke is impossible; log a prominent warning for manual cleanup.
		defer func() { //nolint:contextcheck // teardown: test ctx is canceled at this point; fresh context required
			if run.apiKey == "" {
				logger.Error().Msg("teardown: mixed-mode api key created but key_id unknown — cannot revoke; manual cleanup required")
				return
			}
			revokeCtx, revokeCancel := context.WithTimeout(context.Background(), editionHTTPTimeout)
			defer revokeCancel()
			if err := authResult.ProvClient.RevokeAPIKey(revokeCtx, authResult.TenantID, run.apiKey); err != nil {
				logger.Error().Err(err).Str("key_id", run.apiKey).Msg("teardown: failed to revoke mixed-mode api key")
			}
		}()
		var keyResp struct {
			KeyID string `json:"key_id"`
		}
		if err := json.Unmarshal(keyBody, &keyResp); err != nil || keyResp.KeyID == "" {
			r.failRun(run, "create mixed-mode api key: failed to parse key_id from response")
			return
		}
		run.apiKey = keyResp.KeyID
	}

	var report *metrics.Report
	var err error

	// Mixed mode dispatch: handled here (in execute, a *Runner method) because runLoad/runSoak
	// are package-level functions without access to r.cfg.
	if run.Config.AuthMode == AuthModeMixed {
		switch run.Config.Type {
		case TestLoad:
			report, err = runLoadMixed(ctx, run, logger)
		case TestSoak:
			report, err = runSoakMixed(ctx, run, logger)
		default:
			err = fmt.Errorf("mixed mode not valid for type %s", run.Config.Type)
		}
	} else {
		switch run.Config.Type {
		case TestSmoke:
			report, err = runSmoke(ctx, run, logger)
		case TestLoad:
			report, err = runLoad(ctx, run, logger)
		case TestStress:
			if run.Config.Suite == SuiteRevocation {
				report, err = runStressRevocation(ctx, run, logger)
			} else {
				report, err = runStress(ctx, run, logger)
			}
		case TestSoak:
			if run.Config.Suite == SuiteRevocation {
				report, err = runSoakRevocation(ctx, run, logger)
			} else {
				report, err = runSoak(ctx, run, logger)
			}
		case TestValidate:
			report, err = runValidate(ctx, run, logger)
		default:
			err = fmt.Errorf("unknown test type: %s", run.Config.Type)
		}
	}

	run.mu.Lock()
	switch {
	case err != nil:
		run.Status = StatusFailed
		if report == nil {
			report = &metrics.Report{
				TestType: string(run.Config.Type),
				Status:   metrics.ReportStatusError,
				Metrics:  run.Collector.Snapshot(),
				Errors:   []string{err.Error()},
			}
		}
	case ctx.Err() != nil:
		run.Status = StatusStopped
	default:
		run.Status = StatusComplete
	}
	run.Report = report
	run.mu.Unlock()

	logger.Info().Str("status", string(run.Status)).Msg("test completed")
}

// WebhookReceiveHandler returns the http.HandlerFunc that records incoming webhook
// deliveries. Register it at POST /webhook-receive/{runID} in the API router.
func (r *Runner) WebhookReceiveHandler() http.HandlerFunc {
	return webhookReceiveHandler(r.webhookStore)
}

func (r *Runner) failRun(run *TestRun, errMsg string) {
	run.mu.Lock()
	defer run.mu.Unlock()
	run.Status = StatusFailed
	run.Report = &metrics.Report{
		TestType: string(run.Config.Type),
		Status:   metrics.ReportStatusError,
		Metrics:  run.Collector.Snapshot(),
		Errors:   []string{errMsg},
	}
}
