package meshport

import (
	"errors"
	"net"
	"net/url"
	"slices"
	"strings"
)

func restorePendingDNS(st *State, pending *PendingTUN) {
	if pending.PreviousDNS == nil {
		return
	}
	st.Subscriptions = slices.Clone(st.Subscriptions)
	for i := range st.Subscriptions {
		if st.Subscriptions[i].ID == pending.SubscriptionID {
			st.Subscriptions[i].DNS = *pending.PreviousDNS
		}
	}
}

func validResolver(raw string, bootstrap bool) bool {
	if raw == "system" {
		return !bootstrap
	}
	if raw == "" || len(raw) > 512 || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "\r\n\t #") {
		return false
	}
	if net.ParseIP(raw) != nil {
		return true
	}
	if host, port, e := net.SplitHostPort(raw); e == nil && !strings.Contains(raw, "://") {
		return net.ParseIP(host) != nil && validDNSPort(port)
	}
	u, e := url.Parse(raw)
	if e != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Port() != "" && !validDNSPort(u.Port()) {
		return false
	}
	if u.Scheme != "https" && u.Scheme != "tls" && u.Scheme != "tcp" && u.Scheme != "udp" {
		return false
	}
	if bootstrap && net.ParseIP(u.Hostname()) == nil {
		return false
	}
	return u.Scheme == "https" || (u.Path == "" || u.Path == "/")
}
func validDNSPort(raw string) bool {
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
		if n > 65535 {
			return false
		}
	}
	return n > 0
}
func validateDNS(d DNSSettings) error {
	if d.Override && len(d.Nameservers) == 0 {
		return errors.New("DNS 覆写需要至少一个解析上游")
	}
	for i, list := range [][]string{d.Nameservers, d.Bootstrap, d.ProxyNameservers} {
		if len(list) > 8 {
			return errors.New("每组 DNS 上游最多 8 个")
		}
		for _, raw := range list {
			if !validResolver(raw, i == 1) {
				return errors.New("DNS 地址无效：支持 system、IP 或 udp/tcp/tls/https 地址；引导 DNS 必须使用 IP，不支持凭证和附加参数")
			}
		}
	}
	return nil
}
