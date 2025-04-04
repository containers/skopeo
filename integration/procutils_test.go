//go:build unix && !linux

package main

import (
	"os/exec"
)

// cmdLifecycleToParentIfPossible tries to exit if the parent process exits (only works on Linux).
func cmdLifecycleToParentIfPossible(c *exec.Cmd) {
}
