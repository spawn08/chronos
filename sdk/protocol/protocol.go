// Package protocol defines the agent-to-agent communication protocol.
//
// The Bus is a mutex-protected message router. Replies to a SendAndWait call
// are matched by correlation id (the request's message id): each in-flight
// request registers a private, single-slot reply channel keyed by that id, and
// the delivery path dispatches the reply straight to the matching waiter. This
// means concurrent senders that share a single inbox can never receive one
// another's replies.
//
// Handler invocations run on a bounded pool of goroutines. When the pool is
// saturated, delivery fails with a back-pressure error instead of spawning
// unbounded goroutines. Per-peer inboxes are bounded buffered channels that
// likewise apply back-pressure once full. Direct agent-to-agent channels
// bypass the central router for point-to-point communication.
//
// Envelope values may be reused via the AcquireEnvelope/ReleaseEnvelope pool to
// reduce allocations in high-throughput senders; the bus itself does not pool
// envelopes internally.
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
	e, _ := envelopePool.Get().(*Envelope)
	return e
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
	defaultInboxSize   = 512
	defaultHistoryCap  = 4096
	defaultMaxHandlers = 256
)

// BusConfig tunes Bus resource limits.
type BusConfig struct {
	InboxSize  int // per-peer inbox buffer; 0 = defaultInboxSize
	HistoryCap int // max retained history entries; 0 = defaultHistoryCap
	// MaxConcurrentHandlers caps the number of handler goroutines that may run
	// at once across the whole bus; 0 = defaultMaxHandlers. When the cap is
	// reached, delivery to a handler-backed peer fails with a back-pressure
	// error rather than spawning an unbounded number of goroutines.
	MaxConcurrentHandlers int
}

