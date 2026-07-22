//go:build unix

package sandbox

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup places the child in its own process group so the whole
// tree — including any grandchildren it spawns — can be signaled at once. On
// timeout or cancellation, exec.Cmd's Cancel hook (set below) kills the group
// rather than just the direct child, preventing orphaned processes from
// outliving the sandbox.
func isolateProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative PID targets the process group led by the child (its PGID
		// equals its PID because Setpgid was requested).
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
