package meshport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type App struct {
	Store          *Store
	Kernel         Kernel
	Service        Service
	ControllerPort int
}
type recovery struct {
	State   diskState `json:"state"`
	Config  []byte    `json:"config"`
	Running bool      `json:"running"`
}

func (a *App) journalPath() string { return filepath.Join(a.Store.dir, "recovery.json") }
func toDisk(st State) diskState {
	d := diskState{State: st}
	for _, s := range st.Subscriptions {
		d.Subscriptions = append(d.Subscriptions, diskSubscription{Subscription: s, YAML: s.Content})
	}
	return d
}
func fromDisk(d diskState) State {
	st := d.State
	st.Subscriptions = []Subscription{}
	for _, s := range d.Subscriptions {
		s.Subscription.Content = s.YAML
		st.Subscriptions = append(st.Subscriptions, s.Subscription)
	}
	return st
}
func (a *App) waitHealthy(secret string) error {
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if a.Kernel.Healthy(secret) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("内核未能就绪")
}
func (a *App) restoreSelections(st *State) error {
	choices := st.Selections[st.Active]
	if len(choices) == 0 {
		return nil
	}
	proxies, e := a.Kernel.Proxies(st.Secret)
	if e != nil {
		return e
	}
	for group, name := range choices {
		p, ok := proxies[group]
		if !ok || p.Type != "Selector" || !slices.Contains(p.All, name) {
			delete(choices, group)
			st.Notice = "订阅更新后，部分已选节点不存在，已恢复策略组默认选择"
			continue
		}
		if e = a.Kernel.Select(group, name, st.Secret); e != nil {
			return e
		}
	}
	return nil
}
func checkPorts(next, old Settings, running bool) error {
	for _, p := range []int{next.HTTPPort, next.SOCKSPort} {
		if running && (p == old.HTTPPort || p == old.SOCKSPort) {
			if next.AllowLAN && !old.AllowLAN {
				addresses, e := net.InterfaceAddrs()
				if e != nil {
					return e
				}
				for _, address := range addresses {
					ip, _, e := net.ParseCIDR(address.String())
					if e != nil || ip.To4() == nil || ip.IsLoopback() {
						continue
					}
					l, e := net.Listen("tcp", net.JoinHostPort(ip.String(), fmt.Sprint(p)))
					if e != nil {
						return fmt.Errorf("外部代理端口 %d 被占用，原配置已保留", p)
					}
					l.Close()
				}
			}
			continue
		}
		host := "127.0.0.1"
		if next.AllowLAN {
			host = "0.0.0.0"
		}
		l, e := net.Listen("tcp", fmt.Sprintf("%s:%d", host, p))
		if e != nil {
			return fmt.Errorf("代理端口 %d 无法监听，原配置已保留", p)
		}
		l.Close()
	}
	return nil
}
func (a *App) restore(rec recovery) error {
	st := fromDisk(rec.State)
	if current, e := a.Store.Load(); e == nil && current.Revision > st.Revision {
		st.Revision = current.Revision
	}
	if len(rec.Config) > 0 {
		if e := atomicWrite(runtimePath(a.Store.dir), rec.Config, 0600); e != nil {
			return e
		}
	} else {
		_ = os.Remove(runtimePath(a.Store.dir))
	}
	// Restore durable settings before attempting kernel operations, so a missing
	// or broken core cannot leave the management UI describing the rejected config.
	if e := a.Store.Save(st); e != nil {
		return e
	}
	st.Revision++
	if rec.Running {
		if !a.Service.Status().Running {
			if e := a.Service.Action("start"); e != nil {
				return e
			}
			if e := a.waitHealthy(st.Secret); e != nil {
				return e
			}
		}
		if e := a.Kernel.Apply(rec.Config, st.Secret); e != nil {
			_ = a.Service.Action("stop")
			return errors.New("原配置已恢复到磁盘，但内核恢复失败，代理已停止，请检查后启动")
		}
		if e := a.restoreSelections(&st); e != nil {
			return e
		}
	} else if a.Service.Status().Running {
		if e := a.Service.Action("stop"); e != nil {
			return e
		}
	}
	if a.Service.Status().Autostart != st.Settings.Autostart {
		action := "disable"
		if st.Settings.Autostart {
			action = "enable"
		}
		if e := a.Service.Action(action); e != nil {
			return e
		}
	}
	if e := a.Store.Save(st); e != nil {
		return e
	}
	return os.Remove(a.journalPath())
}

