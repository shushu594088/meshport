package meshport

import (
	"bytes"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const nodesYAML = `proxies:
  - {name: local-direct, type: direct}
  - {name: backup-direct, type: direct}
`
const fullYAML = `proxies:
  - {name: local-direct, type: direct}
proxy-groups:
  - {name: Choice, type: select, proxies: [local-direct, DIRECT]}
rules: ["MATCH,Choice"]
port: 6666
external-controller: 0.0.0.0:9999
secret: subscription-secret
tun: {enable: true}
listeners: [{name: evil, type: mixed, port: 6667}]
`

type fakeKernel struct {
	validErr, applyErr error
	healthy            bool
	proxies            map[string]Proxy
	applied            []byte
	selected           map[string]string
}

func (k *fakeKernel) Validate([]byte) error { return k.validErr }
func (k *fakeKernel) Apply(b []byte, _ string) error {
	if k.applyErr != nil {
		e := k.applyErr
		k.applyErr = nil
		return e
	}
	k.applied = bytes.Clone(b)
	return nil
}
func (k *fakeKernel) Healthy(string) bool                      { return k.healthy }
func (k *fakeKernel) Proxies(string) (map[string]Proxy, error) { return k.proxies, nil }
func (k *fakeKernel) Select(group, name, _ string) error {
	if k.selected == nil {
		k.selected = map[string]string{}
	}
	k.selected[group] = name
	return nil
}

type fakeService struct {
	running, enabled bool
	actions          []string
	fail             string
}

func (s *fakeService) Status() ServiceStatus {
	return ServiceStatus{Running: s.running, Autostart: s.enabled, Available: true}
}
func (s *fakeService) Action(a string) error {
	s.actions = append(s.actions, a)
	if a == s.fail {
		return errors.New("service failed")
	}
	switch a {
	case "start", "restart":
		s.running = true
	case "stop":
		s.running = false
	case "enable":
		s.enabled = true
	case "disable":
		s.enabled = false
	}
	return nil
}
func newTestApp(t *testing.T) (*App, *fakeKernel, *fakeService) {
	t.Helper()
	store, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	k := &fakeKernel{healthy: true, proxies: map[string]Proxy{"PROXY": {Name: "PROXY", Type: "Selector", All: []string{"local-direct", "backup-direct"}, Now: "local-direct"}}}
	s := &fakeService{}
	return &App{Store: store, Kernel: k, Service: s, ControllerPort: 9090}, k, s
}
func withSub(t *testing.T, a *App) State {
	t.Helper()
	st, e := a.Store.Load()
	if e != nil {
		t.Fatal(e)
	}
	st.Subscriptions = []Subscription{{ID: "s1", Name: "one", Content: nodesYAML, Kind: "nodes"}}
	st.Active = "s1"
	if e = a.Store.Save(st); e != nil {
		t.Fatal(e)
	}
	st, _ = a.Store.Load()
	return st
}
func TestConfigurationOwnership(t *testing.T) {
	a, _, _ := newTestApp(t)
	st := withSub(t, a)
	st.Subscriptions[0].Content = fullYAML
	st.Settings.AllowLAN = true
	st.Settings.ProxyUser = "user"
	st.Settings.ProxyPassword = "pass12345"
	b, e := renderConfig(st, 9090)
	if e != nil {
		t.Fatal(e)
	}
	m := map[string]any{}
	yaml.Unmarshal(b, &m)
	if m["port"] != 7890 || m["external-controller"] != "127.0.0.1:9090" || m["secret"] != st.Secret {
		t.Fatal("subscription overwrote managed values")
	}
	if _, ok := m["listeners"]; ok {
		t.Fatal("unmanaged listener")
	}
	if m["tun"].(map[string]any)["enable"] != false {
		t.Fatal("unmanaged TUN")
	}
	if m["rules"].([]any)[0] != "MATCH,Choice" {
		t.Fatal("full routing lost")
	}
	auth := m["authentication"].([]any)
	if auth[0] != "user:pass12345" {
		t.Fatal("authentication missing")
	}
}
func TestNodeTemplateAndInvalidSubscriptions(t *testing.T) {
	a, _, _ := newTestApp(t)
	st := withSub(t, a)
	b, e := renderConfig(st, 9090)
	if e != nil || !bytes.Contains(b, []byte("MATCH,PROXY")) {
		t.Fatal("node template missing", e)
	}
	for _, raw := range []string{"dm1lc3M6Ly8=", "- invalid", nodesYAML + "---\nhello: world", "proxies: &a [*a]", "proxies: [{name: x, type: direct}, {name: x, type: direct}]", "proxies: [{name: x, type: direct}]\nrules: [MATCH,DIRECT]"} {
		if _, _, e = parseSubscription(raw); e == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
}
func TestProviderPaths(t *testing.T) {
	a, _, _ := newTestApp(t)
	st := withSub(t, a)
	st.Subscriptions[0].Content = "proxy-providers:\n  source: {type: http, url: https://example.com/s.yaml, path: /etc/passwd}\n"
	b, e := renderConfig(st, 9090)
	if e != nil || bytes.Contains(b, []byte("/etc/passwd")) {
		t.Fatal(e, "unsafe provider path")
	}
	st.Subscriptions[0].Content = "proxy-providers:\n  source: {type: file, path: /etc/passwd}\n"
	if _, e = renderConfig(st, 9090); e == nil {
		t.Fatal("file provider accepted")
	}
}
func TestPersistence(t *testing.T) {
	a, _, _ := newTestApp(t)
	st := withSub(t, a)
	st.Settings.Mode = "global"
	st.Selections[st.Active] = map[string]string{"PROXY": "backup-direct"}
	if e := a.Store.Save(st); e != nil {
		t.Fatal(e)
	}
	dir := a.Store.dir
	a.Store.Close()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	got, e := s.Load()
	if e != nil || got.Settings.Mode != "global" || got.Subscriptions[0].Content != nodesYAML || got.Selections["s1"]["PROXY"] != "backup-direct" {
		t.Fatal("state not persisted", e)
	}
}
func TestApplyFailureRestoresConfig(t *testing.T) {
	a, k, s := newTestApp(t)
	old := withSub(t, a)
	original, _ := renderConfig(old, 9090)
	atomicWrite(runtimePath(a.Store.dir), original, 0600)
	s.running = true
	next := old
	next.Settings.Mode = "global"
	k.applyErr = errors.New("injected apply failure")
	if e := a.commitConfig(old, next); e == nil {
		t.Fatal("expected failure")
	}
	got, _ := a.Store.Load()
	b, _ := os.ReadFile(runtimePath(a.Store.dir))
	if got.Settings.Mode != "rule" || !bytes.Equal(b, original) || !bytes.Equal(k.applied, original) {
		t.Fatal("failed to restore")
	}
	if _, e := os.Stat(a.journalPath()); !os.IsNotExist(e) {
		t.Fatal("journal not cleared")
	}
}
func TestValidationFailurePreservesState(t *testing.T) {
	a, k, _ := newTestApp(t)
	old := withSub(t, a)
	k.validErr = errors.New("bad")
	next := old
	next.Settings.Mode = "global"
	if e := a.commitConfig(old, next); e == nil {
		t.Fatal("expected validation failure")
	}
	got, _ := a.Store.Load()
	if got.Revision != old.Revision || got.Settings.Mode != "rule" {
		t.Fatal("invalid config committed")
	}
}
func TestAutostartFailureRestoresState(t *testing.T) {
	a, _, s := newTestApp(t)
	old := withSub(t, a)
	next := old
	next.Settings.Autostart = true
	s.fail = "enable"
	if e := a.commitConfig(old, next); e == nil {
		t.Fatal("expected service failure")
	}
	got, _ := a.Store.Load()
	if got.Settings.Autostart {
		t.Fatal("failed autostart persisted")
	}
}
func TestFailedKernelRestoreStillRestoresDurableSettings(t *testing.T) {
	a, _, s := newTestApp(t)
	old := withSub(t, a)
	b, _ := renderConfig(old, 9090)
	changed := old
	changed.Settings.Mode = "global"
	a.Store.Save(changed)
	s.fail = "start"
	rec := recovery{State: toDisk(old), Config: b, Running: true}
	if e := a.restore(rec); e == nil {
		t.Fatal("expected failed kernel restore")
	}
	got, e := a.Store.Load()
	if e != nil || got.Settings.Mode != "rule" || got.Revision <= old.Revision {
		t.Fatal("management retained rejected settings after core failure", e)
	}
}
func TestCrashRecovery(t *testing.T) {
	a, _, s := newTestApp(t)
	old := withSub(t, a)
	b, _ := renderConfig(old, 9090)
	rec := recovery{State: toDisk(old), Config: b, Running: true}
	raw, _ := json.Marshal(rec)
	atomicWrite(a.journalPath(), raw, 0600)
	atomicWrite(runtimePath(a.Store.dir), []byte("broken"), 0600)
	if e := a.Recover(); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(runtimePath(a.Store.dir))
	if !bytes.Equal(got, b) || !s.running {
		t.Fatal("crash recovery failed")
	}
}
func TestTUNTimeoutAndRebootRecovery(t *testing.T) {
	for _, recoverOnRestart := range []bool{false, true} {
		t.Run(fmtBool(recoverOnRestart), func(t *testing.T) {
			a, _, s := newTestApp(t)
			st := withSub(t, a)
			previous := st.Settings
			st.Settings.TUN = true
			deadline := time.Now().Add(-time.Second)
			if recoverOnRestart {
				deadline = time.Now().Add(time.Minute)
			}
			st.Pending = &PendingTUN{Deadline: deadline, Previous: previous, WasRunning: true}
			a.Store.Save(st)
			s.running = true
			var e error
			if recoverOnRestart {
				e = a.Recover()
			} else {
				e = a.RollbackDue(false)
			}
			if e != nil {
				t.Fatal(e)
			}
			got, _ := a.Store.Load()
			if got.Settings.TUN || got.Pending != nil || !s.running || got.Notice == "" {
				t.Fatal("TUN not recovered")
			}
		})
	}
}
func fmtBool(b bool) string {
	if b {
		return "manager-restart"
	}
	return "timeout"
}
func TestMissingSelectedNode(t *testing.T) {
	a, k, _ := newTestApp(t)
	st := withSub(t, a)
	st.Selections[st.Active] = map[string]string{"PROXY": "removed"}
	if e := a.restoreSelections(&st); e != nil {
		t.Fatal(e)
	}
	if len(st.Selections[st.Active]) != 0 || st.Notice == "" || len(k.selected) != 0 {
		t.Fatal("stale selection retained")
	}
}
func TestKernelRecoveryFailureDoesNotBlockManagement(t *testing.T) {
	a, _, s := newTestApp(t)
	st := withSub(t, a)
	st.Pending = &PendingTUN{Deadline: time.Now().Add(-time.Second), Previous: st.Settings, WasRunning: true}
	st.Settings.TUN = true
	if e := a.Store.Save(st); e != nil {
		t.Fatal(e)
	}
	s.fail = "stop"
	if e := a.Prepare(); e != nil {
		t.Fatal("kernel failure blocked management", e)
	}
	st, e := a.Store.Load()
	if e != nil || st.Notice == "" || st.Pending == nil {
		t.Fatal("missing recovery notice or retry marker", e)
	}
	token, e := a.Store.CreateSession()
	if e != nil {
		t.Fatal(e)
	}
	h := &HTTP{App: a, Assets: http.NotFoundHandler()}
	req := httptest.NewRequest("GET", "/api/state", nil)
	req.AddCookie(&http.Cookie{Name: "meshport_session", Value: token})
	response := httptest.NewRecorder()
	h.Handler().ServeHTTP(response, req)
	if response.Code != 200 {
		t.Fatal("management unavailable during core recovery failure")
	}
}
func TestPortValidation(t *testing.T) {
	a, _, _ := newTestApp(t)
	st, _ := a.Store.Load()
	st.Settings.HTTPPort = st.Settings.WebPort
	if validateSettings(st.Settings, 9090) == nil {
		t.Fatal("duplicate accepted")
	}
	st.Settings = defaultState().Settings
	st.Settings.AllowLAN = true
	if validateSettings(st.Settings, 9090) == nil {
		t.Fatal("open unauthenticated proxy")
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	st.Settings = defaultState().Settings
	st.Settings.HTTPPort = l.Addr().(*net.TCPAddr).Port
	if checkPorts(st.Settings, defaultState().Settings, false) == nil {
		t.Fatal("conflict accepted")
	}
}

type apiHarness struct {
	t      *testing.T
	app    *App
	server *httptest.Server
	http   *HTTP
	cookie *http.Cookie
}

func harness(t *testing.T) *apiHarness {
	a, _, _ := newTestApp(t)
	h := &HTTP{App: a, Host: "127.0.0.1", Assets: http.NotFoundHandler()}
	server := httptest.NewServer(h.Handler())
	t.Cleanup(server.Close)
	t.Cleanup(func() { h.Shutdown(t.Context()) })
	return &apiHarness{t: t, app: a, server: server, http: h}
}
func (h *apiHarness) request(method, path string, body any, extra map[string]string) (int, []byte) {
	h.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, h.server.URL+path, bytes.NewReader(b))
	req.Header.Set("X-Meshport-Request", "1")
	if h.cookie != nil {
		req.AddCookie(h.cookie)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		h.t.Fatal(e)
	}
	defer res.Body.Close()
	for _, c := range res.Cookies() {
		h.cookie = c
	}
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}
func (h *apiHarness) initialize() {
	token, _ := os.ReadFile(filepath.Join(h.app.Store.dir, "init-token"))
	code, b := h.request("POST", "/api/setup", map[string]string{"token": strings.TrimSpace(string(token)), "password": "long-test-password"}, nil)
	if code != 200 {
		h.t.Fatal(code, string(b))
	}
}
func TestAuthBoundaries(t *testing.T) {
	h := harness(t)
	code, _ := h.request("GET", "/api/state", nil, nil)
	if code != 401 {
		t.Fatal(code)
	}
	code, _ = h.request("POST", "/api/setup", map[string]string{"password": "long-test-password", "token": "bad"}, nil)
	if code != 403 {
		t.Fatal(code)
	}
	token, _ := os.ReadFile(filepath.Join(h.app.Store.dir, "init-token"))
	h.initialize()
	code, raw := h.request("GET", "/api/state", nil, nil)
	if code != 200 || bytes.Contains(raw, []byte("password_hash")) || bytes.Contains(raw, []byte("secret")) {
		t.Fatal("state leaks credentials", code)
	}
	if h.cookie == nil || !h.cookie.HttpOnly || h.cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("weak session cookie")
	}
	code, _ = h.request("POST", "/api/setup", map[string]string{"password": "long-test-password", "token": strings.TrimSpace(string(token))}, nil)
	if code != 403 {
		t.Fatal("reused init token")
	}
	code, _ = h.request("POST", "/api/logout", nil, map[string]string{"Origin": "https://evil.example"})
	if code != 403 {
		t.Fatal("CSRF accepted")
	}
	code, _ = h.request("POST", "/api/logout", nil, map[string]string{"X-Meshport-Request": ""})
	if code != 403 {
		t.Fatal("missing CSRF marker accepted")
	}
	h.request("POST", "/api/logout", nil, nil)
	code, _ = h.request("GET", "/api/state", nil, nil)
	if code != 401 {
		t.Fatal("logout failed")
	}
}
func TestSubscriptionsAndStoppedManagement(t *testing.T) {
	h := harness(t)
	h.initialize()
	code, b := h.request("POST", "/api/subscriptions", map[string]string{"name": "first", "content": nodesYAML}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	st, _ := h.app.Store.Load()
	id := st.Subscriptions[0].ID
	code, b = h.request("POST", "/api/subscriptions/"+id+"/activate", nil, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, _ = h.request("GET", "/api/nodes", nil, nil)
	if code != 409 {
		t.Fatal("offline node operation")
	}
	code, _ = h.request("POST", "/api/service", map[string]string{"action": "start"}, nil)
	if code != 200 {
		t.Fatal(code)
	}
	code, _ = h.request("DELETE", "/api/subscriptions/"+id, nil, nil)
	if code != 400 {
		t.Fatal("deleted active running subscription")
	}
	h.request("POST", "/api/service", map[string]string{"action": "stop"}, nil)
	code, _ = h.request("GET", "/api/state", nil, nil)
	if code != 200 {
		t.Fatal("manager stopped with kernel")
	}
	code, b = h.request("DELETE", "/api/subscriptions/"+id, nil, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	if _, e := os.Stat(runtimePath(h.app.Store.dir)); !os.IsNotExist(e) {
		t.Fatal("deleting the active subscription left a bootable stale runtime config")
	}
}
func TestSettingsConflictAndWebPortFailure(t *testing.T) {
	h := harness(t)
	h.initialize()
	st, _ := h.app.Store.Load()
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	next := st.Settings
	next.WebPort = l.Addr().(*net.TCPAddr).Port
	code, _ := h.request("PUT", "/api/settings", map[string]any{"settings": next, "revision": st.Revision - 1}, nil)
	if code != 409 {
		t.Fatal("stale revision accepted")
	}
	code, b := h.request("PUT", "/api/settings", map[string]any{"settings": next, "revision": st.Revision}, nil)
	if code != 400 {
		t.Fatal(code, string(b))
	}
	got, _ := h.app.Store.Load()
	if !reflect.DeepEqual(got.Settings, st.Settings) {
		t.Fatal("failed migration saved")
	}
}
func TestPasswordChangeRevokesSessions(t *testing.T) {
	h := harness(t)
	h.initialize()
	cookie := h.cookie
	code, _ := h.request("PUT", "/api/password", map[string]string{"current": "long-test-password", "password": "another-long-test-password"}, nil)
	if code != 200 {
		t.Fatal(code)
	}
	h.cookie = cookie
	code, _ = h.request("GET", "/api/state", nil, nil)
	if code != 401 {
		t.Fatal("old session survived")
	}
	code, _ = h.request("POST", "/api/login", map[string]string{"password": "another-long-test-password"}, nil)
	if code != 200 {
		t.Fatal("new password failed")
	}
}
func TestSecretURLNotReturned(t *testing.T) {
	h := harness(t)
	h.initialize()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, nodesYAML) }))
	defer source.Close()
	code, b := h.request("POST", "/api/subscriptions", map[string]string{"name": "secret subscription", "url": source.URL + "/?token=super-secret-token"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	_, b = h.request("GET", "/api/state", nil, nil)
	if bytes.Contains(b, []byte("super-secret-token")) {
		t.Fatal("URL credential exposed")
	}
}
