package mcp

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConnect_StartFailsForMissingBinary(t *testing.T) {
	cli, err := NewClient(ServerConfig{Name: "x", Command: "/nonexistent/mcp-server-xyz-12345"})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	cerr := cli.Connect(context.Background())
	if cerr == nil {
		t.Fatal("expected start error")
	}
	if !strings.Contains(cerr.Error(), "start") {
		t.Fatalf("unexpected: %v", cerr)
	}
}

func TestConnect_InitResultUnmarshalFails(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "badinit.go")
	bin := filepath.Join(tmp, "badinit")
	prog := `package main
import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)
func main() {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var m map[string]any
		json.Unmarshal(s.Bytes(), &m)
		if m["method"] == "initialize" {
			fmt.Println(` + "`" + `{"jsonrpc":"2.0","id":1,"result":"not-an-object"}` + "`" + `)
		}
	}
}
`
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cli, err := NewClient(ServerConfig{Name: "bad", Command: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	err = cli.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "parse init") {
		t.Fatalf("want parse init error, got %v", err)
	}
}

func TestConnect_InitializeJSONRPCError(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "rpcerr.go")
	bin := filepath.Join(tmp, "rpcerr")
	prog := `package main
import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)
func main() {
	s := bufio.NewScanner(os.Stdin)
	for s.Scan() {
		var m map[string]any
		json.Unmarshal(s.Bytes(), &m)
		if m["method"] == "initialize" {
			fmt.Println(` + "`" + `{"jsonrpc":"2.0","id":1,"error":{"code":-32600,"message":"bad"}}` + "`" + `)
		}
	}
}
`
	if err := os.WriteFile(src, []byte(prog), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	cli, err := NewClient(ServerConfig{Name: "rpc", Command: bin})
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()

	err = cli.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("got %v", err)
	}
}
