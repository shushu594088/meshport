package meshport

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/yaml.v3"
	"io"
	"meshport/internal/update"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type HTTP struct {
	App          *App
	Host         string
	SecureCookie bool
	Version      string
	Assets       http.Handler
	mu           sync.Mutex
	servers      []*http.Server
	listeners    []net.Listener
	attempts     map[string][]time.Time
	testsActive  int
}

func (h *HTTP) Handler() http.Handler { return http.HandlerFunc(h.serve) }
func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func problem(w http.ResponseWriter, status int, e error) {
	jsonResponse(w, status, map[string]string{"error": e.Error()})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 9<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return errors.New("请求格式无效或内容过大")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return errors.New("请求只能包含一个 JSON 对象")
	}
	return nil
}
func (h *HTTP) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Method != "GET" && r.Method != "HEAD" {
			problem(w, 405, errors.New("不支持该请求方法"))
			return
		}
		h.Assets.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "GET" {
		origin := r.Header.Get("Origin")
		if origin != "" {
			u, e := url.Parse(origin)
			if e != nil || u.Host != r.Host {
				problem(w, 403, errors.New("请求来源不匹配，请从后台页面操作"))
				return
			}
		}
		if r.Header.Get("X-Meshport-Request") != "1" {
			problem(w, 403, errors.New("缺少请求校验标记"))
			return
		}
	}
	if r.URL.Path == "/api/auth" && r.Method == "GET" {
		st, e := h.App.Store.Load()
		if e != nil {
			problem(w, 500, errors.New("读取状态失败"))
			return
		}
		jsonResponse(w, 200, map[string]bool{"initialized": st.PasswordHash != "", "authenticated": h.authenticated(r)})
		return
	}
	if (r.URL.Path == "/api/setup" || r.URL.Path == "/api/login") && r.Method == "POST" {
		h.login(w, r)
		return
	}
	if !h.authenticated(r) {
		problem(w, 401, errors.New("登录已过期，请重新登录"))
		return
	}
	if r.URL.Path == "/api/logout" && r.Method == "POST" {
		c, _ := r.Cookie("meshport_session")
		h.App.Store.DeleteSession(c.Value)
		h.cookie(w, "", -1)
		jsonResponse(w, 200, map[string]bool{"ok": true})
		return
	}
	if r.Method == "GET" {
		h.read(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/tests/") && r.Method == "POST" {
		h.diagnose(w, r)
		return
	}
	unlock, e := h.App.Store.Lock()
	if e != nil {
		problem(w, 500, errors.New("无法锁定状态"))
		return
	}
	defer unlock()
	st, e := h.App.Store.Load()
	if e != nil {
		problem(w, 500, errors.New("读取状态失败"))
		return
	}
	// Recheck authentication after acquiring the lock (password changes revoke queued requests).
	if !h.authenticated(r) {
		problem(w, 401, errors.New("登录已过期，请重新登录"))
		return
	}
	e = h.mutate(w, r, st)
	if e != nil {
		problem(w, 400, e)
	}
}
func (h *HTTP) authenticated(r *http.Request) bool {
	c, e := r.Cookie("meshport_session")
	return e == nil && h.App.Store.Session(c.Value)
}
func (h *HTTP) cookie(w http.ResponseWriter, token string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "meshport_session", Value: token, Path: "/", HttpOnly: true, Secure: h.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: age})
}
func (h *HTTP) rateAllowed(addr string) bool {
	host, _, _ := net.SplitHostPort(addr)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.attempts == nil {
		h.attempts = map[string][]time.Time{}
	}
	now := time.Now()
	for ip, times := range h.attempts {
		if len(times) == 0 || now.Sub(times[len(times)-1]) > time.Minute {
			delete(h.attempts, ip)
		}
	}
	if len(h.attempts) >= 4096 {
		return false
	}
	recent := []time.Time{}
	for _, t := range h.attempts[host] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		return false
	}
	h.attempts[host] = append(recent, now)
	return true
}
func (h *HTTP) login(w http.ResponseWriter, r *http.Request) {
	if !h.rateAllowed(r.RemoteAddr) {
		problem(w, 429, errors.New("尝试次数过多，请一分钟后重试"))
		return
	}
	var input struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &input); e != nil {
		problem(w, 400, e)
		return
	}
	unlock, e := h.App.Store.Lock()
	if e != nil {
		problem(w, 500, e)
		return
	}
	defer unlock()
	st, e := h.App.Store.Load()
	if e != nil {
		problem(w, 500, errors.New("读取状态失败"))
		return
	}
	if r.URL.Path == "/api/setup" {
		if st.PasswordHash != "" || subtle.ConstantTimeCompare([]byte(digest(input.Token)), []byte(st.InitHash)) != 1 {
			problem(w, 403, errors.New("初始化凭证无效或已使用"))
			return
		}
		if utf8.RuneCountInString(input.Password) < 12 || len(input.Password) > 72 {
			problem(w, 400, errors.New("管理员密码至少 12 个字符，且不超过 72 字节"))
			return
		}
		hash, e := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
		if e != nil {
			problem(w, 500, e)
			return
		}
		st.PasswordHash = string(hash)
		st.InitHash = ""
		if e = h.App.Store.Save(st); e != nil {
			problem(w, 500, errors.New("保存密码失败"))
			return
		}
		_ = os.Remove(filepath.Join(h.App.Store.dir, "init-token"))
	} else if bcrypt.CompareHashAndPassword([]byte(st.PasswordHash), []byte(input.Password)) != nil {
		problem(w, 403, errors.New("密码不正确"))
		return
	}
	token, e := h.App.Store.CreateSession()
	if e != nil {
		problem(w, 500, errors.New("创建登录会话失败"))
		return
	}
	h.cookie(w, token, 12*3600)
	jsonResponse(w, 200, map[string]bool{"ok": true})
}
func settingsView(s Settings) map[string]any {
	b, _ := json.Marshal(s)
	v := map[string]any{}
	_ = json.Unmarshal(b, &v)
	delete(v, "proxy_password")
	v["has_proxy_password"] = s.ProxyPassword != ""
	return v
}
func subscriptionView(s Subscription, active string) map[string]any {
	source := "手动导入"
	if u, e := url.Parse(s.URL); e == nil && u.Hostname() != "" {
		source = u.Hostname()
	}
	return map[string]any{"id": s.ID, "name": s.Name, "source": source, "kind": s.Kind, "updated_at": s.UpdatedAt, "active": s.ID == active, "dns": s.DNS}
}
func (h *HTTP) read(w http.ResponseWriter, r *http.Request) {
	st, e := h.App.Store.Load()
	if e != nil {
		problem(w, 500, errors.New("读取状态失败"))
		return
	}
	switch r.URL.Path {
	case "/api/core", "/api/connections", "/api/logs":
		h.coreRead(w, r, st)
	case "/api/config":
		b, err := renderConfig(st, h.App.ControllerPort)
		if err != nil {
			problem(w, 409, err)
			return
		}
		var config map[string]any
		_ = yaml.Unmarshal(b, &config)
		safe, _ := yaml.Marshal(redactConfig(config))
		jsonResponse(w, 200, map[string]string{"yaml": string(safe)})
	case "/api/diagnostics":
		jsonResponse(w, 200, map[string]any{"version": h.Version, "generated_at": time.Now().UTC(), "service": h.App.Service.Status(), "settings": settingsView(st.Settings), "subscription_count": len(st.Subscriptions), "pending_tun": st.Pending != nil, "controller": "仅监听本机", "paths": map[string]string{"config": "/var/lib/meshport/runtime.yaml", "data": "/var/lib/meshport", "kernel": "/usr/bin/meshport-mihomo", "logs": "Web 日志页（脱敏）"}})
	case "/api/environment":
		u := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", st.Settings.HTTPPort)}
		if st.Settings.AllowLAN {
			u.User = url.UserPassword(st.Settings.ProxyUser, st.Settings.ProxyPassword)
		}
		proxy := u.String()
		socks := *u
		socks.Scheme = "socks5h"
		socks.Host = fmt.Sprintf("127.0.0.1:%d", st.Settings.SOCKSPort)
		var value string
		switch r.URL.Query().Get("format") {
		case "bash":
			value = "export HTTP_PROXY='" + proxy + "'\nexport HTTPS_PROXY='" + proxy + "'\nexport ALL_PROXY='" + socks.String() + "'\nexport NO_PROXY='localhost,127.0.0.1,::1'"
		case "powershell":
			value = "$env:HTTP_PROXY = '" + proxy + "'\n$env:HTTPS_PROXY = '" + proxy + "'\n$env:ALL_PROXY = '" + socks.String() + "'\n$env:NO_PROXY = 'localhost,127.0.0.1,::1'"
		case "cmd":
			if strings.Contains(proxy+socks.String(), "%") {
				problem(w, 400, errors.New("CMD 无法可靠保留编码后的代理凭证，请选择 PowerShell 或 Bash"))
				return
			}
			value = "set \"HTTP_PROXY=" + proxy + "\"\nset \"HTTPS_PROXY=" + proxy + "\"\nset \"ALL_PROXY=" + socks.String() + "\"\nset \"NO_PROXY=localhost,127.0.0.1,::1\""
		default:
			problem(w, 400, errors.New("请选择 Bash、PowerShell 或 CMD 格式"))
			return
		}
		jsonResponse(w, 200, map[string]any{"value": value, "contains_credentials": st.Settings.AllowLAN})
	case "/api/update":
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		release, err := update.Check(ctx, update.NewClient(), h.Version)
		if err != nil {
			problem(w, 503, err)
			return
		}
		_, systemd := h.App.Service.(*SystemService)
		release.CanUpdate = release.CanUpdate && systemd
		jsonResponse(w, 200, release)
	case "/api/update/status":
		status := update.Status{Phase: "idle", Message: "尚未开始更新"}
		if data, err := os.ReadFile(update.StatusPath); err == nil {
			_ = json.Unmarshal(data, &status)
		}
		jsonResponse(w, 200, map[string]any{"current": h.Version, "status": status})
	case "/api/tests/sites":
		jsonResponse(w, 200, diagnosticSites)
	case "/api/state":
		subs := []map[string]any{}
		for _, s := range st.Subscriptions {
			subs = append(subs, subscriptionView(s, st.Active))
		}
		var pending any
		if st.Pending != nil {
			pending = map[string]any{"deadline": st.Pending.Deadline}
		}
		jsonResponse(w, 200, map[string]any{"revision": st.Revision, "settings": settingsView(st.Settings), "subscriptions": subs, "active": st.Active, "service": h.App.Service.Status(), "pending_tun": pending, "notice": st.Notice})
	case "/api/nodes":
		if !h.App.Service.Status().Running {
			problem(w, 409, errors.New("内核已停止，启动代理后即可查看和选择节点"))
			return
		}
		p, e := h.App.Kernel.Proxies(st.Secret)
		if e != nil {
			problem(w, 503, e)
			return
		}
		jsonResponse(w, 200, map[string]any{"proxies": p, "selections": st.Selections[st.Active]})
	default:
		problem(w, 404, errors.New("接口不存在"))
	}
}
func downloadSubscription(rawURL string) (string, error) {
	u, e := url.Parse(rawURL)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(rawURL) > 8192 || u.Fragment != "" {
		return "", errors.New("订阅地址必须是有效的 HTTP(S) URL")
	}
	client := &http.Client{Timeout: 25 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("重定向过多")
		}
		if req.URL.Scheme != "https" && req.URL.Scheme != "http" {
			return errors.New("无效重定向")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return errors.New("不允许 HTTPS 降级")
		}
		return nil
	}}
	req, e := http.NewRequest("GET", rawURL, nil)
	if e != nil {
		return "", errors.New("无效订阅地址")
	}
	req.Header.Set("User-Agent", "Clash.Meta Meshport/0.1")
	res, e := client.Do(req)
	if e != nil {
		return "", errors.New("订阅下载失败，请检查地址、网络或凭证")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("订阅下载失败（HTTP %d）", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, maxSubscription+1))
	if e != nil {
		return "", errors.New("读取订阅失败")
	}
	if len(b) > maxSubscription {
		return "", errors.New("订阅超过 8 MiB 限制")
	}
	return string(b), nil
}
func (h *HTTP) mutate(w http.ResponseWriter, r *http.Request, old State) error {
	if old.Pending != nil && r.URL.Path != "/api/tun/confirm" && r.URL.Path != "/api/service" {
		return errors.New("TUN 等待确认，暂时不能修改配置或订阅")
	}
	a := h.App
	next := old
	ok := func() { jsonResponse(w, 200, map[string]bool{"ok": true}) }
	switch {
	case r.URL.Path == "/api/backup" && r.Method == "POST":
		var in struct {
			Password string `json:"password"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		backup, e := encryptBackup(old, in.Password)
		if e != nil {
			return e
		}
		jsonResponse(w, 200, backup)
		return nil
	case r.URL.Path == "/api/backup/restore" && r.Method == "POST":
		var in struct {
			Password string `json:"password"`
			Backup   Backup `json:"backup"`
			Revision int64  `json:"revision"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if in.Revision != old.Revision {
			problem(w, 409, errors.New("设置已在其他页面更改，请刷新后重试"))
			return nil
		}
		next, e := decryptBackup(in.Backup, in.Password)
		if e != nil {
			return e
		}
		if e = validateRestore(&next, old, a.ControllerPort); e != nil {
			return e
		}
		if e = a.commitConfig(old, next); e != nil {
			return e
		}
		ok()
		return nil
	case r.URL.Path == "/api/core/action" && r.Method == "POST":
		var in struct {
			Action string `json:"action"`
			ID     string `json:"id"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		m, yes := a.Kernel.(*Mihomo)
		if !yes {
			return errors.New("当前驱动不支持该操作")
		}
		if !a.Service.Status().Running {
			return errors.New("请先启动代理")
		}
		ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
		defer cancel()
		if e := m.CoreAction(ctx, in.Action, in.ID, old.Secret); e != nil {
			return e
		}
		ok()
		return nil
	case r.URL.Path == "/api/update" && r.Method == "POST":
		if _, ok := a.Service.(*SystemService); !ok {
			return errors.New("一键更新仅支持 Linux systemd 安装")
		}
		if e := a.Service.Action("update"); e != nil {
			return e
		}
		jsonResponse(w, 202, map[string]bool{"ok": true})
		return nil
	case r.URL.Path == "/api/subscriptions" && r.Method == "POST":
		var in struct {
			Name    string `json:"name"`
			URL     string `json:"url"`
			Content string `json:"content"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		in.Name = strings.TrimSpace(in.Name)
		if in.Name == "" || utf8.RuneCountInString(in.Name) > 80 {
			return errors.New("订阅名称需为 1–80 个字符")
		}
		if len(next.Subscriptions) >= 64 {
			return errors.New("最多保存 64 个订阅")
		}
		if (in.URL == "") == (in.Content == "") {
			return errors.New("请选择订阅 URL 或手动粘贴 YAML，不能同时提交")
		}
		content := in.Content
		var e error
		if in.URL != "" {
			content, e = downloadSubscription(in.URL)
			if e != nil {
				return e
			}
		}
		_, kind, e := parseSubscription(content)
		if e != nil {
			return e
		}
		sub := Subscription{ID: randomToken()[:16], Name: in.Name, URL: in.URL, Content: content, Kind: kind, UpdatedAt: time.Now().UTC()}
		candidate := next
		candidate.Subscriptions = append(slices.Clone(next.Subscriptions), sub)
		candidate.Active = sub.ID
		b, e := renderConfig(candidate, a.ControllerPort)
		if e != nil {
			return e
		}
		if e = a.Kernel.Validate(b); e != nil {
			return e
		}
		next.Subscriptions = append(next.Subscriptions, sub)
		if e = a.Store.Save(next); e != nil {
			return e
		}
		ok()
		return nil
	case strings.HasPrefix(r.URL.Path, "/api/subscriptions/"):
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/subscriptions/"), "/")
		id := parts[0]
		idx := slices.IndexFunc(next.Subscriptions, func(s Subscription) bool { return s.ID == id })
		if idx < 0 {
			return errors.New("订阅不存在")
		}
		if r.Method == "DELETE" && len(parts) == 1 {
			if next.Pending != nil {
				return errors.New("TUN 等待确认，暂时不能删除订阅")
			}
			if id == next.Active && a.Service.Status().Running {
				return errors.New("请先切换有效订阅或停止代理，再删除当前订阅")
			}
			next.Subscriptions = slices.Delete(slices.Clone(next.Subscriptions), idx, idx+1)
			delete(next.Selections, id)
			if next.Active == id {
				next.Active = ""
			}
			if e := a.commitConfig(old, next); e != nil {
				return e
			}
			ok()
			return nil
		}
		if r.Method != "POST" || len(parts) != 2 {
			return errors.New("无效订阅操作")
		}
		switch parts[1] {
		case "activate":
			next.Active = id
			next.Notice = ""
		case "update":
			sub := next.Subscriptions[idx]
			if sub.URL == "" {
				return errors.New("手动导入的订阅没有更新 URL，请重新导入")
			}
			content, e := downloadSubscription(sub.URL)
			if e != nil {
				return e
			}
			_, kind, e := parseSubscription(content)
			if e != nil {
				return e
			}
			sub.Content = content
			sub.Kind = kind
			sub.UpdatedAt = time.Now().UTC()
			next.Subscriptions = slices.Clone(next.Subscriptions)
			next.Subscriptions[idx] = sub
			if id != next.Active {
				candidate := next
				candidate.Active = id
				b, e := renderConfig(candidate, a.ControllerPort)
				if e != nil {
					return e
				}
				if e = a.Kernel.Validate(b); e != nil {
					return e
				}
				if e = a.Store.Save(next); e != nil {
					return e
				}
				ok()
				return nil
			}
		default:
			return errors.New("无效订阅操作")
		}
		if e := a.commitConfig(old, next); e != nil {
			return e
		}
		ok()
		return nil
	case r.URL.Path == "/api/settings" && r.Method == "PUT":
		var in struct {
			Settings       Settings     `json:"settings"`
			Revision       int64        `json:"revision"`
			DNS            *DNSSettings `json:"dns,omitempty"`
			SubscriptionID string       `json:"subscription_id,omitempty"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if in.Revision != old.Revision {
			problem(w, 409, errors.New("设置已在其他页面更改，请刷新后重试"))
			return nil
		}
		if in.DNS != nil {
			if in.SubscriptionID != old.Active || old.Active == "" {
				return errors.New("当前订阅已变化，请刷新后重新保存 DNS 设置")
			}
			if e := validateDNS(*in.DNS); e != nil {
				return e
			}
			next.Subscriptions = slices.Clone(old.Subscriptions)
			for i := range next.Subscriptions {
				if next.Subscriptions[i].ID == old.Active {
					next.Subscriptions[i].DNS = *in.DNS
				}
			}
		}
		next.Settings = in.Settings
		if next.Settings.ProxyPassword == "" {
			next.Settings.ProxyPassword = old.Settings.ProxyPassword
		}
		if e := validateSettings(next.Settings, a.ControllerPort); e != nil {
			return e
		}
		if !old.Settings.TUN && next.Settings.TUN {
			if !a.Service.Status().Running {
				return errors.New("请先启动代理，再开启 TUN")
			}
			if e := a.Service.Action("preflight"); e != nil {
				return e
			}
			host, _, _ := net.SplitHostPort(r.RemoteAddr)
			if ip := net.ParseIP(host); ip != nil {
				suffix := "/32"
				if ip.To4() == nil {
					suffix = "/128"
				}
				cidr := ip.String() + suffix
				if !slices.Contains(next.Settings.Bypass, cidr) {
					next.Settings.Bypass = append(next.Settings.Bypass, cidr)
				}
			}
			if next.Settings.WebPort != old.Settings.WebPort {
				return errors.New("请分别修改 Web 端口和开启 TUN")
			}
			next.Pending = &PendingTUN{Deadline: time.Now().Add(60 * time.Second), Previous: old.Settings, WasRunning: true}
			if sub := old.activeSub(); sub != nil && in.DNS != nil {
				dns := sub.DNS
				next.Pending.PreviousDNS = &dns
				next.Pending.SubscriptionID = sub.ID
			}
		}
		var listener net.Listener
		var e error
		if next.Settings.WebPort != old.Settings.WebPort {
			listener, e = h.listen(next.Settings.WebPort)
			if e != nil {
				return errors.New("新 Web 端口无法监听，原端口已保留")
			}
			defer func() {
				if listener != nil {
					listener.Close()
				}
			}()
		}
		if e = a.commitConfig(old, next); e != nil {
			return e
		}
		if listener != nil {
			h.adopt(listener)
			listener = nil
			go h.closeOldAfter(5 * time.Second)
		}
		ok()
		return nil
	case r.URL.Path == "/api/service" && r.Method == "POST":
		var in struct {
			Action string `json:"action"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		switch in.Action {
		case "start", "restart":
			if e := a.Start(old, in.Action == "restart"); e != nil {
				return e
			}
		case "stop":
			if old.Pending != nil {
				if e := a.rollbackTUN(old); e != nil {
					return e
				}
			}
			if e := a.Service.Action("stop"); e != nil {
				return e
			}
		default:
			return errors.New("无效服务操作")
		}
		ok()
		return nil
	case r.URL.Path == "/api/nodes/select" && r.Method == "PUT":
		if !a.Service.Status().Running {
			return errors.New("内核已停止，请先启动代理")
		}
		if old.Pending != nil {
			return errors.New("请先确认 TUN，再切换节点")
		}
		var in struct {
			Group string `json:"group"`
			Name  string `json:"name"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		p, e := a.Kernel.Proxies(old.Secret)
		if e != nil {
			return e
		}
		g, exists := p[in.Group]
		if !exists || g.Type != "Selector" || !slices.Contains(g.All, in.Name) {
			return errors.New("该策略组不允许选择此节点")
		}
		if e = a.Kernel.Select(in.Group, in.Name, old.Secret); e != nil {
			return e
		}
		if next.Selections[next.Active] == nil {
			next.Selections[next.Active] = map[string]string{}
		}
		previous := next.Selections[next.Active][in.Group]
		next.Selections[next.Active][in.Group] = in.Name
		if e = a.Store.Save(next); e != nil {
			if previous == "" {
				previous = g.Now
			}
			_ = a.Kernel.Select(in.Group, previous, old.Secret)
			return e
		}
		ok()
		return nil
	case r.URL.Path == "/api/tun/confirm" && r.Method == "POST":
		if old.Pending == nil {
			return errors.New("没有等待确认的 TUN 更改")
		}
		if !time.Now().Before(old.Pending.Deadline) {
			return errors.New("确认窗口已过期，正在恢复原配置")
		}
		if !a.Kernel.Healthy(old.Secret) {
			return errors.New("内核不可用，不能确认 TUN")
		}
		next.Pending = nil
		next.Notice = ""
		if e := a.Store.Save(next); e != nil {
			return e
		}
		_ = a.Service.Action("disarm")
		ok()
		return nil
	case r.URL.Path == "/api/password" && r.Method == "PUT":
		var in struct {
			Current  string `json:"current"`
			Password string `json:"password"`
		}
		if e := decode(w, r, &in); e != nil {
			return e
		}
		if bcrypt.CompareHashAndPassword([]byte(old.PasswordHash), []byte(in.Current)) != nil {
			return errors.New("当前密码不正确")
		}
		if utf8.RuneCountInString(in.Password) < 12 || len(in.Password) > 72 {
			return errors.New("新密码至少 12 个字符，且不超过 72 字节")
		}
		hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if e != nil {
			return e
		}
		next.PasswordHash = string(hash)
		if e = a.Store.Save(next); e != nil {
			return e
		}
		if e = a.Store.RevokeSessions(); e != nil {
			return e
		}
		h.cookie(w, "", -1)
		ok()
		return nil
	default:
		problem(w, 404, errors.New("接口不存在或请求方法不支持"))
		return nil
	}
}
func (h *HTTP) listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(h.Host, fmt.Sprint(port)))
}
func (h *HTTP) adopt(l net.Listener) {
	srv := &http.Server{Handler: h.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 45 * time.Second, WriteTimeout: 90 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16384}
	h.mu.Lock()
	h.servers = append(h.servers, srv)
	h.listeners = append(h.listeners, l)
	h.mu.Unlock()
	go func() { _ = srv.Serve(l) }()
}
func (h *HTTP) Listen(port int) error {
	l, e := h.listen(port)
	if e != nil {
		return e
	}
	h.adopt(l)
	return nil
}
func (h *HTTP) closeOldAfter(delay time.Duration) {
	time.Sleep(delay)
	h.mu.Lock()
	if len(h.servers) < 2 {
		h.mu.Unlock()
		return
	}
	old := h.servers[:len(h.servers)-1]
	h.servers = h.servers[len(h.servers)-1:]
	h.listeners = h.listeners[len(h.listeners)-1:]
	h.mu.Unlock()
	for _, srv := range old {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
}
func (h *HTTP) Shutdown(ctx context.Context) {
	h.mu.Lock()
	servers := slices.Clone(h.servers)
	h.mu.Unlock()
	for _, s := range servers {
		_ = s.Shutdown(ctx)
	}
}
