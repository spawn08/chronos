package repl

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spawn08/chronos/engine/model"
	"github.com/spawn08/chronos/sdk/agent"
)

type pushMockModel struct{}

func (pushMockModel) Chat(ctx context.Context, req *model.ChatRequest) (*model.ChatResponse, error) {
	return &model.ChatResponse{Content: "ok"}, nil
}
func (pushMockModel) StreamChat(ctx context.Context, req *model.ChatRequest) (<-chan *model.ChatResponse, error) {
	return nil, io.EOF
}
func (pushMockModel) Name() string  { return "m" }
func (pushMockModel) Model() string { return "x" }

func TestStart_SlashQuit_Push(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	oldIn := os.Stdin
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = pr

	done := make(chan error, 1)
	go func() {
		_, _ = pw.WriteString("/quit\n")
		pw.Close()
		done <- r.Start()
	}()

	if err := <-done; err != nil {
		t.Errorf("Start: %v", err)
	}
	os.Stdin = oldIn
}

func TestStart_UnknownSlashCommand_Push(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	oldIn := os.Stdin
	oldErr := os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = pr
	os.Stderr = ew

	go func() {
		_, _ = pw.WriteString("/nosuchcommand\n/quit\n")
		pw.Close()
	}()

	err = r.Start()
	ew.Close()
	os.Stdin = oldIn
	os.Stderr = oldErr

	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, er)
	if err != nil {
		t.Errorf("Start: %v", err)
	}
	if !strings.Contains(errBuf.String(), "Unknown command") {
		t.Fatalf("expected unknown command on stderr, got %q", errBuf.String())
	}
}

func TestStart_AgentChat_Push(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	a, _ := agent.New("a1", "Agent1").WithModel(pushMockModel{}).Build()
	r.SetAgent(a)

	oldIn := os.Stdin
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = pr

	go func() {
		_, _ = pw.WriteString("hello\n/quit\n")
		pw.Close()
	}()

	err = r.Start()
	os.Stdin = oldIn
	if err != nil {
		t.Errorf("Start: %v", err)
	}
}

func TestStart_NoAgentPlainMessage_Push(t *testing.T) {
	store := newTestStore(t)
	r := New(store)

	oldIn := os.Stdin
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = pr

	go func() {
		_, _ = pw.WriteString("plain without agent\n/quit\n")
		pw.Close()
	}()

	err = r.Start()
	os.Stdin = oldIn
	if err != nil {
		t.Errorf("Start: %v", err)
	}
}

func TestStart_CommandHandlerError_Push(t *testing.T) {
	store := newTestStore(t)
	r := New(store)
	r.Register(Command{
		Name:    "/boom",
		Handler: func(string) error { return fmt.Errorf("handler boom") },
	})

	oldIn := os.Stdin
	oldErr := os.Stderr
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	er, ew, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = pr
	os.Stderr = ew

	go func() {
		_, _ = pw.WriteString("/boom\n/quit\n")
		pw.Close()
	}()

	_ = r.Start()
	ew.Close()
	os.Stdin = oldIn
	os.Stderr = oldErr

	var errBuf bytes.Buffer
	_, _ = io.Copy(&errBuf, er)
	if !strings.Contains(errBuf.String(), "boom") {
		t.Fatalf("expected error on stderr, got %q", errBuf.String())
	}
}
