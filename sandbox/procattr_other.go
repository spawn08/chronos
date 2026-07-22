//go:build !unix

package sandbox

import "os/exec"

// isolateProcessGroup is a no-op on platforms without POSIX process groups. The
// default exec.CommandContext behavior (killing the direct child on timeout)
// still applies.
func isolateProcessGroup(_ *exec.Cmd) {}