// Recover is run before the HTTP listener opens. A killed manager never commits an unverified config.
func (a *App) Recover() error {
	unlock, e := a.Store.Lock()
	if e != nil {
		return e
	}
	defer unlock()
	b, e := os.ReadFile(a.journalPath())
	if e == nil {
		var rec recovery
		if e = json.Unmarshal(b, &rec); e != nil {
			return e
		}
		return a.restore(rec)
	}
	if !os.IsNotExist(e) {
		return e
	}
	st, e := a.Store.Load()
	if e != nil {
		return e
	}
	if st.Pending != nil {
		return a.rollbackTUN(st)
	}
	return nil
}

// Prepare keeps the management service available when a privileged/kernel
// recovery fails. The durable journal/pending marker is left for a later retry.
func (a *App) Prepare() error {
	if e := a.Recover(); e == nil {
		return nil
	}
	unlock, e := a.Store.Lock()
	if e != nil {
		return e
	}
	defer unlock()
	st, e := a.Store.Load()
	if e != nil {
		return e
	}
	st.Notice = "配置恢复尚未完成，管理后台仍可使用。请检查内核与辅助程序权限，停止代理后修复配置。"
	return a.Store.Save(st)
}

// commitConfig spans disk, core and database using a durable undo journal.
// Caller holds the cross-process operation lock; the old state is unchanged on failure.
func (a *App) commitConfig(old, next State) error {
	if old.Pending != nil {
		return errors.New("请先确认 TUN 可访问或等待回滚，再修改配置")
	}
	var config []byte
	var e error
	if next.activeSub() != nil {
		config, e = renderConfig(next, a.ControllerPort)
		if e != nil {
			return e
		}
		if e = a.Kernel.Validate(config); e != nil {
			return e
		}
	}
	running := a.Service.Status().Running
	if running && len(config) == 0 {
		return errors.New("运行中的代理需要有效订阅")
	}
	if len(config) > 0 {
		if e = checkPorts(next.Settings, old.Settings, running); e != nil {
			return e
		}
		oldConfig, _ := renderConfig(old, a.ControllerPort)
		if configHasDNS(config) && !(running && configHasDNS(oldConfig)) {
			l, err := net.Listen("tcp", "127.0.0.1:1053")
			if err != nil {
				return errors.New("DNS 端口 1053 被占用，原配置已保留")
			}
			l.Close()
			packet, err := net.ListenPacket("udp", "127.0.0.1:1053")
			if err != nil {
				return errors.New("DNS UDP 端口 1053 被占用，原配置已保留")
			}
			packet.Close()
		}
	}
	previous, readErr := os.ReadFile(runtimePath(a.Store.dir))
	if readErr != nil && !os.IsNotExist(readErr) {
		return errors.New("无法读取原运行配置，应用已取消")
	}
	rec := recovery{State: toDisk(old), Config: previous, Running: running}
	b, _ := json.Marshal(rec)
	if e = atomicWrite(a.journalPath(), b, 0600); e != nil {
		return e
	}
	fail := func(cause error) error {
		if re := a.restore(rec); re != nil {
			return fmt.Errorf("%v；%v", cause, re)
		}
		if next.Pending != nil {
			_ = a.Service.Action("disarm")
		}
		return cause
	}
	if next.Pending != nil {
		if e = a.Service.Action("arm"); e != nil {
			return fail(e)
		}
	}
	if len(config) > 0 {
		if e = atomicWrite(runtimePath(a.Store.dir), config, 0600); e != nil {
			return fail(e)
		}
	} else {
		if e = os.Remove(runtimePath(a.Store.dir)); e != nil && !os.IsNotExist(e) {
			return fail(e)
		}
	}
	if running {
		listenersChanged := old.Settings.HTTPPort != next.Settings.HTTPPort || old.Settings.SOCKSPort != next.Settings.SOCKSPort || old.Settings.AllowLAN != next.Settings.AllowLAN || old.Settings.ProxyUser != next.Settings.ProxyUser || old.Settings.ProxyPassword != next.Settings.ProxyPassword
		if listenersChanged {
			e = a.Service.Action("restart")
			if e == nil {
				e = a.waitHealthy(next.Secret)
			}
		} else {
			e = a.Kernel.Apply(config, next.Secret)
		}
		if e != nil {
			return fail(e)
		}
		if checker, ok := a.Kernel.(interface{ Listening(Settings) bool }); ok && !checker.Listening(next.Settings) {
			return fail(errors.New("内核代理入口未就绪，已恢复原配置"))
		}
		if e = a.restoreSelections(&next); e != nil {
			return fail(e)
		}
	}
	if old.Settings.Autostart != next.Settings.Autostart {
		action := "disable"
		if next.Settings.Autostart {
			action = "enable"
		}
		if e = a.Service.Action(action); e != nil {
			return fail(e)
		}
	}
	if e = a.Store.Save(next); e != nil {
		return fail(e)
	}
	if e = os.Remove(a.journalPath()); e != nil {
		return e
	}
	return nil
}
func (a *App) Start(st State, restart bool) error {
	if st.activeSub() == nil {
		return errors.New("请先添加并启用有效订阅")
	}
	if st.Pending != nil {
		return errors.New("TUN 等待确认，请先确认或等待回滚")
	}
	// Persist the recovery window before systemctl start; ExecStartPre sees it and won't lock twice.
	if st.Settings.TUN {
		if e := a.Service.Action("preflight"); e != nil {
			return e
		}
		previous := st.Settings
		previous.TUN = false
		st.Pending = &PendingTUN{Deadline: time.Now().Add(60 * time.Second), Previous: previous, WasRunning: false}
		if e := a.Store.Save(st); e != nil {
			return e
		}
		if e := a.Service.Action("arm"); e != nil {
			st.Pending = nil
			_ = a.Store.Save(st)
			return e
		}
	}
	config, e := renderConfig(st, a.ControllerPort)
	if e != nil {
		return e
	}
	if e = a.Kernel.Validate(config); e != nil {
		return e
	}
	if e = checkPorts(st.Settings, st.Settings, a.Service.Status().Running); e != nil {
		return e
	}
	if e = atomicWrite(runtimePath(a.Store.dir), config, 0600); e != nil {
		return e
	}
	action := "start"
	if restart {
		action = "restart"
	}
	if e = a.Service.Action(action); e != nil {
		return e
	}
	if e = a.waitHealthy(st.Secret); e != nil {
		_ = a.Service.Action("stop")
		return e
	}
	if e = a.restoreSelections(&st); e != nil {
		_ = a.Service.Action("stop")
		return e
	}
	return a.Store.Save(st)
}

