//go:build linux

package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func command(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	if cmd.Run() != nil {
		return fmt.Errorf("更新操作失败：%s", filepath.Base(name))
	}
	return nil
}
func saveStatus(phase, message, version string) {
	b, _ := json.Marshal(Status{Phase: phase, Message: message, Version: version, UpdatedAt: time.Now().UTC()})
	_ = writeFile(StatusPath, b, 0644)
}
func osRelease(data string) (string, string) {
	values := map[string]string{}
	for _, line := range strings.Split(data, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = strings.Trim(value, `"'`)
		}
	}
	return values["ID"], values["VERSION_ID"]
}

// Run is root-only and accepts no caller-supplied download URL, destination or executable.
func Run(current string, port int) (err error) {
	if os.Geteuid() != 0 {
		return errors.New("请使用 sudo meshport update 更新")
	}
	if port < 1024 || port > 65535 {
		return errors.New("Web 端口无效")
	}
	lock, e := os.OpenFile("/run/meshport-update.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer lock.Close()
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		return errors.New("已有更新正在进行")
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	defer func() {
		if err != nil {
			saveStatus("failed", err.Error(), current)
		}
	}()
	saveStatus("checking", "正在检查最新稳定版本", current)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	client := NewClient()
	release, e := Check(ctx, client, current)
	if e != nil {
		return e
	}
	if !release.Available {
		saveStatus("done", "当前已是最新稳定版本", current)
		return nil
	}
	if !release.CanUpdate {
		return errors.New("当前系统或架构不支持自动更新")
	}
	osData, e := os.ReadFile("/etc/os-release")
	if e != nil {
		return e
	}
	distro, osVersion := osRelease(string(osData))
	root := Repository + "/releases/download/v" + release.Latest + "/"
	saveStatus("downloading", "正在下载更新包与校验清单", release.Latest)
	manifest, e := Fetch(ctx, client, root+"INSTALL-TARGETS.tsv", 64<<10)
	if e != nil {
		return e
	}
	asset, e := selectAsset(string(manifest), distro, osVersion, runtime.GOARCH, release.Latest)
	if e != nil {
		return e
	}
	sums, e := Fetch(ctx, client, root+"SHA256SUMS", 1<<20)
	if e != nil {
		return e
	}
	data, e := Fetch(ctx, client, root+asset, 200<<20)
	if e != nil {
		return e
	}
	saveStatus("verifying", "正在校验 SHA-256 与更新包", release.Latest)
	if e = verify(data, string(sums), asset); e != nil {
		return e
	}
	files, e := unpack(data)
	if e != nil {
		return e
	}
	tmp, e := os.MkdirTemp("", "meshport-update-")
	if e != nil {
		return e
	}
	defer os.RemoveAll(tmp)
	candidate := filepath.Join(tmp, "meshport")
	if e = os.WriteFile(candidate, files["meshport"], 0700); e != nil {
		return e
	}
	versionCtx, versionCancel := context.WithTimeout(ctx, 10*time.Second)
	output, e := exec.CommandContext(versionCtx, candidate, "version").Output()
	versionCancel()
	if e != nil || strings.TrimSpace(string(output)) != release.Latest {
		return errors.New("更新包程序版本检查失败")
	}
	if e = os.WriteFile(filepath.Join(tmp, "sudoers"), []byte(Sudoers), 0600); e != nil {
		return e
	}
	if e = command("/usr/sbin/visudo", "-cf", filepath.Join(tmp, "sudoers")); e != nil {
		return e
	}
	items := []replacement{
		{target: "/usr/bin/meshport", data: files["meshport"], mode: 0755},
		{target: "/usr/libexec/meshport-helper", data: files["meshport-helper"], mode: 0755},
		{target: "/etc/systemd/system/meshport.service", data: files["meshport.service"], mode: 0644},
		{target: "/etc/systemd/system/meshport-mihomo.service", data: files["meshport-mihomo.service"], mode: 0644},
		{target: "/etc/sudoers.d/meshport", data: []byte(Sudoers), mode: 0440},
	}
	wasRunning := command("/usr/bin/systemctl", "is-active", "--quiet", "meshport.service") == nil
	saveStatus("installing", "正在替换程序，配置与订阅保留；后台将短暂重启", release.Latest)
	stop := func() error {
		if wasRunning {
			return command("/usr/bin/systemctl", "stop", "meshport.service")
		}
		return nil
	}
	restore := func() error {
		if e := command("/usr/bin/systemctl", "daemon-reload"); e != nil {
			return e
		}
		if wasRunning {
			return command("/usr/bin/systemctl", "start", "meshport.service")
		}
		return nil
	}
	err = installFiles(items, stop, func() error {
		if e := command("/usr/bin/systemctl", "daemon-reload"); e != nil {
			return e
		}
		if wasRunning {
			saveStatus("restarting", "正在重启并检查管理后台", release.Latest)
			if e = command("/usr/bin/systemctl", "start", "meshport.service"); e != nil {
				return e
			}
			healthy := false
			health := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
			for i := 0; i < 20; i++ {
				res, e := health.Get(fmt.Sprintf("http://127.0.0.1:%d/api/auth", port))
				if e == nil {
					var auth map[string]bool
					valid := json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&auth) == nil
					res.Body.Close()
					if res.StatusCode == 200 && valid {
						if _, ok := auth["initialized"]; ok {
							healthy = true
							break
						}
					}
				}
				time.Sleep(500 * time.Millisecond)
			}
			if !healthy {
				return errors.New("新版本后台未能通过健康检查")
			}
		}
		return nil
	}, restore)
	if err != nil {
		return err
	}
	saveStatus("done", "更新完成，配置与订阅已保留", release.Latest)
	return nil
}
