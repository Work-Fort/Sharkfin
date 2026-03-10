// SPDX-License-Identifier: AGPL-3.0-or-later
package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Work-Fort/sharkfin/pkg/domain"
)

// SessionManager manages identity tokens, MCP sessions, and presence state.
type SessionManager struct {
	mu          sync.RWMutex
	tokens      map[string]*IdentityToken // token string → state
	mcpSessions map[string]*MCPSession    // MCP session ID → session
	onlineUsers map[string]string         // username → token
	store       domain.UserStore
}

// IdentityToken tracks the lifecycle of an identity token.
type IdentityToken struct {
	Token        string
	Username     string // empty until identified
	Identified   bool
	MCPSessionID string
	PresenceDone chan struct{} // closed when presence should disconnect
	HasPresence  bool
	CreatedAt    time.Time
	presenceConn *websocket.Conn
	presenceMu   sync.Mutex
}

// MCPSession links an MCP session ID to a token and user.
type MCPSession struct {
	ID       string
	TokenID  string
	Username string
}

// NewSessionManager creates a new session manager.
func NewSessionManager(store domain.UserStore) *SessionManager {
	return &SessionManager{
		tokens:      make(map[string]*IdentityToken),
		mcpSessions: make(map[string]*MCPSession),
		onlineUsers: make(map[string]string),
		store:       store,
	}
}

// CreateIdentityToken generates a new pending identity token.
func (sm *SessionManager) CreateIdentityToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	token := hex.EncodeToString(b)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.tokens[token] = &IdentityToken{
		Token:        token,
		PresenceDone: make(chan struct{}),
		CreatedAt:    time.Now(),
	}
	return token
}

// AttachPresence links a presence connection to a token.
// Returns a channel that is closed when the presence should disconnect.
func (sm *SessionManager) AttachPresence(token string, conn *websocket.Conn) (<-chan struct{}, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	it, ok := sm.tokens[token]
	if !ok {
		return nil, fmt.Errorf("invalid token")
	}
	it.HasPresence = true
	it.presenceConn = conn
	return it.PresenceDone, nil
}

// PresenceWrite serializes a write to the token's presence WebSocket connection.
// All writes to the presence conn (pings, notifications) must go through this
// or SendNotification to satisfy gorilla/websocket's single-writer constraint.
func (sm *SessionManager) PresenceWrite(token string, messageType int, data []byte) error {
	sm.mu.RLock()
	it, ok := sm.tokens[token]
	sm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("invalid token")
	}
	it.presenceMu.Lock()
	defer it.presenceMu.Unlock()
	if it.presenceConn == nil {
		return fmt.Errorf("no presence connection")
	}
	it.presenceConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return it.presenceConn.WriteMessage(messageType, data)
}

// SendNotification sends a JSON notification to a user's presence WebSocket connection.
func (sm *SessionManager) SendNotification(username string, data []byte) error {
	sm.mu.RLock()
	tokenStr, ok := sm.onlineUsers[username]
	if !ok {
		sm.mu.RUnlock()
		return fmt.Errorf("user not online")
	}
	it := sm.tokens[tokenStr]
	sm.mu.RUnlock()

	it.presenceMu.Lock()
	defer it.presenceMu.Unlock()
	if it.presenceConn == nil {
		return fmt.Errorf("no presence connection")
	}
	it.presenceConn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return it.presenceConn.WriteMessage(websocket.TextMessage, data)
}

// Register creates a new user and associates the token with them.
// Returns an MCP session ID.
func (sm *SessionManager) Register(token, username, password string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	it, ok := sm.tokens[token]
	if !ok {
		return "", fmt.Errorf("invalid token")
	}
	if it.Identified {
		return "", fmt.Errorf("session already identified")
	}

	// Check if user is already online
	if _, online := sm.onlineUsers[username]; online {
		return "", fmt.Errorf("user already online: %s", username)
	}

	// Create user in database
	if _, err := sm.store.CreateUser(username, password); err != nil {
		return "", fmt.Errorf("create user: %w", err)
	}

	return sm.associateToken(it, username)
}

// Identify associates a token with an existing user.
// Returns an MCP session ID.
func (sm *SessionManager) Identify(token, username, password string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	it, ok := sm.tokens[token]
	if !ok {
		return "", fmt.Errorf("invalid token")
	}
	if it.Identified {
		return "", fmt.Errorf("session already identified")
	}

	// Check if user is already online
	if _, online := sm.onlineUsers[username]; online {
		return "", fmt.Errorf("user already online: %s", username)
	}

	// Verify user exists
	if _, err := sm.store.GetUserByUsername(username); err != nil {
		return "", fmt.Errorf("user not found: %s", username)
	}

	return sm.associateToken(it, username)
}

// associateToken links a token to a username and creates an MCP session.
// Caller must hold sm.mu.
func (sm *SessionManager) associateToken(it *IdentityToken, username string) (string, error) {
	sessionID := generateSessionID()

	it.Identified = true
	it.Username = username
	it.MCPSessionID = sessionID

	sm.onlineUsers[username] = it.Token
	sm.mcpSessions[sessionID] = &MCPSession{
		ID:       sessionID,
		TokenID:  it.Token,
		Username: username,
	}

	return sessionID, nil
}

// GetSession returns the MCP session for a given session ID.
func (sm *SessionManager) GetSession(mcpSessionID string) (*MCPSession, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.mcpSessions[mcpSessionID]
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	return session, nil
}

// IsUserOnline returns true if the user has an active presence connection.
func (sm *SessionManager) IsUserOnline(username string) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	_, ok := sm.onlineUsers[username]
	return ok
}

// DisconnectPresence removes the presence for a token and marks the user offline.
func (sm *SessionManager) DisconnectPresence(token string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	it, ok := sm.tokens[token]
	if !ok {
		return
	}

	// Nil out presence connection so concurrent SendNotification won't write
	it.presenceMu.Lock()
	it.presenceConn = nil
	it.presenceMu.Unlock()

	// Close the done channel to signal the presence handler
	select {
	case <-it.PresenceDone:
		// already closed
	default:
		close(it.PresenceDone)
	}

	// Remove from online users
	if it.Username != "" {
		delete(sm.onlineUsers, it.Username)
	}

	// Clean up MCP session
	if it.MCPSessionID != "" {
		delete(sm.mcpSessions, it.MCPSessionID)
	}

	delete(sm.tokens, token)
}

func generateSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return hex.EncodeToString(b)
}
