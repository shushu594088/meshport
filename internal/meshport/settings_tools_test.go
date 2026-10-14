package meshport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func decodedConfig(t *testing.T, st State) map[string]any {
	t.Helper()
	b, e := renderConfig(st, 9090)
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = yaml.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	return m
}
func TestDNSInheritanceAndPerSubscriptionOverride(t *testing.T) {
	a, _, _ := newTestApp(t)
	st := withSub(t, a)
	st.Subscriptions[0].Content = fullYAML + "dns: {enable: true, nameserver: [223.5.5.5], fallback: [119.29.29.29], enhanced-mode: fake-ip}\nunified-delay: true\n"
	st.Settings.TUN = true
	m := decodedConfig(t, st)
	dns := m["dns"].(map[string]any)
	if dns["nameserver"].([]any)[0] != "223.5.5.5" || dns["enhanced-mode"] != "fake-ip" || m["unified-delay"] != true {
		t.Fatal("TUN replaced subscription DNS or unified delay", dns)
	}
	st.Subscriptions[0].DNS = DNSSettings{Override: true, Nameservers: []string{"https://dns.alidns.com/dns-query"}, Bootstrap: []string{"223.5.5.5"}, ProxyNameservers: []string{"https://doh.pub/dns-query"}}
	st.Subscriptions = append(st.Subscriptions, Subscription{ID: "s2", Name: "two", Kind: "nodes", Content: nodesYAML})
	if e := a.Store.Save(st); e != nil {
		t.Fatal(e)
	}
	st, _ = a.Store.Load()
	dns = decodedConfig(t, st)["dns"].(map[string]any)
	if dns["nameserver"].([]any)[0] != "https://dns.alidns.com/dns-query" {
		t.Fatal("override not persisted")
	}
	if _, ok := dns["fallback"]; ok {
		t.Fatal("overseas fallback retained")
	}
	st.Active = "s2"
	dns = decodedConfig(t, st)["dns"].(map[string]any)
	if dns["nameserver"].([]any)[0] != "system" {
		t.Fatal("override leaked across subscriptions")
	}
	off := false
	st.Settings.UnifiedDelay = &off
	st.Settings.IPv6 = true
	st.Settings.LogLevel = "info"
	m = decodedConfig(t, st)
	if m["unified-delay"] != false || m["ipv6"] != true || m["log-level"] != "info" {
		t.Fatal("core settings ignored")
	}
}
func TestDNSValidationAndAtomicRollback(t *testing.T) {
	for _, v := range []string{"file:///etc/passwd", "https://user:secret@dns.example/query", "https://dns.example/query?token=secret", "https://dns.example/query#PROXY", "1.1.1.1:0", "tls://dns.example:99999"} {
		if validateDNS(DNSSettings{Override: true, Nameservers: []string{v}}) == nil {
			t.Errorf("accepted invalid resolver %q", v)
		}
	}
	if validateDNS(DNSSettings{Override: true, Nameservers: []string{"system"}, Bootstrap: []string{"https://dns.example/query"}}) == nil {
		t.Fatal("bootstrap accepted domain")
	}
	a, k, s := newTestApp(t)
	old := withSub(t, a)
	s.running = true
	next := old
	next.Subscriptions = append([]Subscription(nil), old.Subscriptions...)
	next.Subscriptions[0].DNS = DNSSettings{Override: true, Nameservers: []string{"system"}}
	k.applyErr = errors.New("apply failed")
	if a.commitConfig(old, next) == nil {
		t.Fatal("apply error ignored")
	}
	restored, _ := a.Store.Load()
	if !reflect.DeepEqual(restored.Subscriptions, old.Subscriptions) {
		t.Fatal("failed override changed persisted subscription")
	}
}
func TestEncryptedBackupIntegrityAndRestoreBoundaries(t *testing.T) {
	a, _, _ := newTestApp(t)
	st := withSub(t, a)
	st.Secret = "private-core-secret"
	st.Subscriptions[0].URL = "https://example.com/sub/private-token"
	st.Settings.ProxyPassword = "private-proxy-password"
	password := "independent-backup-password"
	backup, e := encryptBackup(st, password)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(backup)
	if bytes.Contains(raw, []byte(st.Secret)) || bytes.Contains(raw, []byte(st.Subscriptions[0].URL)) || bytes.Contains(raw, []byte(st.Settings.ProxyPassword)) {
		t.Fatal("plaintext leaked")
	}
	restored, e := decryptBackup(backup, password)
	if e != nil || restored.Subscriptions[0].Content != nodesYAML {
		t.Fatal("backup cannot restore", e)
	}
	if _, e = decryptBackup(backup, "incorrect-backup-password"); e == nil {
		t.Fatal("wrong password accepted")
	}
	backup.Data[0] ^= 1
	if _, e = decryptBackup(backup, password); e == nil {
		t.Fatal("tampered backup accepted")
	}
	old := defaultState()
	old.Secret = "current-secret"
	old.PasswordHash = "current-hash"
	old.Settings.WebPort = 8899
	restored.Settings.TUN = true
	if e = validateRestore(&restored, old, 9090); e != nil {
		t.Fatal(e)
	}
	if restored.Secret != old.Secret || restored.PasswordHash != old.PasswordHash || restored.Settings.TUN || restored.Settings.WebPort != 8899 {
		t.Fatal("restore changed management boundaries")
	}
	restored.Subscriptions[0].Content = "invalid"
	if validateRestore(&restored, old, 9090) == nil {
		t.Fatal("invalid backup subscription accepted")
	}
}
func TestAdminRedactionAndCoreActions(t *testing.T) {
	st := defaultState()
	st.Secret = "private-secret"
	st.Subscriptions = []Subscription{{URL: "https://example.com/private-token", Content: "uuid: private-uuid\npassword: private-password\n"}}
	raw := safeLog(st, "private-secret private-uuid private-password https://user:pass@example.com/private-token?key=value vmess://encoded-secret")
	for _, secret := range []string{"private-secret", "private-uuid", "private-password", "private-token", "key=value", "encoded-secret", "user:pass"} {
		if strings.Contains(raw, secret) {
			t.Errorf("log leaked %s", secret)
		}
	}
	v := redactConfig(map[string]any{"authentication": []any{"username:password"}, "proxies": []any{map[string]any{"uuid": "secret-uuid"}}, "url": "https://example.com/sub/secret-path?token=secret-query"})
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "secret-") || strings.Contains(string(b), "username:password") {
		t.Fatal("config leaked credentials")
	}
	calls := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("missing core authorization")
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()
	m := &Mihomo{Address: server.URL, Client: server.Client()}
	for _, action := range []string{"dns-flush", "geodata", "connections-close"} {
		if e := m.CoreAction(context.Background(), action, "", "test-secret"); e != nil {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual(calls, []string{"POST /cache/dns/flush", "POST /upgrade/geo", "DELETE /connections"}) {
		t.Fatal(calls)
	}
	if m.CoreAction(context.Background(), "arbitrary", "", "test-secret") == nil {
		t.Fatal("arbitrary core operation accepted")
	}
}

func TestDNSAPIScopeConflictAndBackupAuthorization(t *testing.T) {
	h := harness(t)
	if code, _ := h.request("POST", "/api/backup", map[string]string{"password": "independent-backup-password"}, nil); code != 401 {
		t.Fatal("unauthenticated backup accepted")
	}
	h.initialize()
	old := withSub(t, h.app)
	dns := DNSSettings{Override: true, Nameservers: []string{"https://dns.alidns.com/dns-query"}, Bootstrap: []string{"223.5.5.5"}}
	body := map[string]any{"settings": old.Settings, "revision": old.Revision, "dns": dns, "subscription_id": "other-subscription"}
	if code, _ := h.request("PUT", "/api/settings", body, nil); code != 400 {
		t.Fatal("wrong DNS subscription accepted")
	}
	body["subscription_id"] = old.Active
	body["revision"] = old.Revision - 1
	if code, _ := h.request("PUT", "/api/settings", body, nil); code != 409 {
		t.Fatal("stale DNS save accepted")
	}
	body["revision"] = old.Revision
	if code, b := h.request("PUT", "/api/settings", body, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	current, _ := h.app.Store.Load()
	if !reflect.DeepEqual(current.Subscriptions[0].DNS, dns) {
		t.Fatal("DNS API did not persist")
	}
	code, b := h.request("POST", "/api/backup", map[string]string{"password": "independent-backup-password"}, nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	var backup Backup
	json.Unmarshal(b, &backup)
	restore := map[string]any{"password": "incorrect-backup-password", "backup": backup, "revision": current.Revision}
	if code, _ = h.request("POST", "/api/backup/restore", restore, nil); code != 400 {
		t.Fatal("incorrect restore password accepted")
	}
	unchanged, _ := h.app.Store.Load()
	if unchanged.Revision != current.Revision {
		t.Fatal("failed restore mutated state")
	}
	restore["password"] = "independent-backup-password"
	if code, b = h.request("POST", "/api/backup/restore", restore, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	restored, _ := h.app.Store.Load()
	if restored.PasswordHash != current.PasswordHash || restored.Secret != current.Secret || restored.Settings.WebPort != current.Settings.WebPort {
		t.Fatal("HTTP restore changed management identity")
	}
}

func TestTUNRollbackRestoresSimultaneousDNSChange(t *testing.T) {
	h := harness(t)
	h.initialize()
	old := withSub(t, h.app)
	h.app.Service.(*fakeService).running = true
	settings := old.Settings
	settings.TUN = true
	dns := DNSSettings{Override: true, Nameservers: []string{"https://dns.alidns.com/dns-query"}, Bootstrap: []string{"223.5.5.5"}}
	if code, b := h.request("PUT", "/api/settings", map[string]any{"settings": settings, "revision": old.Revision, "dns": dns, "subscription_id": old.Active}, nil); code != 200 {
		t.Fatal(code, string(b))
	}
	pending, _ := h.app.Store.Load()
	if pending.Pending == nil || pending.Pending.PreviousDNS == nil {
		t.Fatal("TUN recovery lacks previous DNS")
	}
	if e := h.app.RollbackDue(true); e != nil {
		t.Fatal(e)
	}
	restored, _ := h.app.Store.Load()
	if restored.Settings.TUN || !reflect.DeepEqual(restored.Subscriptions[0].DNS, old.Subscriptions[0].DNS) {
		t.Fatal("TUN rollback retained new DNS")
	}
}
