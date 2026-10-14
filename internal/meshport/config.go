package meshport

import (
	"bytes"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
)

const maxSubscription = 8 << 20

func configHasTUN(b []byte) bool {
	var m map[string]any
	if yaml.Unmarshal(b, &m) != nil {
		return false
	}
	tun, _ := m["tun"].(map[string]any)
	enabled, _ := tun["enable"].(bool)
	return enabled
}
func configHasDNS(b []byte) bool {
	var m map[string]any
	if yaml.Unmarshal(b, &m) != nil {
		return false
	}
	dns, _ := m["dns"].(map[string]any)
	enabled, _ := dns["enable"].(bool)
	return enabled
}

func parseSubscription(raw string) (map[string]any, string, error) {
	if len(raw) > maxSubscription {
		return nil, "", errors.New("订阅超过 8 MiB 限制")
	}
	dec := yaml.NewDecoder(strings.NewReader(raw))
	var root yaml.Node
	if e := dec.Decode(&root); e != nil {
		return nil, "", errors.New("订阅不是有效的 Clash / Mihomo YAML")
	}
	// Bound aliases and nesting before decoding to avoid expansion and recursion attacks.
	count := 0
	var check func(*yaml.Node, int) error
	check = func(n *yaml.Node, d int) error {
		count++
		if count > 100000 || d > 40 {
			return errors.New("订阅结构过于复杂")
		}
		if n.Kind == yaml.AliasNode {
			return errors.New("订阅不支持 YAML 别名，请展开后重试")
		}
		for _, c := range n.Content {
			if e := check(c, d+1); e != nil {
				return e
			}
		}
		return nil
	}
	if e := check(&root, 0); e != nil {
		return nil, "", e
	}
	var m map[string]any
	if e := root.Decode(&m); e != nil || m == nil {
		return nil, "", errors.New("订阅必须是 YAML 对象")
	}
	var extra any
	if e := dec.Decode(&extra); e != io.EOF {
		return nil, "", errors.New("订阅只能包含一个 YAML 文档")
	}
	proxies, _ := m["proxies"].([]any)
	providers, _ := m["proxy-providers"].(map[string]any)
	if len(proxies) == 0 && len(providers) == 0 {
		return nil, "", errors.New("订阅没有可用节点或节点集合")
	}
	names := map[string]bool{}
	for _, p := range proxies {
		node, ok := p.(map[string]any)
		if !ok {
			return nil, "", errors.New("节点格式无效")
		}
		name, _ := node["name"].(string)
		typ, _ := node["type"].(string)
		if name == "" || typ == "" || names[name] || name == "DIRECT" || name == "REJECT" {
			return nil, "", errors.New("节点名称、类型缺失或名称重复")
		}
		names[name] = true
	}
	_, hasGroups := m["proxy-groups"]
	_, hasRules := m["rules"]
	kind := "nodes"
	if hasGroups || hasRules {
		kind = "full"
		if !hasGroups || !hasRules {
			return nil, "", errors.New("完整配置必须同时提供策略组和规则")
		}
	}
	return m, kind, nil
}
func validateSettings(s Settings, controllerPort int) error {
	seen := map[int]bool{controllerPort: true, 1053: true}
	for _, port := range []int{s.WebPort, s.HTTPPort, s.SOCKSPort} {
		if port < 1024 || port > 65535 || seen[port] {
			return errors.New("端口必须在 1024–65535 范围内，且不能重复或占用内核控制 / DNS 端口")
		}
		seen[port] = true
	}
	if s.Mode != "rule" && s.Mode != "global" && s.Mode != "direct" {
		return errors.New("无效的代理模式")
	}
	if s.LogLevel != "" && s.LogLevel != "silent" && s.LogLevel != "error" && s.LogLevel != "warning" && s.LogLevel != "info" && s.LogLevel != "debug" {
		return errors.New("无效日志等级")
	}
	if s.AllowLAN && (strings.TrimSpace(s.ProxyUser) == "" || len(s.ProxyPassword) < 8) {
		return errors.New("外部访问需要独立的代理用户名和至少 8 位密码")
	}
	if strings.ContainsAny(s.ProxyUser, ":\r\n") || strings.ContainsAny(s.ProxyPassword, "\r\n") {
		return errors.New("代理凭证包含无效字符")
	}
	if len(s.Bypass) > 100 {
		return errors.New("最多设置 100 个绕行网段")
	}
	for _, cidr := range s.Bypass {
		if _, _, e := net.ParseCIDR(cidr); e != nil {
			return fmt.Errorf("无效绕行网段：%s", cidr)
		}
	}
	return nil
}
func renderConfig(st State, controllerPort int) ([]byte, error) {
	sub := st.activeSub()
	if sub == nil {
		return nil, errors.New("请先启用有效订阅")
	}
	source, kind, e := parseSubscription(sub.Content)
	if e != nil {
		return nil, e
	}
	// Preserve routing and outbound behavior; all ingress/control/network takeover is tool-owned.
	m := map[string]any{}
	for _, key := range strings.Fields("proxies proxy-groups proxy-providers rule-providers rules sub-rules dns hosts sniffer geodata-mode geodata-loader geo-auto-update geo-update-interval geox-url global-client-fingerprint tcp-concurrent unified-delay keep-alive-idle keep-alive-interval find-process-mode") {
		if v, ok := source[key]; ok {
			m[key] = v
		}
	}
	for _, key := range []string{"proxy-providers", "rule-providers"} {
		providers, _ := m[key].(map[string]any)
		for name, v := range providers {
			p, ok := v.(map[string]any)
			if !ok {
				return nil, errors.New("集合配置无效")
			}
			typ, _ := p["type"].(string)
			switch typ {
			case "http":
				u, _ := p["url"].(string)
				parsed, err := url.Parse(u)
				if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
					return nil, errors.New("集合需要有效的 HTTP(S) URL")
				}
				p["path"] = "./providers/" + key + "-" + digest(name) + ".yaml"
			case "inline":
				delete(p, "path")
			default:
				return nil, errors.New("仅支持 HTTP 或 inline 集合，不能读取任意本地文件")
			}
		}
	}
	if kind == "nodes" {
		members := []string{}
		if p, ok := m["proxies"].([]any); ok {
			for _, v := range p {
				members = append(members, v.(map[string]any)["name"].(string))
			}
		}
		group := map[string]any{"name": "PROXY", "type": "select"}
		if len(members) > 0 {
			group["proxies"] = members
		}
		providers, _ := m["proxy-providers"].(map[string]any)
		use := []string{}
		for name := range providers {
			use = append(use, name)
		}
		if len(use) > 0 {
			group["use"] = use
		}
		m["proxy-groups"] = []any{group}
		m["rules"] = []string{"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve", "IP-CIDR,172.16.0.0/12,DIRECT,no-resolve", "IP-CIDR,192.168.0.0/16,DIRECT,no-resolve", "IP-CIDR6,::1/128,DIRECT,no-resolve", "IP-CIDR6,fc00::/7,DIRECT,no-resolve", "MATCH,PROXY"}
	}
	s := st.Settings
	m["port"] = s.HTTPPort
	m["socks-port"] = s.SOCKSPort
	m["mixed-port"] = 0
	m["allow-lan"] = s.AllowLAN
	m["bind-address"] = "127.0.0.1"
	m["authentication"] = []string{}
	if s.AllowLAN {
		m["bind-address"] = "*"
		m["authentication"] = []string{s.ProxyUser + ":" + s.ProxyPassword}
	}
	m["external-controller"] = fmt.Sprintf("127.0.0.1:%d", controllerPort)
	m["secret"] = st.Secret
	m["mode"] = s.Mode
	m["log-level"] = "silent"
	if s.LogLevel != "" {
		m["log-level"] = s.LogLevel
	}
	m["ipv6"] = s.IPv6
	if s.UnifiedDelay != nil {
		m["unified-delay"] = *s.UnifiedDelay
	}
	m["profile"] = map[string]any{"store-selected": false, "store-fake-ip": false}
	dns, _ := m["dns"].(map[string]any)
	if dns == nil {
		dns = map[string]any{}
	}
	if sub.DNS.Override {
		if e := validateDNS(sub.DNS); e != nil {
			return nil, e
		}
		dns = map[string]any{"enable": true, "enhanced-mode": "redir-host", "nameserver": sub.DNS.Nameservers, "default-nameserver": sub.DNS.Bootstrap, "proxy-server-nameserver": sub.DNS.ProxyNameservers}
		if len(sub.DNS.Bootstrap) == 0 {
			delete(dns, "default-nameserver")
		}
		if len(sub.DNS.ProxyNameservers) == 0 {
			delete(dns, "proxy-server-nameserver")
		}
	}
	if s.TUN {
		dns["enable"] = true
		if _, ok := dns["enhanced-mode"]; !ok {
			dns["enhanced-mode"] = "redir-host"
		}
		if _, ok := dns["nameserver"]; !ok {
			dns["nameserver"] = []string{"system"}
		}
	}
	if len(dns) > 0 {
		dns["listen"] = "127.0.0.1:1053"
		dns["ipv6"] = s.IPv6
		m["dns"] = dns
	}
	bypass := []string{"127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"}
	bypass = append(bypass, s.Bypass...)
	m["tun"] = map[string]any{"enable": s.TUN, "device": "meshport0", "stack": "gvisor", "auto-route": true, "auto-redirect": false, "auto-detect-interface": true, "strict-route": false, "dns-hijack": []string{"any:53", "tcp://any:53"}, "route-exclude-address": bypass, "exclude-uid": []int{0, os.Getuid()}, "iproute2-table-index": 20220, "iproute2-rule-index": 9200}
	b, e := yaml.Marshal(m)
	if e != nil {
		return nil, e
	}
	return bytes.TrimSpace(b), nil
}
