package sandbox

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type stubSandbox struct {
	id       int
	closeErr error
}

func (s *stubSandbox) Execute(context.Context, string, []string, time.Duration) (*Result, error) {
	return &Result{}, nil
}

func (s *stubSandbox) Close() error {
	return s.closeErr
}

func TestNewPool_NilFactory(t *testing.T) {
	_, err := NewPool(PoolConfig{Factory: nil})
	if err == nil {
		t.Fatal("expected error for nil factory")
	}
}

func TestNewPool_Defaults(t *testing.T) {
	var n atomic.Int32
	p, err := NewPool(PoolConfig{
		MaxSize:     0,
		MaxIdleTime: 0,
		Factory: func() (Sandbox, error) {
			n.Add(1)
			return &stubSandbox{id: int(n.Load())}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.maxSize != 5 {
		t.Errorf("maxSize = %d, want 5", p.maxSize)
	}
	_ = p.Close()
}

func TestContainerPool_Warmup_CapsAtMaxSize(t *testing.T) {
	var created atomic.Int32
	p, err := NewPool(PoolConfig{
		MaxSize: 2,
		Factory: func() (Sandbox, error) {
			created.Add(1)
			return &stubSandbox{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if err := p.Warmup(context.Background(), 100); err != nil {
		t.Fatalf("Warmup: %v", err)
	}
	if created.Load() != 2 {
		t.Errorf("created %d sandboxes, want 2", created.Load())
	}
	if p.Size() != 2 {
		t.Errorf("Size = %d, want 2", p.Size())
	}
}

func TestContainerPool_Warmup_FactoryError(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 3,
		Factory: func() (Sandbox, error) {
			return nil, errors.New("factory boom")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	if err := p.Warmup(context.Background(), 1); err == nil {
		t.Fatal("expected warmup error")
	}
}

func TestContainerPool_Acquire_FromPool(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 5,
		Factory: func() (Sandbox, error) { return &stubSandbox{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_ = p.Warmup(context.Background(), 1)
	sb, err := p.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if p.Size() != 0 {
		t.Errorf("Size = %d after acquire", p.Size())
	}
	if p.InUse() != 1 {
		t.Errorf("InUse = %d", p.InUse())
	}
	_ = sb
	_ = p.Release(sb)
	if p.Size() != 1 {
		t.Errorf("Size = %d after release", p.Size())
	}
}

func TestContainerPool_Release_WhenFullClosesExtra(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 1,
		Factory: func() (Sandbox, error) { return &stubSandbox{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	a, _ := p.Acquire()
	b, _ := p.Acquire()
	_ = p.Release(a)
	// Pool has 1 available, maxSize 1 — releasing b should Close b
	if err := p.Release(b); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestContainerPool_Acquire_Closed(t *testing.T) {
	p, err := NewPool(PoolConfig{
		MaxSize: 2,
		Factory: func() (Sandbox, error) { return &stubSandbox{}, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Close()
	_, err = p.Acquire()
	if err == nil {
		t.Fatal("expected error acquiring from closed pool")
	}
}

func TestContainerPool_Close_PropagatesCloseError(t *testing.T) {
	errFail := errors.New("close failed")
	sb := &stubSandbox{closeErr: errFail}
	p, err := NewPool(PoolConfig{
		MaxSize: 2,
		Factory: func() (Sandbox, error) { return sb, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.Warmup(context.Background(), 1)
	if err := p.Close(); !errors.Is(err, errFail) {
		t.Errorf("Close err = %v, want %v", err, errFail)
	}
}
