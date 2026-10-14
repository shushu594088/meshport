package meshport

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Connection struct {
	ID       string `json:"id"`
	Metadata struct {
		Network         string `json:"network"`
		Type            string `json:"type"`
		Host            string `json:"host"`
		SourceIP        string `json:"sourceIP"`
		DestinationIP   string `json:"destinationIP"`
		DestinationPort string `json:"destinationPort"`
		Process         string `json:"process"`
	} `json:"metadata"`
	Upload   int64    `json:"upload"`
	Download int64    `json:"download"`
	Start    string   `json:"start"`
	Chains   []string `json:"chains"`
	Rule     string   `json:"rule"`
}
type Connections struct {
	UploadTotal   int64        `json:"uploadTotal"`
	DownloadTotal int64        `json:"downloadTotal"`
	Memory        int64        `json:"memory"`
	Connections   []Connection `json:"connections"`
}
type LogEntry struct {
	Type    string `json:"type"`
	Payload string `json:"payload"`
}

func (m *Mihomo) CoreInfo(ctx context.Context, secret string) (map[string]any, error) {
	v := map[string]any{}
	e := m.requestContext(ctx, "GET", "/version", secret, nil, &v)
	return v, e
}
func (m *Mihomo) Connections(ctx context.Context, secret string) (Connections, error) {
	v := Connections{Connections: []Connection{}}
	e := m.requestContext(ctx, "GET", "/connections", secret, nil, &v)
	return v, e
}
func (m *Mihomo) CoreAction(ctx context.Context, action, id, secret string) error {
	method, path := "POST", "/cache/dns/flush"
	switch action {
	case "dns-flush":
	case "geodata":
		path = "/upgrade/geo"
	case "connections-close":
		method = "DELETE"
		path = "/connections"
		if id != "" {
			path += "/" + url.PathEscape(id)
		}
	default:
		return errors.New("不支持该内核操作")
	}
	clone := *m
	client := *m.Client
	client.Timeout = 55 * time.Second
	clone.Client = &client
	return clone.requestContext(ctx, method, path, secret, map[string]string{}, nil)
}
func (m *Mihomo) Logs(ctx context.Context, secret string) ([]LogEntry, error) {
	request, e := http.NewRequestWithContext(ctx, "GET", m.Address+"/logs?level=debug", nil)
	if e != nil {
		return nil, e
	}
	request.Header.Set("Authorization", "Bearer "+secret)
	response, e := m.Client.Do(request)
	if e != nil {
		if ctx.Err() != nil {
			return []LogEntry{}, nil
		}
		return nil, errors.New("暂时无法读取内核日志")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("内核日志接口不可用")
	}
	logs := []LogEntry{}
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 256<<10))
	scanner.Buffer(make([]byte, 4096), 64<<10)
	for scanner.Scan() {
		var entry LogEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil {
			logs = append(logs, entry)
		}
		if len(logs) >= 100 {
			break
		}
	}
	return logs, nil
}

var sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|uuid|secret|token|private.?key|client.?key|api.?key|access.?key|authorization|authentication)`)
var textURL = regexp.MustCompile(`(?:https?|ss|vmess|trojan)://[^\s<>"']+`)

func safeURL(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return "[地址已隐藏]"
	}
	if u.Scheme == "ss" || u.Scheme == "vmess" || u.Scheme == "trojan" {
		return u.Scheme + "://[已隐藏]"
	}
	if u.User != nil {
		u.User = url.User("REDACTED")
	}
	if u.RawQuery != "" {
		u.RawQuery = "REDACTED"
	}
	u.Fragment = ""
	if u.Path != "" && u.Path != "/" {
		u.Path = "/[地址已隐藏]"
		u.RawPath = ""
	}
	return u.String()
}
func redactConfig(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			if sensitiveKey.MatchString(key) {
				out[key] = "[已隐藏]"
			} else if strings.EqualFold(key, "url") {
				if raw, ok := item.(string); ok {
					out[key] = safeURL(raw)
				} else {
					out[key] = redactConfig(item)
				}
			} else {
				out[key] = redactConfig(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactConfig(item)
		}
		return out
	case string:
		return textURL.ReplaceAllStringFunc(v, safeURL)
	default:
		return value
	}
}
func safeLog(st State, text string) string {
	secrets := []string{st.Secret, st.Settings.ProxyPassword}
	var collect func(any)
	collect = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			for key, item := range v {
				if sensitiveKey.MatchString(key) {
					if raw, ok := item.(string); ok {
						secrets = append(secrets, raw)
					}
				}
				collect(item)
			}
		case []any:
			for _, item := range v {
				collect(item)
			}
		}
	}
	for _, sub := range st.Subscriptions {
		secrets = append(secrets, sub.URL)
		var values map[string]any
		if yaml.Unmarshal([]byte(sub.Content), &values) == nil {
			collect(values)
		}
	}
	slices.SortFunc(secrets, func(a, b string) int { return len(b) - len(a) })
	for _, secret := range secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, "[已隐藏]")
		}
	}
	return textURL.ReplaceAllStringFunc(text, safeURL)
}

func (h *HTTP) coreRead(w http.ResponseWriter, r *http.Request, st State) {
	m, ok := h.App.Kernel.(*Mihomo)
	if !ok {
		problem(w, 503, errors.New("当前驱动不支持内核监控"))
		return
	}
	if !h.App.Service.Status().Running {
		problem(w, 409, errors.New("内核已停止，请先启动代理"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	switch r.URL.Path {
	case "/api/core":
		v, e := m.CoreInfo(ctx, st.Secret)
		if e != nil {
			problem(w, 503, e)
			return
		}
		jsonResponse(w, 200, v)
	case "/api/connections":
		v, e := m.Connections(ctx, st.Secret)
		if e != nil {
			problem(w, 503, e)
			return
		}
		jsonResponse(w, 200, v)
	case "/api/logs":
		short, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		logs, e := m.Logs(short, st.Secret)
		if e != nil {
			problem(w, 503, e)
			return
		}
		for i := range logs {
			logs[i].Payload = safeLog(st, logs[i].Payload)
		}
		jsonResponse(w, 200, logs)
	}
}
