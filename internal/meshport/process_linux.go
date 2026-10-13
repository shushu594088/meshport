//go:build linux

package meshport

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func detach(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func processMatches(pid int, binary string) bool {
	p, e := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	abs, _ := filepath.Abs(binary)
	return e == nil && p == abs
}