// Guard is invoked by the kernel unit at boot, even when the web manager is down.
func (a *App) Guard() error {
	st, e := a.Store.Load()
	if e != nil {
		return e
	}
	b, err := os.ReadFile(runtimePath(a.Store.dir))
	if err != nil {
		return err
	}
	if !st.Settings.TUN || !configHasTUN(b) {
		return nil
	}
	if st.Pending != nil && time.Now().Before(st.Pending.Deadline) {
		return a.Service.Action("arm")
	}
	unlock, e := a.Store.Lock()
	if e != nil {
		return e
	}
	defer unlock()
	st, e = a.Store.Load()
	if e != nil {
		return e
	}
	if !st.Settings.TUN {
		return nil
	}
	if st.Pending != nil {
		st.Settings = st.Pending.Previous
		st.Pending = nil
		st.Notice = "检测到未确认的 TUN 配置，已在内核启动前恢复"
		b, e := renderConfig(st, a.ControllerPort)
		if e != nil {
			return e
		}
		if e = atomicWrite(runtimePath(a.Store.dir), b, 0600); e != nil {
			return e
		}
		if e = a.Store.Save(st); e != nil {
			return e
		}
		return a.Service.Action("disarm")
	}
	previous := st.Settings
	previous.TUN = false
	st.Pending = &PendingTUN{Deadline: time.Now().Add(60 * time.Second), Previous: previous, WasRunning: false}
	if e = a.Store.Save(st); e != nil {
		return e
	}
	return a.Service.Action("arm")
}
func (a *App) rollbackTUN(st State) error {
	if st.Pending == nil {
		return nil
	}
	pending := st.Pending
	// Stop first so no broken routing survives a failed config reload.
	if e := a.Service.Action("stop"); e != nil {
		return e
	}
	st.Settings = pending.Previous
	st.Pending = nil
	st.Notice = "TUN 未在 60 秒内确认，已自动恢复启用前配置"
	b, e := renderConfig(st, a.ControllerPort)
	if e != nil {
		return e
	}
	if e = atomicWrite(runtimePath(a.Store.dir), b, 0600); e != nil {
		return e
	}
	if pending.WasRunning {
		if e = a.Service.Action("start"); e != nil {
			return e
		}
		if e = a.waitHealthy(st.Secret); e != nil {
			_ = a.Service.Action("stop")
			return e
		}
		if e = a.restoreSelections(&st); e != nil {
			return e
		}
	}
	if e = a.Store.Save(st); e != nil {
		return e
	}
	_ = a.Service.Action("disarm")
	return nil
}
func (a *App) RollbackDue(force bool) error {
	unlock, e := a.Store.Lock()
	if e != nil {
		return e
	}
	defer unlock()
	st, e := a.Store.Load()
	if e != nil {
		return e
	}
	if st.Pending != nil && (force || !time.Now().Before(st.Pending.Deadline)) {
		return a.rollbackTUN(st)
	}
	return nil
}
