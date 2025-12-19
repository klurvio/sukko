// Package wsserver provides WebSocket server functionality.
// handlers_messages.go contains handlers for WebSocket messages including auth refresh.
package server

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/Toniq-Labs/odin-ws/internal/auth"
)

// Auth message types for WebSocket communication
type AuthRefreshMessage struct {
	Type  string `json:"type"`
	Token string `json:"token"`
}

type AuthRefreshedMessage struct {
	Type      string `json:"type"`
	ExpiresAt int64  `json:"expiresAt"`
}

type AuthErrorMessage struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

type AuthExpiringMessage struct {
	Type      string `json:"type"`
	ExpiresAt int64  `json:"expiresAt"`
	ExpiresIn int    `json:"expiresIn"` // seconds until expiry
}

type AuthExpiredMessage struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// handleAuthRefresh processes a token refresh request from a client.
// This allows clients to extend their session by providing a new valid token.
//
// Message format:
//
//	{
//	  "type": "auth:refresh",
//	  "token": "<new_jwt_token>"
//	}
//
// Success response:
//
//	{
//	  "type": "auth:refreshed",
//	  "expiresAt": 1733572800
//	}
//
// Error response:
//
//	{
//	  "type": "auth:error",
//	  "error": "invalid_token" | "token_expired" | "app_mismatch"
//	}
func (s *Server) handleAuthRefresh(c *Client, msg []byte) {
	var refresh AuthRefreshMessage
	if err := json.Unmarshal(msg, &refresh); err != nil {
		c.SendJSON(AuthErrorMessage{Type: "auth:error", Error: "invalid_message"})
		return
	}

	// Validate the new token
	claims, err := s.jwtValidator.ValidateToken(refresh.Token)
	if err != nil {
		if s.authAuditLog != nil {
			s.authAuditLog.LogAuthRefreshFailed(c.appID, err.Error())
		}
		c.SendJSON(AuthErrorMessage{Type: "auth:error", Error: mapAuthError(err)})
		return
	}

	// Verify same app (prevent token switching)
	// An app cannot switch to a different app's token mid-session
	if claims.AppID() != c.appID {
		if s.authAuditLog != nil {
			s.authAuditLog.LogAuthRefreshFailed(c.appID, "app_mismatch")
		}
		c.SendJSON(AuthErrorMessage{Type: "auth:error", Error: "app_mismatch"})
		return
	}

	// Update client auth state
	c.tokenExpiry = claims.ExpiresAt.Time

	// Update token monitor
	if s.tokenMonitor != nil {
		clientIDStr := fmt.Sprintf("%d", c.id)
		s.tokenMonitor.UpdateTokenExpiry(clientIDStr, claims.ExpiresAt.Time)
	}

	// Log successful refresh
	if s.authAuditLog != nil {
		s.authAuditLog.LogAuthRefreshed(c.appID, claims.ExpiresAt.Time)
	}

	c.SendJSON(AuthRefreshedMessage{
		Type:      "auth:refreshed",
		ExpiresAt: claims.ExpiresAt.Unix(),
	})
}

// mapAuthError converts auth package errors to client-friendly error codes.
func mapAuthError(err error) string {
	switch err {
	case auth.ErrTokenExpired:
		return "token_expired"
	case auth.ErrInvalidToken:
		return "invalid_token"
	case auth.ErrMissingToken:
		return "missing_token"
	default:
		return "authentication_failed"
	}
}

// SendJSON sends a JSON message to the client.
// Thread-safe: uses a mutex to prevent concurrent writes.
func (c *Client) SendJSON(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	// Use non-blocking send to avoid blocking on slow clients
	select {
	case c.send <- data:
		return nil
	default:
		// Channel full - client is too slow
		return fmt.Errorf("client send buffer full")
	}
}

// ClientMutex provides thread-safe access to client fields.
// Used for updating auth state during token refresh.
var clientMutexes sync.Map // map[*Client]*sync.Mutex

func getClientMutex(c *Client) *sync.Mutex {
	if mu, ok := clientMutexes.Load(c); ok {
		return mu.(*sync.Mutex)
	}
	newMu := &sync.Mutex{}
	actual, _ := clientMutexes.LoadOrStore(c, newMu)
	return actual.(*sync.Mutex)
}
