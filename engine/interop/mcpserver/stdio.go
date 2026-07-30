package mcpserver

import (
	"bufio"
	"context"
	"fmt"
	"io"
)

// maxLineBytes bounds a single JSON-RPC message read over stdio, guarding
// against a peer streaming an unbounded line and exhausting memory.
const maxLineBytes = 16 << 20 // 16 MiB

// ServeStdio runs the MCP server over newline-delimited JSON-RPC on in/out,
// blocking until in reaches EOF or ctx is canceled. Each request is dispatched
// sequentially and its response written as one line; notifications produce no
// output.
//
// A frame larger than maxLineBytes is a transport-level fault (bufio.ErrTooLong)
// and ends the stream with an error — distinct from a malformed-but-bounded
// message, which HandleMessage turns into an error response without tearing down
// the connection.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		// Copy: the scanner reuses its buffer across Scan calls.
		raw := make([]byte, len(line))
		copy(raw, line)

		resp, reply := s.HandleMessage(ctx, raw)
		if !reply {
			continue
		}
		if _, err := out.Write(append(resp, '\n')); err != nil {
			return fmt.Errorf("mcpserver: write response: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("mcpserver: read stdin: %w", err)
	}
	return nil
}
