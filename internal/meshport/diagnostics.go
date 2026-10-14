package meshport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Site struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"-"`
}

// The server owns the destinations. Callers cannot turn diagnostics into an arbitrary URL fetcher.
var diagnosticSites = []Site{
	{"bilibili-cn", "哔哩哔哩大陆", "https://api.bilibili.com/pgc/player/web/playurl?avid=82846771&ep_id=307247&fnval=16"},
	{"bilibili-tw", "哔哩哔哩港澳台", "https://api.bilibili.com/pgc/player/web/playurl?avid=18281381&cid=29892777&ep_id=183799&fnval=16"},
	{"bahamut", "Bahamut Anime", "https://ani.gamer.com.tw/"},
	{"chatgpt", "ChatGPT Web", "https://api.openai.com/compliance/cookie_requirements"},
	{"claude", "Claude", "https://claude.ai/"},
	{"disney", "Disney+", "https://www.disneyplus.com/"},
	{"gemini", "Gemini", "https://gemini.google.com/"},
	{"netflix", "Netflix", "https://www.netflix.com/title/70143836"},
	{"prime", "Prime Video", "https://www.primevideo.com/"},
	{"spotify", "Spotify", "https://www.spotify.com/api/content/v1/country-selector?platform=web&format=json"},
	{"tiktok", "TikTok", "https://www.tiktok.com/"},
	{"youtube", "YouTube Premium", "https://www.youtube.com/premium?hl=en"},
}

type DiagnosticResult struct {
	Status    string    `json:"status"`
	Detail    string    `json:"detail"`
	Region    string    `json:"region,omitempty"`
	Delay     int64     `json:"delay"`
	CheckedAt time.Time `json:"checked_at"`
	Revision  int64     `json:"revision"`
}

func (h *HTTP) diagnose(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	if h.testsActive >= 4 {
		h.mu.Unlock()
		problem(w, 429, errors.New("已有 4 项测试正在运行，请稍后重试"))
		return
	}
	h.testsActive++
	h.mu.Unlock()
	defer func() { h.mu.Lock(); h.testsActive--; h.mu.Unlock() }()
	st, e := h.App.Store.Load()
	if e != nil {
		problem(w, 500, errors.New("读取状态失败"))
		return
	}
	if !h.App.Service.Status().Running {
		problem(w, 409, errors.New("内核已停止，请先启动代理"))
		return
	}
	var input struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if e = decode(w, r, &input); e != nil {
		problem(w, 400, e)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	result := DiagnosticResult{Status: "failed", Revision: st.Revision}
	start := time.Now()
	switch r.URL.Path {
	case "/api/tests/delay":
		p, err := h.App.Kernel.Proxies(st.Secret)
		if err != nil {
			problem(w, 503, err)
			return
		}
		if _, ok := p[input.Name]; !ok || input.Name == "" {
			problem(w, 400, errors.New("节点不存在，请刷新节点列表"))
			return
		}
		kernel, ok := h.App.Kernel.(interface {
			Delay(context.Context, string, string) (int, error)
		})
		if !ok {
			problem(w, 503, errors.New("当前内核不支持延迟测试"))
			return
		}
		delay, err := kernel.Delay(ctx, input.Name, st.Secret)
		if err != nil {
			result.Detail = "连接超时或测试失败，请重试"
		} else {
			result.Status, result.Detail, result.Delay = "supported", "节点可用", int64(delay)
		}
	case "/api/tests/site":
		var target *Site
		for i := range diagnosticSites {
			if diagnosticSites[i].ID == input.ID {
				target = &diagnosticSites[i]
				break
			}
		}
		if target == nil {
			problem(w, 400, errors.New("测试网站不存在"))
			return
		}
		client, transport := diagnosticClient(st.Settings)
		defer transport.CloseIdleConnections()
		result = testSite(ctx, client, *target)
		result.Revision = st.Revision
		result.Delay = time.Since(start).Milliseconds()
	default:
		problem(w, 404, errors.New("测试接口不存在"))
		return
	}
	result.CheckedAt = time.Now().UTC()
	current, err := h.App.Store.Load()
	if err != nil || current.Revision != st.Revision {
		problem(w, 409, errors.New("测试期间代理配置已变化，请重新测试"))
		return
	}
	jsonResponse(w, 200, result)
}

func diagnosticClient(s Settings) (*http.Client, *http.Transport) {
	proxyURL := &url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", s.HTTPPort)}
	if s.AllowLAN {
		proxyURL.User = url.UserPassword(s.ProxyUser, s.ProxyPassword)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL), TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second}
	return &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" {
			return errors.New("无效重定向")
		}
		return nil
	}}, transport
}

func fetchProbe(ctx context.Context, client *http.Client, target string) (*http.Response, string, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", target, nil)
	if e != nil {
		return nil, "", e
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	res, e := client.Do(req)
	if e != nil {
		return nil, "", e
	}
	defer res.Body.Close()
	data, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if len(data) > 2<<20 {
		return res, "", errors.New("测试响应过大")
	}
	return res, string(data), e
}

var youtubeRegion = regexp.MustCompile(`"GL"\s*:\s*"([A-Z]{2})"`)
var primeRegion = regexp.MustCompile(`"currentTerritory"\s*:\s*"([A-Z]{2})"`)
var geoPattern = regexp.MustCompile(`data-geo="([A-Z]{2})"`)

func testSite(ctx context.Context, client *http.Client, site Site) DiagnosticResult {
	r := DiagnosticResult{Status: "failed", Detail: "连接超时或测试失败，请重试"}
	res, body, e := fetchProbe(ctx, client, site.URL)
	if e != nil {
		return r
	}
	lower := strings.ToLower(body)
	// Extract only provider-specific exit markers; country-selector lists are not exit locations.
	var region *regexp.Regexp
	switch site.ID {
	case "youtube":
		region = youtubeRegion
	case "prime":
		region = primeRegion
	case "bahamut":
		region = geoPattern
	}
	if region != nil {
		if match := region.FindStringSubmatch(body); len(match) > 1 {
			r.Region = match[1]
		}
	}
	r.Detail = fmt.Sprintf("HTTP %d", res.StatusCode)
	// Anti-bot challenges and HTTP errors are inconclusive, not proof of geographic blocking.
	if res.StatusCode == 429 || strings.Contains(lower, "just a moment") || strings.Contains(lower, "cf-chl-") {
		r.Detail = "触发网站验证或限流，请稍后重试"
		return r
	}
	for _, marker := range []string{"unsupported_country", "not available in your country", "not available in your region", "forbidden-location"} {
		if strings.Contains(lower, marker) {
			r.Status, r.Detail = "unsupported", "网站明确限制当前出口地区"
			return r
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return r
	}
	r.Status, r.Detail = "reachable", "网页可访问，尚未确认账号或播放解锁"
	switch site.ID {
	case "bilibili-cn", "bilibili-tw":
		var payload struct {
			Code *int `json:"code"`
		}
		if json.Unmarshal([]byte(body), &payload) != nil || payload.Code == nil {
			r.Status, r.Detail = "failed", "测试接口响应无效"
			break
		}
		switch *payload.Code {
		case 0:
			r.Status, r.Detail = "supported", "测试番剧播放接口可用"
		case -10403:
			r.Status, r.Detail = "unsupported", "测试番剧受到地区限制"
		default:
			r.Status, r.Detail = "failed", fmt.Sprintf("播放接口返回代码 %d", *payload.Code)
		}
	case "chatgpt":
		var data map[string]any
		if json.Unmarshal([]byte(body), &data) != nil || len(data) == 0 {
			r.Status, r.Detail = "failed", "地区检查接口响应无效"
			break
		}
		r.Status, r.Detail = "supported", "地区检查接口可用；实际使用仍需账号验证"
		if trace, text, err := fetchProbe(ctx, client, "https://chatgpt.com/cdn-cgi/trace"); err == nil && trace.StatusCode == 200 {
			for _, line := range strings.Split(text, "\n") {
				if strings.HasPrefix(line, "loc=") {
					code := strings.TrimSpace(strings.TrimPrefix(line, "loc="))
					if regexp.MustCompile(`^[A-Z]{2}$`).MatchString(code) {
						r.Region = code
					}
				}
			}
		}
	case "youtube":
		if strings.Contains(lower, `"browseid":"spunlimited"`) || strings.Contains(lower, "ad-free") {
			r.Status, r.Detail = "supported", "Premium 页面提供订阅入口"
		}
	case "prime":
		if strings.Contains(lower, `"isservicerestricted":true`) {
			r.Status, r.Detail = "unsupported", "当前地区服务受限"
		}
	case "disney":
		if strings.Contains(res.Request.URL.Path, "unavailable") || strings.Contains(res.Request.URL.Path, "preview") {
			r.Status, r.Detail = "unsupported", "网站跳转至地区不可用页面"
		}
	}
	return r
}
