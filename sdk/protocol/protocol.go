// Package protocol defines the agent-to-agent communication protocol.
//
// The bus uses lock-free delivery with object pooling, bounded inboxes with
// back-pressure, and direct agent-to-agent channels that bypass the central
// router for point-to-point communication.
package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// MessageType classifies the intent of an inter-agent message.
type MessageType string

const (
	TypeTaskRequest MessageType = "task_request"
	TypeTaskResult  MessageType = "task_result"
	TypeQuestion    MessageType = "question"
	TypeAnswer      MessageType = "answer"
	TypeBroadcast   MessageType = "broadcast"
	TypeAck         MessageType = "ack"
	TypeError       MessageType = "error"
	TypeHandoff     MessageType = "handoff"
	TypeStatus      MessageType = "status"
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
	From      string            `json:"from"`
	To        string            `json:"to"`
	ReplyTo   string            `json:"reply_to,omitempty"`
	Subject   string            `json:"subject"`
	Body      json.RawMessage   `json:"body"`
	Priority  Priority          `json:"priority"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	ExpiresAt time.Time         `json:"expires_at,omitempty"`
}

var envelopePool = sync.Pool{
	New: func() any { return &Envelope{} },
}

// AcquireEnvelope returns a pooled envelope. Call ReleaseEnvelope when done.
func AcquireEnvelope() *Envelope {
	return envelopePool.Get().(*Envelope)
}

// ReleaseEnvelope returns an envelope to the pool after clearing it.
func ReleaseEnvelope(e *Envelope) {
	*e = Envelope{}
	envelopePool.Put(e)
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
	Progress float64 `json:"progress"`
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

// ---- Direct channel for agent-to-agent bypass ----

// DirectChannel is a dedicated bidirectional pipe between two specific agents
// that bypasses the central bus for minimal-latency point-to-point messaging.
// Each direction uses a separate buffered channel to avoid head-of-line blocking.
type DirectChannel struct {
	AtoB chan *Envelope
	BtoA chan *Envelope
}

// NewDirectChannel creates a channel pair with the given buffer capacity.
func NewDirectChannel(bufSize int) *DirectChannel {
	if bufSize <= 0 {
		bufSize = 64
	}
	return &DirectChannel{
		AtoB: make(chan *Envelope, bufSize),
		BtoA: make(chan *Envelope, bufSize),
	}
}

// Close drains and closes both directions.
func (dc *DirectChannel) Close() {
	close(dc.AtoB)
	close(dc.BtoA)
}

// directKey returns a deterministic key for an unordered pair.
func directKey(a, b string) string {
	if a < b {
		return a + "\x00" + b
	}
	return b + "\x00" + a
}

// ---- Bus ----

const (
	defaultInboxSize  = 512
	defaultHistoryCap = 4096
)

// BusConfig tunes Bus resource limits.
type BusConfig struct {
	InboxSize  int // per-peer inbox buffer; 0 = defaultInboxSize
	HistoryCap int // max retained history entries; 0 = defaultHistoryCap
}

// Bus is the central message router for agent-to-agent communication.
// Delivery is non-blocking: if a peer's inbox is full the send fails with
// an error rather than blocking the sender (back-pressure).
type Bus struct {
	mu    sync.RWMutex
	peers map[string]*Peer
	inbox map[string]chan *Envelope

	directMu   sync.RWMutex
	directs    map[string]*DirectChannel // directKey -> channel

	histMu  sync.Mutex
	history []*Envelope
	histCap int

	seqNum    atomic.Int64
	inboxSize int
	closed    atomic.Bool
}

// NewBus creates a new communication bus with default settings.
func NewBus() *Bus {
	return NewBusWithConfig(BusConfig{})
}

// NewBusWithConfig creates a bus with explicit resource limits.
func NewBusWithConfig(cfg BusConfig) *Bus {
	iSize := cfg.InboxSize
	if iSize <= 0 {
		iSize = defaultInboxSize
	}
	hCap := cfg.HistoryCap
	if hCap <= 0 {
		hCap = defaultHistoryCap
	}
	return &Bus{
		peers:     make(map[string]*Peer),
		inbox:     make(map[string]chan *Envelope),
		directs:   make(map[string]*DirectChannel),
		history:   make([]*Envelope, 0, min(hCap, 256)),
		histCap:   hCap,
		inboxSize: iSize,
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
	b.inbox[id] = make(chan *Envelope, b.inboxSize)
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

// DirectChannelBetween returns (or creates) a dedicated channel between two agents.
func (b *Bus) DirectChannelBetween(agentA, agentB string, bufSize int) *DirectChannel {
	key := directKey(agentA, agentB)

	b.directMu.RLock()
	if dc, ok := b.directs[key]; ok {
		b.directMu.RUnlock()
		return dc
	}
	b.directMu.RUnlock()

	b.directMu.Lock()
	defer b.directMu.Unlock()

	if dc, ok := b.directs[key]; ok {
		return dc
	}
	dc := NewDirectChannel(bufSize)
	b.directs[key] = dc
	return dc
}

// Send delivers an envelope to its recipient.
// For broadcasts (To=="*") the message is delivered to all peers except the sender.
// Returns an error immediately if the recipient's inbox is full (back-pressure).
func (b *Bus) Send(ctx context.Context, env *Envelope) error {
	if b.closed.Load() {
		return fmt.Errorf("protocol: bus is closed")
	}

	if env.ID == "" {
		seq := b.seqNum.Add(1)
		env.ID = fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), seq)
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

// Close shuts down the bus and all direct channels.
func (b *Bus) Close() {
	if !b.closed.CompareAndSwap(false, true) {
		return
	}

	b.directMu.Lock()
	for k, dc := range b.directs {
		dc.Close()
		delete(b.directs, k)
	}
	b.directMu.Unlock()

	b.mu.Lock()
	for id, ch := range b.inbox {
		close(ch)
		delete(b.inbox, id)
	}
	b.mu.Unlock()
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

	ch, ok := b.inbox[agentID]
	if !ok {
		return fmt.Errorf("protocol: no inbox for %q", agentID)
	}
	select {
	case ch <- env:
		return nil
	default:
		return fmt.Errorf("protocol: inbox full for %q (back-pressure)", agentID)
	}
}

func (b *Bus) recordHistory(env *Envelope) {
	b.histMu.Lock()
	defer b.histMu.Unlock()

	if len(b.history) >= b.histCap {
		n := b.histCap / 4
		copy(b.history, b.history[n:])
		b.history = b.history[:len(b.history)-n]
	}
	b.history = append(b.history, env)
}
