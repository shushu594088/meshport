package meshport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type delayKernel struct {
	*fakeKernel
	change   func()
	delayErr error
}

func (k *delayKernel) Delay(ctx context.Context, name, secret string) (int, error) {
	if k.change != nil {
		k.change()
	}
	return 37, k.delayErr
}

func TestDiagnosticsAuthValidationResultsAndStaleConfig(t *testing.T) {
	h := harness(t)
	code, _ := h.request("POST", "/api/tests/delay", map[string]string{"name": "node"}, nil)
	if code != 401 {
		t.Fatal("unauthenticated diagnostics accepted", code)
	}
	h.initialize()
	code, _ = h.request("POST", "/api/tests/delay", map[string]string{"name": "node"}, nil)
	if code != 409 {
		t.Fatal("stopped kernel accepted", code)
	}
	h.app.Service.(*fakeService).running = true
	k := &delayKernel{fakeKernel: h.app.Kernel.(*fakeKernel)}
	k.proxies["香港 / #? %"] = Proxy{Name: "香港 / #? %", Type: "Shadowsocks"}
	h.app.Kernel = k
	code, _ = h.request("POST", "/api/tests/site", map[string]string{"id": "https://127.0.0.1/private"}, nil)
	if code != 400 {
		t.Fatal("arbitrary test destination accepted", code)
	}
	code, _ = h.request("POST", "/api/tests/delay", map[string]string{"name": "missing"}, nil)
	if code != 400 {
		t.Fatal("missing node accepted", code)
	}
	code, raw := h.request("POST", "/api/tests/delay", map[string]string{"name": "香港 / #? %"}, nil)
	var result DiagnosticResult
	if json.Unmarshal(raw, &result) != nil || code != 200 || result.Delay != 37 || result.Status != "supported" || result.CheckedAt.IsZero() {
		t.Fatal(code, string(raw))
	}
	k.delayErr = errors.New("secret raw transport error")
	code, raw = h.request("POST", "/api/tests/delay", map[string]string{"name": "香港 / #? %"}, nil)
	if code != 200 || !strings.Contains(string(raw), "failed") || strings.Contains(string(raw), "secret") {
		t.Fatal(code, string(raw))
	}
	k.change = func() { st, _ := h.app.Store.Load(); st.Settings.Mode = "global"; _ = h.app.Store.Save(st) }
	code, _ = h.request("POST", "/api/tests/delay", map[string]string{"name": "香港 / #? %"}, nil)
	if code != 409 {
		t.Fatal("stale diagnostic was accepted", code)
	}
	h.http.testsActive = 4
	code, _ = h.request("POST", "/api/tests/delay", map[string]string{"name": "香港 / #? %"}, nil)
	if code != 429 {
		t.Fatal("concurrency limit missing", code)
	}
}

func TestMihomoDelayUsesEscapedNameFixedTargetAndCancellation(t *testing.T) {
	name := "香港 / #? %"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/proxies/"+url.PathEscape(name)+"/delay" {
			t.Errorf("bad path %s", r.URL.EscapedPath())
		}
		if r.URL.Query().Get("url") != "https://www.gstatic.com/generate_204" || r.URL.Query().Get("timeout") != "5000" || r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("incorrect delay request")
		}
		io.WriteString(w, `{"delay":0}`)
	}))
	defer server.Close()
	k := &Mihomo{Address: server.URL, Client: server.Client()}
	delay, err := k.Delay(context.Background(), name, "test-secret")
	if err != nil || delay != 0 {
		t.Fatal(delay, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = k.Delay(ctx, name, "test-secret"); err == nil {
		t.Fatal("cancelled request succeeded")
	}
}

func TestSiteClassificationDoesNotConfuseConnectivityWithUnlock(t *testing.T) {
	cases := []struct {
		id             string
		code           int
		body, expected string
	}{
		{"bilibili-cn", 200, `{"code":0}`, "supported"},
		{"bilibili-tw", 200, `{"code":-10403}`, "unsupported"},
		{"bilibili-cn", 200, `{}`, "failed"},
		{"bilibili-cn", 200, `<html>login</html>`, "failed"},
		{"netflix", 200, `<html>Welcome</html>`, "reachable"},
		{"claude", 403, `<html>Forbidden</html>`, "failed"},
		{"chatgpt", 403, `{"error":"unsupported_country_region_territory"}`, "unsupported"},
		{"youtube", 200, `YouTube Premium is not available in your country`, "unsupported"},
		{"youtube", 200, `"browseId":"SPunlimited","GL":"JP"`, "supported"},
		{"gemini", 200, `Just a moment`, "failed"},
		{"spotify", 429, `retry`, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.id+tc.expected+tc.body[:2], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.code); io.WriteString(w, tc.body) }))
			defer server.Close()
			result := testSite(context.Background(), server.Client(), Site{ID: tc.id, URL: server.URL})
			if result.Status != tc.expected {
				t.Fatalf("%s: %+v", tc.expected, result)
			}
		})
	}
}

func TestSiteProxyAuthenticationAndNoDirectFallback(t *testing.T) {
	client, transport := diagnosticClient(Settings{HTTPPort: 1, AllowLAN: true, ProxyUser: "admin", ProxyPassword: "private"})
	defer transport.CloseIdleConnections()
	proxy, err := transport.Proxy(httptest.NewRequest("GET", "https://example.org", nil))
	if err != nil || proxy.Host != "127.0.0.1:1" || proxy.User.String() != "admin:private" {
		t.Fatal(proxy, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := testSite(ctx, client, Site{ID: "spotify", URL: "https://example.org"})
	if r.Status != "failed" || strings.Contains(r.Detail, "private") {
		t.Fatal(r)
	}
}
