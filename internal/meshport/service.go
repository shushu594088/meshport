package meshport

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ServiceStatus struct {
	Running   bool   `json:"running"`
	Autostart bool   `json:"autostart"`
	Available bool   `json:"available"`
	Driver    string `json:"driver"`
}
type Service interface {
	Status() ServiceStatus
	Action(string) error
}
type SystemService struct{ Helper string }

func (s *SystemService) run(action string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "sudo", "-n", s.Helper, action).Output()
}
func (s *SystemService) Status() ServiceStatus {
	b, e := s.run("status")
	fields := strings.Fields(string(b))
	return ServiceStatus{Running: len(fields) > 0 && fields[0] == "active", Autostart: len(fields) > 1 && fields[1] == "enabled", Available: e == nil, Driver: "systemd"}
}
func (s *SystemService) Action(action string) error {
	if _, e := s.run(action); e != nil {
		return errors.New("服务操作失败，请检查 meshport-helper 权限和 systemd 日志")
	}
	return nil
}

// ProcessService is an explicit development-only driver. Production always uses independent systemd units.
type ProcessService struct{ Binary, Dir string }

func (s *ProcessService) process() *os.Process {
	b, e := os.ReadFile(filepath.Join(s.Dir, "kernel.pid"))
	if e != nil {
		return nil
	}
	pid, e := strconv.Atoi(strings.TrimSpace(string(b)))
	if e != nil || pid <= 0 {
		return nil
	}
	p, e := os.FindProcess(pid)
	if e != nil || !processMatches(pid, s.Binary) {
		return nil
	}
	return p
}
func (s *ProcessService) Status() ServiceStatus {
	return ServiceStatus{Running: s.process() != nil, Available: true, Driver: "process (development)"}
}
func (s *ProcessService) Action(action string) error {
	switch action {
	case "start":
		if s.process() != nil {
			return nil
		}
		cmd := exec.Command(s.Binary, "-d", s.Dir, "-f", runtimePath(s.Dir))
		detach(cmd)
		if e := cmd.Start(); e != nil {
			return errors.New("无法启动 Mihomo")
		}
		if e := atomicWrite(filepath.Join(s.Dir, "kernel.pid"), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); e != nil {
			cmd.Process.Kill()
			return e
		}
		go func() { _ = cmd.Wait() }()
		return nil
	case "stop":
		p := s.process()
		if p == nil {
			return nil
		}
		_ = p.Signal(os.Interrupt)
		for i := 0; i < 30; i++ {
			if s.process() == nil {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return p.Kill()
	case "restart":
		if e := s.Action("stop"); e != nil {
			return e
		}
		return s.Action("start")
	case "disarm":
		return nil
	default:
		return errors.New("开发驱动不支持自启或 TUN；请通过 Linux 安装脚本部署")
	}
}
