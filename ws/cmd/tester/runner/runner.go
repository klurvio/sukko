package runner

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/klurvio/sukko/cmd/tester/auth"
	"github.com/klurvio/sukko/cmd/tester/metrics"
	"github.com/klurvio/sukko/internal/shared/logging"
	"github.com/rs/zerolog"
)

// Sentinel errors for expected conditions.
var (
	ErrTestNotFound      = errors.New("test not found")
	ErrTestAlreadyExists = errors.New("test already exists")
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
	AdminToken         string `json:"-"` // never serialize in API responses
	Environment        string `json:"environment"`
	MessageBackendURLs string `json:"message_backend_urls,omitempty"`
}

// TestConfig holds the parameters for a test run.
type TestConfig struct {
	Type              TestType     `json:"type"`
	GatewayURL        string       `json:"gateway_url"`
	ProvisioningURL   string       `json:"provisioning_url,omitempty"`
	Token             string       `json:"-"` // never serialize auth tokens in API responses
	APIKey            string       `json:"api_key,omitempty"`
	MessageBackend    string       `json:"message_backend,omitempty"`
	KafkaBrokers      string       `json:"kafka_brokers,omitempty"`
	NATSJetStreamURLs string       `json:"nats_jetstream_urls,omitempty"`
	Connections       int          `json:"connections,omitzero"`
	Duration          string       `json:"duration,omitempty"`
	PublishRate       int          `json:"publish_rate,omitzero"`
	RampRate          int          `json:"ramp_rate,omitzero"`
	Suite             string       `json:"suite,omitempty"`       // for validate type
	ChannelMode       bool         `json:"channel_mode,omitzero"` // for load: distribute across public/user/group channels
	TenantID          string       `json:"tenant_id,omitempty"`
	Context           *TestContext `json:"context,omitzero"`
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
}

// StatusSnapshot returns the current Status and Report under a read lock.
func (t *TestRun) StatusSnapshot() (TestStatus, *metrics.Report) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.Status, t.Report
}

// Runner manages concurrent test executions.
type Runner struct {
	mu     sync.RWMutex
	tests  map[string]*TestRun
	wg     sync.WaitGroup
	logger zerolog.Logger
	cfg    Config
}

// Config holds default settings applied to all test runs.
type Config struct {
	GatewayURL        string
	ProvisioningURL   string
	Token             string
	MessageBackend    string
	KafkaBrokers      string
	NATSJetStreamURLs string
	JWTLifetime       time.Duration
	JWTRefreshBefore  time.Duration
	KeyExpiry         time.Duration
}

// New creates a Runner with the given configuration and logger.
func New(cfg Config, logger zerolog.Logger) *Runner {
	return &Runner{
		tests:  make(map[string]*TestRun),
		logger: logger,
		cfg:    cfg,
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
	if cfg.Token == "" {
		cfg.Token = r.cfg.Token
	}
	if cfg.MessageBackend == "" {
		cfg.MessageBackend = r.cfg.MessageBackend
	}
	if cfg.KafkaBrokers == "" {
		cfg.KafkaBrokers = r.cfg.KafkaBrokers
	}

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118: cancel stored in TestRun.cancel and called by Stop()/StopAll()

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

func (r *Runner) execute(ctx context.Context, run *TestRun) {
	logger := r.logger.With().Str("test_id", run.ID).Str("type", string(run.Config.Type)).Logger()

	defer logging.RecoverPanic(logger, "test-execution", map[string]any{"test_id": run.ID})

	logger.Info().Msg("starting test")

	// Auth setup: register key + create minter for JWT-authenticated connections
	authResult, authErr := auth.Setup(ctx, auth.SetupConfig{
		TestID:          run.ID,
		TenantID:        run.Config.TenantID,
		ProvisioningURL: run.Config.ProvisioningURL,
		AdminToken:      run.Config.Token,
		JWTLifetime:     r.cfg.JWTLifetime,
		KeyExpiry:       r.cfg.KeyExpiry,
		Logger:          logger,
	})
	if authErr != nil {
		run.mu.Lock()
		run.Status = StatusFailed
		run.Report = &metrics.Report{
			TestType: string(run.Config.Type),
			Status:   "error",
			Metrics:  run.Collector.Snapshot(),
			Errors:   []string{fmt.Sprintf("auth setup: %v", authErr)},
		}
		run.mu.Unlock()
		logger.Error().Err(authErr).Msg("auth setup failed")
		return
	}
	defer authResult.Cleanup(context.Background()) //nolint:contextcheck // NFR-002: cleanup must survive parent cancellation

	// Populate auth state on run for test functions.
	// Config.TenantID needs mu because getTest reads Config without lock.
	// The unexported fields are only accessed from this goroutine chain.
	run.mu.Lock()
	run.Config.TenantID = authResult.TenantID
	run.mu.Unlock()
	run.authResult = authResult
	run.jwtLifetime = r.cfg.JWTLifetime
	run.jwtRefreshBefore = r.cfg.JWTRefreshBefore

	var report *metrics.Report
	var err error

	switch run.Config.Type {
	case TestSmoke:
		report, err = runSmoke(ctx, run, logger)
	case TestLoad:
		report, err = runLoad(ctx, run, logger)
	case TestStress:
		report, err = runStress(ctx, run, logger)
	case TestSoak:
		report, err = runSoak(ctx, run, logger)
	case TestValidate:
		report, err = runValidate(ctx, run, logger)
	default:
		err = fmt.Errorf("unknown test type: %s", run.Config.Type)
	}

	run.mu.Lock()
	switch {
	case err != nil:
		run.Status = StatusFailed
		if report == nil {
			report = &metrics.Report{
				TestType: string(run.Config.Type),
				Status:   "error",
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
