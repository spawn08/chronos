// Example: wasm_sandbox runs an untrusted WebAssembly (WASI) module inside the
// pure-Go wazero-backed sandbox. WebAssembly gives strong isolation for free —
// the module runs in its own linear memory and can only touch the host through
// capabilities the sandbox grants (here: stdio and argv, nothing else).
//
// The example compiles a tiny Go program to WASI on the fly, so it is fully
// self-contained and needs no external .wasm file. It requires the Go toolchain
// on PATH (it invokes `GOOS=wasip1 GOARCH=wasm go build`).
//
//	go run ./examples/wasm_sandbox/
//
// To run your own module instead, build it to WASI and pass its path:
//
//	GOOS=wasip1 GOARCH=wasm go build -o prog.wasm ./yourprog
//	go run ./examples/wasm_sandbox/ prog.wasm
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spawn08/chronos/sandbox"
)

// helloSource is compiled to WASI when no module path is supplied. It echoes
// its arguments and exits, demonstrating argv passthrough and stdout capture.
const helloSource = `package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("Hello from inside the WASM sandbox!")
	for i, arg := range os.Args {
		fmt.Printf("  argv[%d] = %s\n", i, arg)
	}
}
`

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos WASM (WASI) Sandbox Example                ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	ctx := context.Background()

	// Use a caller-provided module if given, otherwise build one.
	var modulePath string
	if len(os.Args) > 1 {
		modulePath = os.Args[1]
	} else {
		path, cleanup, err := buildHelloModule()
		if err != nil {
			log.Fatalf("build WASI module: %v", err)
		}
		defer cleanup()
		modulePath = path
	}

	fmt.Printf("\nModule: %s\n", modulePath)

	// Constructing the sandbox compiles the module up front, so an invalid
	// module is rejected here rather than at execution time.
	sb, err := sandbox.NewWASMSandbox(modulePath)
	if err != nil {
		log.Fatalf("new wasm sandbox: %v", err)
	}
	defer sb.Close()

	fmt.Println("\n━━━ Running the module with arguments ━━━")
	result, err := sb.Execute(ctx, "greeter", []string{"alice", "bob"}, 10*time.Second)
	if err != nil {
		log.Fatalf("execute: %v", err)
	}
	fmt.Printf("  exit code: %d\n", result.ExitCode)
	fmt.Printf("  stdout:\n%s", result.Stdout)
	if result.Stderr != "" {
		fmt.Printf("  stderr:\n%s", result.Stderr)
	}

	fmt.Println("\n✓ WASM sandbox example completed.")
}

// buildHelloModule writes helloSource to a temp dir and compiles it to a WASI
// module using the local Go toolchain. It returns the module path and a cleanup
// func that removes the temp dir.
func buildHelloModule() (path string, cleanup func(), err error) {
	dir, err := os.MkdirTemp("", "chronos-wasm-*")
	if err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = os.RemoveAll(dir) }

	srcPath := filepath.Join(dir, "main.go")
	if err = os.WriteFile(srcPath, []byte(helloSource), 0o600); err != nil {
		cleanup()
		return "", nil, err
	}

	wasmPath := filepath.Join(dir, "hello.wasm")
	cmd := exec.Command("go", "build", "-o", wasmPath, srcPath)
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, buildErr := cmd.CombinedOutput(); buildErr != nil {
		cleanup()
		return "", nil, fmt.Errorf("%w: %s", buildErr, out)
	}
	return wasmPath, cleanup, nil
}
