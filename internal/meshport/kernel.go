package meshport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type Proxy struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Now     string   `json:"now,omitempty"`
	All     []string `json:"all,omitempty"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history,omitempty"`
}
type Kernel interface {
	Validate([]byte) error
	Apply([]byte, string) error
	Proxies(string) (map[string]Proxy, error)
	Select(string, string, string) error
	Healthy(string) bool
}
type Mihomo struct {
	Binary, Dir, Address string
	Client               *http.Client
}

func NewMihomo(binary, dir string, port int) *Mihomo {
	return &Mihomo{Binary: binary, Dir: dir, Address: fmt.Sprintf("http://127.0.0.1:%d", port), Client: &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil}}}
}
func (m *Mihomo) Validate(b []byte) error {
	f, e := os.CreateTemp(m.Dir, ".validate-*.yaml")
	if e != nil {
		return e
	}
	path := f.Name()
	defer os.Remove(path)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Do not return raw validator output: it can echo subscription credentials.
	cmd := exec.CommandContext(ctx, m.Binary, "-t", "-d", m.Dir, "-f", path)
	if e = cmd.Run(); e != nil {
		return errors.New("Mihomo 配置验证失败，请检查节点类型、策略组、规则或集合资源")
	}
	return nil
}
func (m *Mihomo) request(method, path, secret string, body any, out any) error {
	return m.requestContext(context.Background(), method, path, secret, body, out)
}
func (m *Mihomo) requestContext(ctx context.Context, method, path, secret string, body any, out any) error {
	var data []byte
	var e error
	if body != nil {
		data, e = json.Marshal(body)
		if e != nil {
			return e
		}
	}
	req, e := http.NewRequestWithContext(ctx, method, m.Address+path, bytes.NewReader(data))
	if e != nil {
		return e
	}
	if path == "/version" {
		ctx, cancel := context.WithTimeout(req.Context(), 2*time.Second)
		defer cancel()
		req = req.WithContext(ctx)
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	res, e := m.Client.Do(req)
	if e != nil {
		return errors.New("内核控制接口不可用，请检查内核运行状态")
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
		return fmt.Errorf("内核操作失败（HTTP %d）", res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 16<<20)).Decode(out)
	}
	return nil
}
func (m *Mihomo) Apply(b []byte, secret string) error {
	return m.request("PUT", "/configs?force=true", secret, map[string]string{"payload": string(b)}, nil)
}
func (m *Mihomo) Healthy(secret string) bool {
	return m.request("GET", "/version", secret, nil, &map[string]any{}) == nil
}
func (m *Mihomo) Listening(s Settings) bool {
	for _, port := range []int{s.HTTPPort, s.SOCKSPort} {
		c, e := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if e != nil {
			return false
		}
		c.Close()
	}
	return true
}
func (m *Mihomo) Proxies(secret string) (map[string]Proxy, error) {
	var p struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	e := m.request("GET", "/proxies", secret, nil, &p)
	return p.Proxies, e
}
func (m *Mihomo) Select(group, name, secret string) error {
	return m.request("PUT", "/proxies/"+url.PathEscape(group), secret, map[string]string{"name": name}, nil)
}
func (m *Mihomo) Delay(ctx context.Context, name, secret string) (int, error) {
	var result struct {
		Delay int `json:"delay"`
	}
	query := url.Values{"url": {"https://www.gstatic.com/generate_204"}, "timeout": {"5000"}, "expected": {"204"}}
	e := m.requestContext(ctx, "GET", "/proxies/"+url.PathEscape(name)+"/delay?"+query.Encode(), secret, nil, &result)
	return result.Delay, e
}
func runtimePath(dir string) string { return filepath.Join(dir, "runtime.yaml") }
