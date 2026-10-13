package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// No shell, paths, unit names, command options or arbitrary arguments are accepted from callers.
func run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	return cmd.CombinedOutput()
}
func cleanup() error {
	// Mihomo normally removes these on graceful shutdown. Only our reserved table is recovered after a crash.
	for _, family := range []string{"-4", "-6"} {
		b, e := run("/usr/sbin/ip", family, "rule", "show")
		if e != nil {
			return e
		}
		for _, line := range strings.Split(string(b), "\n") {
			fields := strings.Fields(line)
			owned := false
			for i, f := range fields {
				if f == "lookup" && i+1 < len(fields) && fields[i+1] == "20220" {
					owned = true
				}
			}
			if owned && len(fields) > 0 {
				pref := strings.TrimSuffix(fields[0], ":")
				if _, e = run("/usr/sbin/ip", family, "rule", "del", "pref", pref, "lookup", "20220"); e != nil {
					return e
				}
			}
		}
		_, _ = run("/usr/sbin/ip", family, "route", "flush", "table", "20220")
	}
	// No /etc/resolv.conf edits or global nftables flushes. DNS hijacking belongs to this TUN only.
	return nil
}
func main() {
	if os.Geteuid() != 0 || len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "root and one fixed operation required")
		os.Exit(1)
	}
	var e error
	switch action := os.Args[1]; action {
	case "start", "stop", "restart", "enable", "disable":
		_, e = run("/usr/bin/systemctl", action, "meshport-mihomo.service")
	case "status":
		active, _ := run("/usr/bin/systemctl", "is-active", "meshport-mihomo.service")
		enabled, _ := run("/usr/bin/systemctl", "is-enabled", "meshport-mihomo.service")
		fmt.Printf("%s %s\n", strings.TrimSpace(string(active)), strings.TrimSpace(string(enabled)))
		return
	case "preflight":
		f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
		if err != nil {
			e = err
		} else {
			f.Close()
		}
		ifaces, err := net.Interfaces()
		if err != nil {
			e = err
		}
		up := false
		for _, i := range ifaces {
			if i.Flags&net.FlagUp != 0 && i.Flags&net.FlagLoopback == 0 {
				up = true
			}
		}
		if !up {
			e = fmt.Errorf("no active network interface")
		}
	case "arm":
		_, _ = run("/usr/bin/systemctl", "stop", "meshport-tun-rollback.timer", "meshport-tun-rollback.service")
		_, _ = run("/usr/bin/systemctl", "reset-failed", "meshport-tun-rollback.service")
		_, e = run("/usr/bin/systemd-run", "--quiet", "--unit=meshport-tun-rollback", "--on-active=60s", "--timer-property=AccuracySec=1s", "--property=User=meshport", "--property=Group=meshport", "--property=Type=oneshot", "/usr/bin/meshport", "rollback")
	case "disarm":
		_, _ = run("/usr/bin/systemctl", "stop", "meshport-tun-rollback.timer")
		return
	case "cleanup":
		e = cleanup()
	default:
		e = fmt.Errorf("operation not allowed")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, "fixed operation failed:", e)
		os.Exit(1)
	}
}
