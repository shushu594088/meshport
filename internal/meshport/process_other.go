//go:build !linux

package meshport

import "os/exec"

func detach(c *exec.Cmd)                         {}
func processMatches(pid int, binary string) bool { return false }
