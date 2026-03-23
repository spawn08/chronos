// Example: sandbox_execution demonstrates the process sandbox for running
// untrusted commands with timeouts and output capture.
//
// No API keys needed. Uses local shell commands (echo, ls).
//
//	go run ./examples/sandbox_execution/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/spawn08/chronos/sandbox"
)

func main() {
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║    Chronos Sandbox Execution Example                 ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")

	ctx := context.Background()

	workDir, err := os.MkdirTemp("", "chronos-sandbox-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(workDir)

	sb := sandbox.NewProcessSandbox(workDir)
	defer sb.Close()

	// ── 1. Simple command execution ──

	fmt.Println("\n━━━ 1. Simple Command Execution ━━━")

	result, err := sb.Execute(ctx, "echo", []string{"Hello from Chronos sandbox!"}, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  stdout:    %q\n", result.Stdout)
	fmt.Printf("  stderr:    %q\n", result.Stderr)
	fmt.Printf("  exit code: %d\n", result.ExitCode)

	// ── 2. Multi-line output ──

	fmt.Println("\n━━━ 2. Multi-Line Output ━━━")

	result, err = sb.Execute(ctx, "sh", []string{"-c", "echo line1 && echo line2 && echo line3"}, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  stdout:\n%s", indent(result.Stdout, "    "))

	// ── 3. Capturing stderr ──

	fmt.Println("\n━━━ 3. Capturing Stderr ━━━")

	result, err = sb.Execute(ctx, "sh", []string{"-c", "echo 'normal output' && echo 'error message' >&2"}, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  stdout: %q\n", result.Stdout)
	fmt.Printf("  stderr: %q\n", result.Stderr)

	// ── 4. Non-zero exit code ──

	fmt.Println("\n━━━ 4. Non-Zero Exit Code ━━━")

	result, err = sb.Execute(ctx, "sh", []string{"-c", "echo 'about to fail' && exit 42"}, 5*time.Second)
	if err != nil {
		fmt.Printf("  error: %v\n", err)
	}
	fmt.Printf("  stdout:    %q\n", result.Stdout)
	fmt.Printf("  exit code: %d (expected: 42)\n", result.ExitCode)

	// ── 5. Environment variables ──

	fmt.Println("\n━━━ 5. Environment Variables ━━━")

	result, err = sb.Execute(ctx, "sh", []string{"-c", "echo HOME=$HOME && echo PATH length: ${#PATH}"}, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  output:\n%s", indent(result.Stdout, "    "))

	// ── 6. Working directory ──

	fmt.Println("\n━━━ 6. Working Directory ━━━")

	result, err = sb.Execute(ctx, "pwd", nil, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  working directory: %s", result.Stdout)
	fmt.Printf("  (configured: %s)\n", workDir)

	// ── 7. Timeout handling ──

	fmt.Println("\n━━━ 7. Timeout Handling ━━━")

	start := time.Now()
	result, err = sb.Execute(ctx, "sleep", []string{"10"}, 500*time.Millisecond)
	elapsed := time.Since(start)
	if err != nil {
		fmt.Printf("  timeout triggered after %v: %v\n", elapsed.Round(time.Millisecond), err)
	} else {
		fmt.Printf("  exit code: %d (expected non-zero due to timeout)\n", result.ExitCode)
		fmt.Printf("  elapsed: %v (should be ~500ms, not 10s)\n", elapsed.Round(time.Millisecond))
	}

	// ── 8. File I/O in sandbox ──

	fmt.Println("\n━━━ 8. File I/O in Sandbox ━━━")

	result, err = sb.Execute(ctx, "sh", []string{"-c",
		"echo 'hello world' > test.txt && cat test.txt && wc -c test.txt",
	}, 5*time.Second)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("  output:\n%s", indent(result.Stdout, "    "))

	fmt.Println("\n✓ Sandbox Execution example completed.")
}

func indent(s, prefix string) string {
	result := ""
	for _, line := range splitLines(s) {
		result += prefix + line + "\n"
	}
	return result
}

func splitLines(s string) []string {
	var lines []string
	current := ""
	for _, c := range s {
		if c == '\n' {
			lines = append(lines, current)
			current = ""
		} else {
			current += string(c)
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
