// Package protocol defines the agent-to-agent communication protocol.
//
// Agents communicate like human developers: they can send messages to each other,
// delegate tasks, share results, ask questions, and broadcast updates. The protocol
// is built around an in-process message bus that routes typed envelopes between
// registered agents.
package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// MessageType classifies the intent of an inter-agent message.
type MessageType string

const (
	// TypeTaskRequest asks another agent to perform work.
	TypeTaskRequest MessageType = "task_request"
	// TypeTaskResult carries the outcome of a delegated task.
	TypeTaskResult MessageType = "task_result"
	// TypeQuestion asks another agent for information.
	TypeQuestion MessageType = "question"
	// TypeAnswer responds to a question.
	TypeAnswer MessageType = "answer"
	// TypeBroadcast sends an update to all agents.
	TypeBroadcast MessageType = "broadcast"
	// TypeAck acknowledges receipt of a message.
	TypeAck MessageType = "ack"
	// TypeError signals a failure.
	TypeError MessageType = "error"
	// TypeHandoff transfers full ownership of a conversation/task to another agent.
	TypeHandoff MessageType = "handoff"
	// TypeStatus reports progress on a long-running task.
	TypeStatus MessageType = "status"
)

// Priority controls message ordering when an agent's inbox has multiple pending messages.
type Priority int

const (
	PriorityLow    Priority = 0
	PriorityNormal Priority = 1
	PriorityHigh   Priority = 2
	PriorityUrgent Priority = 3
)

// Envelope is the unit of communication between agents.
type Envelope struct {
	ID        string            `json:"id"`
	Type      MessageType       `json:"type"`
	From      string            `json:"from"`       // sender agent ID
	To        string            `json:"to"`         // recipient agent ID ("*" for broadcast)
	ReplyTo   string            `json:"reply_to,omitempty"` // ID of the message being replied to
	Subject   string            `json:"subject"`
	Body      json.RawMessage   `json:"body"`
	Priority  Priority          `json:"priority"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at,omitempty"`
}

// TaskPayload is the body of a TypeTaskRequest envelope.
type TaskPayload struct {
	Description string         `json:"description"`
	Input       map[string]any `json:"input,omitempty"`
	Constraints []string       `json:"constraints,omitempty"`
	Deadline    time.Time      `json:"deadline,omitempty"`
}

// ResultPayload is the body of a TypeTaskResult envelope.
type ResultPayload struct {
	TaskID  string         `json:"task_id"`
	Success bool           `json:"success"`
	Output  map[string]any `json:"output,omitempty"`
	Error   string         `json:"error,omitempty"`
	Summary string         `json:"summary,omitempty"`
}

// StatusPayload is the body of a TypeStatus envelope.
type StatusPayload struct {
	TaskID   string  `json:"task_id"`
	Progress float64 `json:"progress"` // 0.0 to 1.0
	Message  string  `json:"message"`
}

// HandoffPayload is the body of a TypeHandoff envelope.
type HandoffPayload struct {
	Reason       string         `json:"reason"`
	Conversation []ChatMessage  `json:"conversation,omitempty"`
	Context      map[string]any `json:"context,omitempty"`
}

// ChatMessage is a message in a conversation being handed off.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Handler processes an incoming envelope and optionally returns a reply.
type Handler func(ctx context.Context, env *Envelope) (*Envelope, error)

// Peer represents a registered agent in the communication bus.
type Peer struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	handler      Handler
}

// Bus is the central message router for agent-to-agent communication.
// It maintains a registry of peers and routes envelopes between them.
type Bus struct {
	mu       sync.RWMutex
	peers    map[string]*Peer
	inbox    map[string]chan *Envelope // per-agent inbox
	history  []*Envelope              // message log for observability
	histMu   sync.Mutex
	seqNum   int64
}

// NewBus creates a new communication bus.
func NewBus() *Bus {
	return &Bus{
		peers: make(map[string]*Peer),
		inbox: make(map[string]chan *Envelope),
	}
}

// Register adds an agent to the bus so it can send and receive messages.
func (b *Bus) Register(id, name, description string, capabilities []string, handler Handler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, exists := b.peers[id]; exists {
		return fmt.Errorf("protocol: agent %q already registered", id)
	}

	b.peers[id] = &Peer{
		ID:           id,
		Name:         name,
		Description:  description,
		Capabilities: capabilities,
		handler:      handler,
	}
	b.inbox[id] = make(chan *Envelope, 256)
	return nil
}

// Unregister removes an agent from the bus.
func (b *Bus) Unregister(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if ch, ok := b.inbox[id]; ok {
		close(ch)
		delete(b.inbox, id)
	}
	delete(b.peers, id)
}

// Peers returns all registered agents.
func (b *Bus) Peers() []*Peer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]*Peer, 0, len(b.peers))
	for _, p := range b.peers {
		out = append(out, p)
	}
	return out
}

// FindByCapability returns agents that advertise the given capability.
func (b *Bus) FindByCapability(capability string) []*Peer {
	b.mu.RLock()
	defer b.mu.RUnlock()

	var matches []*Peer
	for _, p := range b.peers {
		for _, c := range p.Capabilities {
			if c == capability {
				matches = append(matches, p)
				break
			}
		}
	}
	return matches
}

