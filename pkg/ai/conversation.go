// Package ai provides conversation state management for multi-turn AI interactions.
package ai

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// ConversationState represents the current state of a conversation.
type ConversationState string

const (
	// StateActive indicates the conversation is actively being used
	StateActive ConversationState = "active"
	// StateCompacted indicates the conversation history was compacted due to context limits
	StateCompacted ConversationState = "compacted"
	// StateEnded indicates the conversation has been ended
	StateEnded ConversationState = "ended"
)

// ConversationManager manages multiple concurrent conversations.
type ConversationManager struct {
	conversations map[string]*Conversation
	mu            sync.RWMutex
}

// Conversation represents a multi-turn conversation with an AI assistant.
type Conversation struct {
	// ID is a unique identifier for this conversation
	ID string

	// Turns contains the conversation history (user and assistant messages)
	Turns []ConversationTurn

	// Context contains the original context that started this conversation
	Context ConversationContext

	// State tracks the current state of the conversation
	State ConversationState

	// Created is when the conversation was started
	Created time.Time

	// Updated is when the conversation was last modified
	Updated time.Time

	// CompactedAt is when the conversation was last compacted (nil if never)
	CompactedAt *time.Time
}

// ConversationTurn represents a single message in the conversation.
type ConversationTurn struct {
	// Role is either "user" or "assistant"
	Role string

	// Content is the message content
	Content string

	// Timestamp is when this turn was added
	Timestamp time.Time
}

// ConversationContext contains the context that initiated a conversation.
type ConversationContext struct {
	// AlertFingerprint uniquely identifies the alert
	AlertFingerprint string

	// Cluster is the Kubernetes cluster context
	Cluster string

	// Namespace is the Kubernetes namespace
	Namespace string

	// InitialSummary contains the AI summary that was shown before conversation started
	InitialSummary string

	// Metadata contains additional context-specific data
	Metadata map[string]string
}

// NewConversationManager creates a new conversation manager.
func NewConversationManager() *ConversationManager {
	return &ConversationManager{
		conversations: make(map[string]*Conversation),
	}
}

// Start creates a new conversation with the given context.
func (cm *ConversationManager) Start(ctx ConversationContext) (*Conversation, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Generate unique conversation ID
	id, err := generateConversationID(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to generate conversation ID: %w", err)
	}

	now := time.Now()
	conv := &Conversation{
		ID:      id,
		Context: ctx,
		State:   StateActive,
		Created: now,
		Updated: now,
		Turns:   []ConversationTurn{},
	}

	cm.conversations[id] = conv
	return conv, nil
}

// AddTurn appends a new turn to the conversation.
func (cm *ConversationManager) AddTurn(id, role, content string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conv, exists := cm.conversations[id]
	if !exists {
		return fmt.Errorf("conversation %s not found", id)
	}

	if conv.State == StateEnded {
		return fmt.Errorf("conversation %s has ended", id)
	}

	turn := ConversationTurn{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	}

	conv.Turns = append(conv.Turns, turn)
	conv.Updated = time.Now()

	return nil
}

// Get retrieves a conversation by ID.
func (cm *ConversationManager) Get(id string) (*Conversation, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conv, exists := cm.conversations[id]
	if !exists {
		return nil, fmt.Errorf("conversation %s not found", id)
	}

	return conv, nil
}

// GetByAlert retrieves the active conversation for a given alert fingerprint.
// Returns nil if no active conversation exists for this alert.
func (cm *ConversationManager) GetByAlert(fingerprint string) *Conversation {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for _, conv := range cm.conversations {
		if conv.Context.AlertFingerprint == fingerprint && conv.State == StateActive {
			return conv
		}
	}

	return nil
}

// End marks a conversation as ended and returns it for persistence.
func (cm *ConversationManager) End(id string) (*Conversation, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conv, exists := cm.conversations[id]
	if !exists {
		return nil, fmt.Errorf("conversation %s not found", id)
	}

	conv.State = StateEnded
	conv.Updated = time.Now()

	return conv, nil
}

// CompactHistory summarizes older turns to reduce context size.
// Keeps the most recent keepRecent turns intact, summarizes older ones.
func (cm *ConversationManager) CompactHistory(id string, keepRecent int) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	conv, exists := cm.conversations[id]
	if !exists {
		return fmt.Errorf("conversation %s not found", id)
	}

	if len(conv.Turns) <= keepRecent {
		return nil // Nothing to compact
	}

	// Mark as compacted
	now := time.Now()
	conv.CompactedAt = &now
	conv.State = StateCompacted
	conv.Updated = now

	// Calculate how many turns to compact
	toCompact := len(conv.Turns) - keepRecent

	// Create a summary of the compacted turns
	summary := fmt.Sprintf("[Earlier conversation compacted: %d turns summarized on %s]",
		toCompact, now.Format("2006-01-02 15:04:05"))

	// Replace old turns with summary
	compactedTurn := ConversationTurn{
		Role:      "system",
		Content:   summary,
		Timestamp: now,
	}

	// Keep recent turns and prepend summary
	conv.Turns = append([]ConversationTurn{compactedTurn}, conv.Turns[toCompact:]...)

	return nil
}

// Delete removes a conversation from memory.
func (cm *ConversationManager) Delete(id string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.conversations[id]; !exists {
		return fmt.Errorf("conversation %s not found", id)
	}

	delete(cm.conversations, id)
	return nil
}

// List returns all conversation IDs.
func (cm *ConversationManager) List() []string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	ids := make([]string, 0, len(cm.conversations))
	for id := range cm.conversations {
		ids = append(ids, id)
	}
	return ids
}

// GetMessages returns the conversation turns formatted for AI client.
func (conv *Conversation) GetMessages() []Message {
	messages := make([]Message, len(conv.Turns))
	for i, turn := range conv.Turns {
		messages[i] = Message{
			Role:    turn.Role,
			Content: turn.Content,
		}
	}
	return messages
}

// TurnCount returns the number of turns in the conversation.
func (conv *Conversation) TurnCount() int {
	return len(conv.Turns)
}

// generateConversationID creates a unique ID for a conversation.
func generateConversationID(ctx ConversationContext) (string, error) {
	// Format: <cluster>-<fingerprint-prefix>-<random>
	// Example: qa3-a1b2c3d4-7f9e8a1b

	fingerprintPrefix := ctx.AlertFingerprint
	if len(fingerprintPrefix) > 8 {
		fingerprintPrefix = fingerprintPrefix[:8]
	}

	// Generate 4 random bytes (8 hex chars)
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return "", err
	}
	randHex := hex.EncodeToString(randBytes)

	return fmt.Sprintf("%s-%s-%s", ctx.Cluster, fingerprintPrefix, randHex), nil
}

// SaveCompacted saves a conversation to disk in compacted format.
// The file is saved to ~/.muxctl/conversations/<conversation-id>.json
func (cm *ConversationManager) SaveCompacted(id string) error {
	cm.mu.RLock()
	conv, exists := cm.conversations[id]
	cm.mu.RUnlock()

	if !exists {
		return fmt.Errorf("conversation not found: %s", id)
	}

	// TODO: Implement file persistence
	// For now, just mark as compacted
	cm.mu.Lock()
	now := time.Now()
	conv.CompactedAt = &now
	conv.State = StateCompacted
	cm.mu.Unlock()

	return nil
}

// LoadCompacted loads a conversation from disk.
// Returns nil if the conversation file doesn't exist.
func (cm *ConversationManager) LoadCompacted(id string) (*Conversation, error) {
	// TODO: Implement file loading
	// For now, return nil (not found)
	return nil, nil
}