// Bus is the central message router for agent-to-agent communication.
// Delivery is non-blocking: if a peer's inbox is full the send fails with
// an error rather than blocking the sender (back-pressure).
type Bus struct {
	mu    sync.RWMutex
	peers map[string]*Peer
	inbox map[string]chan *Envelope

	directMu sync.RWMutex
	directs  map[string]*DirectChannel // directKey -> channel

	// pending maps a request's correlation id (its message id) to the private
	// reply channel of the SendAndWait caller waiting for it. Guarded by
	// pendingMu so concurrent senders never contend for a shared inbox when
	// matching replies.
	pendingMu sync.Mutex
	pending   map[string]chan *Envelope

	// handlerSem bounds the number of concurrent handler goroutines. A slot is
	// acquired before a handler goroutine is spawned and released when it
	// returns; a full semaphore yields a back-pressure error.
	handlerSem chan struct{}

	histMu  sync.Mutex
	history []*Envelope
	histCap int

	seqNum    atomic.Int64
	inboxSize int
	closed    atomic.Bool
	// closedCh is closed exactly once by Close to unblock any SendAndWait
	// callers still waiting for a reply.
	closedCh chan struct{}
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
	maxHandlers := cfg.MaxConcurrentHandlers
	if maxHandlers <= 0 {
		maxHandlers = defaultMaxHandlers
	}
	return &Bus{
		peers:      make(map[string]*Peer),
		inbox:      make(map[string]chan *Envelope),
		directs:    make(map[string]*DirectChannel),
		pending:    make(map[string]chan *Envelope),
		handlerSem: make(chan struct{}, maxHandlers),
		closedCh:   make(chan struct{}),
		history:    make([]*Envelope, 0, min(hCap, 256)),
		histCap:    hCap,
		inboxSize:  iSize,
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

	b.ensureID(env)
	b.recordHistory(env)

	if env.To == "*" {
		return b.broadcast(ctx, env)
	}
	return b.deliverTo(ctx, env, env.To)
}

// SendAndWait sends an envelope and blocks until the matching reply is received
// or the context is canceled.
//
// The reply is matched by correlation id: before sending, the caller registers
// a private reply channel keyed by the request's message id. The delivery path
// dispatches the reply to that channel, so many concurrent SendAndWait calls on
// the same sender inbox each receive exactly their own reply with no
// mis-delivery and no requeue races.
func (b *Bus) SendAndWait(ctx context.Context, env *Envelope) (*Envelope, error) {
	if b.closed.Load() {
		return nil, fmt.Errorf("protocol: bus is closed")
	}

	b.mu.RLock()
	_, ok := b.inbox[env.From]
	b.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("protocol: sender %q not registered", env.From)
	}

	// Assign the correlation id up front so the waiter is registered before the
	// envelope (and therefore any reply) can be delivered.
	b.ensureID(env)
	replyCh := b.registerWaiter(env.ID)
	defer b.unregisterWaiter(env.ID)

	if err := b.Send(ctx, env); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-b.closedCh:
		return nil, fmt.Errorf("protocol: bus is closed")
	case reply := <-replyCh:
		return reply, nil
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

	// Unblock any SendAndWait callers still waiting for a reply.
	close(b.closedCh)

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

func (b *Bus) deliverToLocked(ctx context.Context, env *Envelope, agentID string) error {
	// Reply routing: if this envelope answers an in-flight SendAndWait, hand it
	// straight to the waiter registered under the correlation id. This is what
	// keeps concurrent senders that share an inbox from stealing each other's
	// replies.
	if env.ReplyTo != "" && b.routeToWaiter(env, env.ReplyTo) {
		return nil
	}

	peer, ok := b.peers[agentID]
	if !ok {
		return fmt.Errorf("protocol: recipient %q not found", agentID)
	}

	if peer.handler != nil {
		handler := peer.handler
		// Bound handler-goroutine spawning: acquire a semaphore slot before
		// spawning. A full pool applies back-pressure instead of spawning an
		// unbounded number of goroutines.
		select {
		case b.handlerSem <- struct{}{}:
		default:
			return fmt.Errorf("protocol: handler pool saturated for %q (back-pressure)", agentID)
		}
		// Propagate the caller's context so the handler honors cancellation and
		// deadlines. Delivery is asynchronous, so a fire-and-forget Send whose
		// context is canceled immediately after it returns will cancel the
		// handler too; callers that need the handler to outlive the call should
		// pass a context with an appropriate lifetime.
		hctx := ctx
		reqEnv := env
		go func() {
			defer func() { <-b.handlerSem }()
			// Panic recovery converts a misbehaving handler into an error reply
			// rather than crashing the process.
			reply, err := invokeHandler(hctx, handler, reqEnv)
			if err != nil {
				errBody, _ := json.Marshal(map[string]string{"error": err.Error()})
				reply = &Envelope{
					Type:    TypeError,
					From:    agentID,
					To:      reqEnv.From,
					ReplyTo: reqEnv.ID,
					Subject: "error",
					Body:    errBody,
				}
			}
			if reply != nil {
				reply.ReplyTo = reqEnv.ID
				reply.From = agentID
				reply.To = reqEnv.From
				if reply.CreatedAt.IsZero() {
					reply.CreatedAt = time.Now()
				}
				b.recordHistory(reply)
				b.deliverReply(reply, reqEnv.From)
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

// invokeHandler runs a peer handler, converting any panic into an error so a
// misbehaving handler fails only its own delivery rather than crashing the
// process.
func invokeHandler(ctx context.Context, handler Handler, env *Envelope) (reply *Envelope, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			reply = nil
			err = fmt.Errorf("protocol: handler panicked: %v", rec)
		}
	}()
	return handler(ctx, env)
}

// ensureID assigns a unique correlation id and creation timestamp to an
// envelope if they are not already set. It is safe to call more than once for
// the same envelope; subsequent calls are no-ops.
func (b *Bus) ensureID(env *Envelope) {
	if env.ID == "" {
		seq := b.seqNum.Add(1)
		env.ID = fmt.Sprintf("msg_%d_%d", time.Now().UnixNano(), seq)
	}
	if env.CreatedAt.IsZero() {
		env.CreatedAt = time.Now()
	}
}

// registerWaiter records a single-slot reply channel for the given correlation
// id and returns it. The buffer of one guarantees the dispatcher never blocks
// when delivering the reply, even if the waiter has already stopped listening.
func (b *Bus) registerWaiter(id string) chan *Envelope {
	ch := make(chan *Envelope, 1)
	b.pendingMu.Lock()
	b.pending[id] = ch
	b.pendingMu.Unlock()
	return ch
}

// unregisterWaiter removes the reply channel for the given correlation id.
func (b *Bus) unregisterWaiter(id string) {
	b.pendingMu.Lock()
	delete(b.pending, id)
	b.pendingMu.Unlock()
}

// routeToWaiter delivers a reply to the waiter registered under correlationID,
// if any, and reports whether it did so. The send is non-blocking thanks to the
// single-slot buffer.
func (b *Bus) routeToWaiter(reply *Envelope, correlationID string) bool {
	b.pendingMu.Lock()
	ch, ok := b.pending[correlationID]
	b.pendingMu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- reply:
	default:
	}
	return true
}

// deliverReply routes a handler-produced reply to the matching SendAndWait
// waiter when one is registered, falling back to the recipient's inbox
// otherwise. It acquires its own locks and must not be called while holding
// b.mu.
func (b *Bus) deliverReply(reply *Envelope, to string) {
	if reply.ReplyTo != "" && b.routeToWaiter(reply, reply.ReplyTo) {
		return
	}
	b.mu.RLock()
	if ch, exists := b.inbox[to]; exists {
		select {
		case ch <- reply:
		default:
		}
	}
	b.mu.RUnlock()
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
