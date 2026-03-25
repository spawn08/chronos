package sandbox

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ContainerPool maintains a pool of pre-warmed containers for reduced cold-start latency.
type ContainerPool struct {
	mu        sync.Mutex
	available []Sandbox
	inUse     map[Sandbox]bool
	factory   func() (Sandbox, error)
	maxSize   int
	maxIdle   time.Duration
	closed    bool
}

// PoolConfig configures a container pool.
type PoolConfig struct {
	// MaxSize is the maximum number of containers in the pool (default 5).
	MaxSize int
	// MaxIdleTime is how long an idle container is kept before being destroyed (default 5m).
	MaxIdleTime time.Duration
	// Factory creates new sandbox instances.
	Factory func() (Sandbox, error)
}

// NewPool creates a ContainerPool with the given configuration.
func NewPool(cfg PoolConfig) (*ContainerPool, error) {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 5
	}
	if cfg.MaxIdleTime <= 0 {
		cfg.MaxIdleTime = 5 * time.Minute
	}
	if cfg.Factory == nil {
		return nil, fmt.Errorf("container pool: factory function is required")
	}

	return &ContainerPool{
		available: make([]Sandbox, 0, cfg.MaxSize),
		inUse:     make(map[Sandbox]bool),
		factory:   cfg.Factory,
		maxSize:   cfg.MaxSize,
		maxIdle:   cfg.MaxIdleTime,
	}, nil
}

// Warmup pre-creates n containers in the pool.
func (p *ContainerPool) Warmup(ctx context.Context, n int) error {
	if n > p.maxSize {
		n = p.maxSize
	}
	for i := 0; i < n; i++ {
		sb, err := p.factory()
		if err != nil {
			return fmt.Errorf("container pool warmup: %w", err)
		}
		p.mu.Lock()
		p.available = append(p.available, sb)
		p.mu.Unlock()
	}
	return nil
}

// Acquire returns a ready sandbox from the pool. If the pool is empty,
// a new sandbox is created. Returns immediately if a warm container is available.
func (p *ContainerPool) Acquire() (Sandbox, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return nil, fmt.Errorf("container pool: pool is closed")
	}

	// Return an available container
	if len(p.available) > 0 {
		sb := p.available[len(p.available)-1]
		p.available = p.available[:len(p.available)-1]
		p.inUse[sb] = true
		return sb, nil
	}

	// Create a new one
	sb, err := p.factory()
	if err != nil {
		return nil, fmt.Errorf("container pool acquire: %w", err)
	}
	p.inUse[sb] = true
	return sb, nil
}

// Release returns a sandbox to the pool. If the pool is full, the sandbox is closed.
func (p *ContainerPool) Release(sb Sandbox) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.inUse, sb)

	if p.closed || len(p.available) >= p.maxSize {
		return sb.Close()
	}

	p.available = append(p.available, sb)
	return nil
}

// Size returns the current number of available containers.
func (p *ContainerPool) Size() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.available)
}

// InUse returns the number of containers currently in use.
func (p *ContainerPool) InUse() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.inUse)
}

// Close shuts down the pool and all containers.
func (p *ContainerPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.closed = true
	var lastErr error
	for _, sb := range p.available {
		if err := sb.Close(); err != nil {
			lastErr = err
		}
	}
	for sb := range p.inUse {
		if err := sb.Close(); err != nil {
			lastErr = err
		}
	}
	p.available = nil
	p.inUse = nil
	return lastErr
}