// Send delivers an envelope to its recipient. For broadcasts (To=="*"),
// the message is delivered to all peers except the sender.
func (b *Bus) Send(ctx context.Context, env *Envelope) error {
	if env.ID == "" {
		b.mu.Lock()
		b.seqNum++
		env.ID = fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), b.seqNum)
		b.mu.Unlock()
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now()
	}

	b.recordHistory(env)

	if env.To == "*" {
		return b.broadcast(ctx, env)
	}
	return b.deliverTo(ctx, env, env.To)
}

// SendAndWait sends an envelope and blocks until a reply is received or the context is cancelled.
func (b *Bus) SendAndWait(ctx context.Context, env *Envelope) (*Envelope, error) {
	if err := b.Send(ctx, env); err != nil {
		return nil, err
	}

	b.mu.RLock()
	inbox, ok := b.inbox[env.From]
	b.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("protocol: sender %q not registered", env.From)
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case reply, open := <-inbox:
			if !open {
				return nil, fmt.Errorf("protocol: inbox closed for %q", env.From)
			}
			if reply.ReplyTo == env.ID {
				return reply, nil
			}
			// Not the reply we're waiting for; re-queue it.
			b.mu.RLock()
			ch := b.inbox[env.From]
			b.mu.RUnlock()
			select {
			case ch <- reply:
			default:
			}
		}
	}
}

// DelegateTask sends a task request and waits for the result.
func (b *Bus) DelegateTask(ctx context.Context, from, to, subject string, task TaskPayload) (*ResultPayload, error) {
	body, _ := json.Marshal(task)
	env := &Envelope{
		Type:     TypeTaskRequest,
		From:     from,
		To:       to,
		Subject:  subject,
		Body:     body,
		Priority: PriorityNormal,
	}

	reply, err := b.SendAndWait(ctx, env)
	if err != nil {
		return nil, fmt.Errorf("protocol: delegate task: %w", err)
	}

	var result ResultPayload
	if err := json.Unmarshal(reply.Body, &result); err != nil {
		return nil, fmt.Errorf("protocol: decode task result: %w", err)
	}
	return &result, nil
}

// Ask sends a question and waits for the answer.
func (b *Bus) Ask(ctx context.Context, from, to, question string) (string, error) {
	body, _ := json.Marshal(map[string]string{"question": question})
	env := &Envelope{
		Type:     TypeQuestion,
		From:     from,
		To:       to,
		Subject:  question,
		Body:     body,
		Priority: PriorityNormal,
	}

	reply, err := b.SendAndWait(ctx, env)
	if err != nil {
		return "", fmt.Errorf("protocol: ask: %w", err)
	}
	var answer map[string]string
	if err := json.Unmarshal(reply.Body, &answer); err != nil {
		return string(reply.Body), nil
	}
	return answer["answer"], nil
}

// History returns all messages recorded by the bus (for observability/debugging).
func (b *Bus) History() []*Envelope {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	out := make([]*Envelope, len(b.history))
	copy(out, b.history)
	return out
}

func (b *Bus) broadcast(ctx context.Context, env *Envelope) error {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for id := range b.peers {
		if id == env.From {
			continue
		}
		envCopy := *env
		envCopy.To = id
		if err := b.deliverToLocked(ctx, &envCopy, id); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bus) deliverTo(ctx context.Context, env *Envelope, agentID string) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.deliverToLocked(ctx, env, agentID)
}

func (b *Bus) deliverToLocked(_ context.Context, env *Envelope, agentID string) error {
	peer, ok := b.peers[agentID]
	if !ok {
		return fmt.Errorf("protocol: recipient %q not found", agentID)
	}

	// If the peer has a synchronous handler, call it and route the reply back.
	if peer.handler != nil {
		go func() {
			reply, err := peer.handler(context.Background(), env)
			if err != nil {
				errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
				reply = &Envelope{
					Type:    TypeError,
					From:    agentID,
					To:      env.From,
					ReplyTo: env.ID,
					Subject: "error",
					Body:    errBody,
				}
			}
			if reply != nil {
				reply.ReplyTo = env.ID
				reply.From = agentID
				reply.To = env.From
				if reply.CreatedAt.IsZero() {
					reply.CreatedAt = time.Now()
				}
				b.recordHistory(reply)
				b.mu.RLock()
				if ch, ok := b.inbox[env.From]; ok {
					select {
					case ch <- reply:
					default:
					}
				}
				b.mu.RUnlock()
			}
		}()
		return nil
	}

	// Otherwise, deliver to inbox for polling.
	ch, ok := b.inbox[agentID]
	if !ok {
		return fmt.Errorf("protocol: no inbox for %q", agentID)
	}
	select {
	case ch <- env:
		return nil
	default:
		return fmt.Errorf("protocol: inbox full for %q", agentID)
	}
}

func (b *Bus) recordHistory(env *Envelope) {
	b.histMu.Lock()
	defer b.histMu.Unlock()
	b.history = append(b.history, env)
}
