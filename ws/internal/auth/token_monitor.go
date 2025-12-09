// Package auth provides JWT authentication for WebSocket connections.
package auth

import (
	"sync"
	"time"
)

const (
	// DefaultCheckInterval is how often the monitor checks for expiring tokens.
	DefaultCheckInterval = 1 * time.Minute

	// DefaultExpiryWarning is how long before expiry to warn clients.
	DefaultExpiryWarning = 5 * time.Minute
)

// TrackedClient represents a client being monitored for token expiry.
type TrackedClient struct {
	ClientID    string
	AppID       string // App ID from JWT (identifies the connecting application)
	RemoteAddr  string
	TokenExpiry time.Time
	Warned      bool // Whether client has been warned about expiring token
}

// TokenMonitorCallbacks defines the callbacks the monitor uses to interact with clients.
type TokenMonitorCallbacks interface {
	// OnTokenExpiring is called when a token is about to expire.
	// The client should be sent a warning message.
	OnTokenExpiring(clientID string, expiresIn time.Duration)

	// OnTokenExpired is called when a token has expired.
	// The client connection should be closed.
	OnTokenExpired(clientID string)
}

// TokenMonitor monitors authenticated clients and handles token expiry.
// It runs a background goroutine that periodically checks all tracked clients.
// Thread-safe for concurrent use.
type TokenMonitor struct {
	mu            sync.RWMutex
	clients       map[string]*TrackedClient // clientID -> TrackedClient
	callbacks     TokenMonitorCallbacks
	auditLog      *AuditLogger
	checkInterval time.Duration
	expiryWarning time.Duration
	stopCh        chan struct{}
	wg            sync.WaitGroup
}

// NewTokenMonitor creates a new token monitor.
// callbacks: required, handles client notifications
// auditLog: optional, logs auth events (can be nil)
func NewTokenMonitor(callbacks TokenMonitorCallbacks, auditLog *AuditLogger) *TokenMonitor {
	return &TokenMonitor{
		clients:       make(map[string]*TrackedClient),
		callbacks:     callbacks,
		auditLog:      auditLog,
		checkInterval: DefaultCheckInterval,
		expiryWarning: DefaultExpiryWarning,
		stopCh:        make(chan struct{}),
	}
}

// SetCheckInterval sets the interval between expiry checks.
// Must be called before Start().
func (m *TokenMonitor) SetCheckInterval(interval time.Duration) {
	m.checkInterval = interval
}

// SetExpiryWarning sets how long before expiry to warn clients.
// Must be called before Start().
func (m *TokenMonitor) SetExpiryWarning(warning time.Duration) {
	m.expiryWarning = warning
}

// Start begins the background monitoring goroutine.
func (m *TokenMonitor) Start() {
	m.wg.Add(1)
	go m.monitorLoop()
}

// Stop stops the background monitoring goroutine.
func (m *TokenMonitor) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

// TrackClient adds a client to be monitored.
func (m *TokenMonitor) TrackClient(clientID, appID, remoteAddr string, tokenExpiry time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.clients[clientID] = &TrackedClient{
		ClientID:    clientID,
		AppID:       appID,
		RemoteAddr:  remoteAddr,
		TokenExpiry: tokenExpiry,
		Warned:      false,
	}
}

// UntrackClient removes a client from monitoring (e.g., on disconnect).
func (m *TokenMonitor) UntrackClient(clientID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.clients, clientID)
}

// UpdateTokenExpiry updates the token expiry for a client (after refresh).
func (m *TokenMonitor) UpdateTokenExpiry(clientID string, newExpiry time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	client, ok := m.clients[clientID]
	if !ok {
		return false
	}

	client.TokenExpiry = newExpiry
	client.Warned = false // Reset warning flag after refresh
	return true
}

// GetClientExpiry returns the token expiry time for a client.
func (m *TokenMonitor) GetClientExpiry(clientID string) (time.Time, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	client, ok := m.clients[clientID]
	if !ok {
		return time.Time{}, false
	}
	return client.TokenExpiry, true
}

// ClientCount returns the number of clients being monitored.
func (m *TokenMonitor) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

func (m *TokenMonitor) monitorLoop() {
	defer m.wg.Done()

	ticker := time.NewTicker(m.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.checkExpiries()
		}
	}
}

func (m *TokenMonitor) checkExpiries() {
	now := time.Now()
	warningThreshold := now.Add(m.expiryWarning)

	// Take a snapshot of clients to check (copy-on-read pattern)
	m.mu.RLock()
	snapshot := make([]*TrackedClient, 0, len(m.clients))
	for _, client := range m.clients {
		// Copy the struct to avoid holding lock during callbacks
		clientCopy := *client
		snapshot = append(snapshot, &clientCopy)
	}
	m.mu.RUnlock()

	// Process clients outside the lock
	var expiredIDs []string
	var warningClients []struct {
		id        string
		expiresIn time.Duration
		client    *TrackedClient
	}

	for _, client := range snapshot {
		if client.TokenExpiry.Before(now) {
			// Token has expired
			expiredIDs = append(expiredIDs, client.ClientID)

			if m.auditLog != nil {
				m.auditLog.LogTokenExpired(client.ClientID, client.AppID, client.RemoteAddr)
			}
		} else if client.TokenExpiry.Before(warningThreshold) && !client.Warned {
			// Token expiring soon, not yet warned
			expiresIn := client.TokenExpiry.Sub(now)
			warningClients = append(warningClients, struct {
				id        string
				expiresIn time.Duration
				client    *TrackedClient
			}{client.ClientID, expiresIn, client})

			if m.auditLog != nil {
				m.auditLog.LogTokenExpiring(client.ClientID, client.AppID, client.RemoteAddr, expiresIn)
			}
		}
	}

	// Mark warned clients
	if len(warningClients) > 0 {
		m.mu.Lock()
		for _, wc := range warningClients {
			if c, ok := m.clients[wc.id]; ok {
				c.Warned = true
			}
		}
		m.mu.Unlock()
	}

	// Send warnings (outside lock)
	for _, wc := range warningClients {
		m.callbacks.OnTokenExpiring(wc.id, wc.expiresIn)
	}

	// Handle expired tokens (outside lock, callbacks will remove from tracking)
	for _, clientID := range expiredIDs {
		m.callbacks.OnTokenExpired(clientID)
	}
}
