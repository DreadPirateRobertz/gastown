// Package mail provides messaging for agent communication via beads.
package mail

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Priority levels for messages.
type Priority string

const (
	// PriorityNormal is the default priority.
	PriorityNormal Priority = "normal"

	// PriorityHigh indicates an urgent message.
	PriorityHigh Priority = "high"
)

// Message represents a mail message between agents.
// This is the GGT-side representation; it gets translated to/from beads messages.
type Message struct {
	// ID is a unique message identifier (beads issue ID like "bd-abc123").
	ID string `json:"id"`

	// From is the sender address (e.g., "gastown/Toast" or "mayor/").
	From string `json:"from"`

	// To is the recipient address.
	To string `json:"to"`

	// Subject is a brief summary.
	Subject string `json:"subject"`

	// Body is the full message content.
	Body string `json:"body"`

	// Timestamp is when the message was sent.
	Timestamp time.Time `json:"timestamp"`

	// Read indicates if the message has been read (closed in beads).
	Read bool `json:"read"`

	// Priority is the message priority.
	Priority Priority `json:"priority"`
}

// NewMessage creates a new message with a generated ID (for legacy JSONL mode).
func NewMessage(from, to, subject, body string) *Message {
	return &Message{
		ID:        generateID(),
		From:      from,
		To:        to,
		Subject:   subject,
		Body:      body,
		Timestamp: time.Now(),
		Read:      false,
		Priority:  PriorityNormal,
	}
}

// generateID creates a random message ID.
func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "msg-" + hex.EncodeToString(b)
}

// BeadsMessage represents a message as returned by bd mail commands.
type BeadsMessage struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`       // Subject
	Description string    `json:"description"` // Body
	Sender      string    `json:"sender"`      // From identity
	Assignee    string    `json:"assignee"`    // To identity
	Priority    int       `json:"priority"`    // 0=urgent, 2=normal
	Status      string    `json:"status"`      // open=unread, closed=read
	CreatedAt   time.Time `json:"created_at"`
}

// ToMessage converts a BeadsMessage to a GGT Message.
func (bm *BeadsMessage) ToMessage() *Message {
	priority := PriorityNormal
	if bm.Priority == 0 {
		priority = PriorityHigh
	}

	return &Message{
		ID:        bm.ID,
		From:      identityToAddress(bm.Sender),
		To:        identityToAddress(bm.Assignee),
		Subject:   bm.Title,
		Body:      bm.Description,
		Timestamp: bm.CreatedAt,
		Read:      bm.Status == "closed",
		Priority:  priority,
	}
}

// addressToIdentity converts a GGT address to a beads identity.
//
// Examples:
//   - "mayor/" → "mayor"
//   - "gastown/Toast" → "gastown-Toast"
//   - "gastown/refinery" → "gastown-refinery"
//   - "gastown/" → "gastown" (rig broadcast)
func addressToIdentity(address string) string {
	// Trim trailing slash
	if len(address) > 0 && address[len(address)-1] == '/' {
		address = address[:len(address)-1]
	}

	// Mayor special case
	if address == "mayor" {
		return "mayor"
	}

	// Replace / with - for beads identity
	// gastown/Toast → gastown-Toast
	result := ""
	for _, c := range address {
		if c == '/' {
			result += "-"
		} else {
			result = result + string(c)
		}
	}
	return result
}

// identityToAddress converts a beads identity back to a GGT address.
//
// Examples:
//   - "mayor" → "mayor/"
//   - "gastown-Toast" → "gastown/Toast"
//   - "gastown-refinery" → "gastown/refinery"
func identityToAddress(identity string) string {
	if identity == "mayor" {
		return "mayor/"
	}

	// Find first dash and replace with /
	// gastown-Toast → gastown/Toast
	for i, c := range identity {
		if c == '-' {
			return identity[:i] + "/" + identity[i+1:]
		}
	}

	// No dash found, return as-is with trailing slash
	return identity + "/"
}
