package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const Repository = "https://github.com/shushu594088/meshport"
const StatusPath = "/var/lib/meshport/update-status.json"
const Sudoers = "meshport ALL=(root) NOPASSWD: /usr/libexec/meshport-helper start, /usr/libexec/meshport-helper stop, /usr/libexec/meshport-helper restart, /usr/libexec/meshport-helper enable, /usr/libexec/meshport-helper disable, /usr/libexec/meshport-helper status, /usr/libexec/meshport-helper preflight, /usr/libexec/meshport-helper arm, /usr/libexec/meshport-helper disarm, /usr/libexec/meshport-helper update\n"

type Release struct {
	Current     string    `json:"current"`
	Latest      string    `json:"latest"`
	Available   bool      `json:"available"`
	CanUpdate   bool      `json:"can_update"`
	URL         string    `json:"url"`
	PublishedAt string    `json:"published_at"`
	Notes       string    `json:"notes"`
	CheckedAt   time.Time `json:"checked_at"`
}
type Status struct {
	Phase     string    `json:"phase"`
	Message   string    `json:"message"`
	Version   string    `json:"version,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

var stableVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func NewClient() *http.Client {
	return &http.Client{Timeout: 45 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 || req.URL.Scheme != "https" {
			return errors.New("更新地址重定向无效")
		}
		return nil
	}}
}
func newer(latest, current string) bool {
	a, b := stableVersion.FindStringSubmatch(latest), stableVersion.FindStringSubmatch(current)
	if a == nil || b == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		x, ex := strconv.ParseUint(a[i], 10, 32)
		y, ey := strconv.ParseUint(b[i], 10, 32)
		if ex != nil || ey != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}
func Fetch(ctx context.Context, client *http.Client, endpoint string, limit int64) ([]byte, error) {
	req, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("User-Agent", "Meshport-update")
	res, e := client.Do(req)
	if e != nil {
		return nil, errors.New("无法连接 GitHub，请检查服务器网络后重试")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub 请求失败（HTTP %d），请稍后重试", res.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, errors.New("更新响应读取失败或超过大小限制")
	}
	return b, nil
}
func Check(ctx context.Context, client *http.Client, current string) (Release, error) {
	b, e := Fetch(ctx, client, "https://api.github.com/repos/shushu594088/meshport/releases/latest", 1<<20)
	if e != nil {
		return Release{}, e
	}
	var metadata struct {
		Tag        string `json:"tag_name"`
		Published  string `json:"published_at"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.Unmarshal(b, &metadata) != nil || !stableVersion.MatchString(metadata.Tag) || !strings.HasPrefix(metadata.Tag, "v") || metadata.Draft || metadata.Prerelease {
		return Release{}, errors.New("GitHub 最新稳定版本信息无效")
	}
	return Release{Current: current, Latest: strings.TrimPrefix(metadata.Tag, "v"), Available: newer(metadata.Tag, current), CanUpdate: runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"), URL: Repository + "/releases/tag/v" + strings.TrimPrefix(metadata.Tag, "v"), PublishedAt: metadata.Published, Notes: metadata.Body, CheckedAt: time.Now().UTC()}, nil
}

func selectAsset(manifest, distro, osVersion, arch, version string) (string, error) {
	s := bufio.NewScanner(strings.NewReader(manifest))
	if !s.Scan() || strings.TrimSpace(s.Text()) != "# meshport-install-targets-v1" {
		return "", errors.New("安装清单格式无效")
	}
	best, asset := 0, ""
	validName := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._+-]*\.tar\.gz$`)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 4 {
			return "", errors.New("安装清单字段无效")
		}
		if f[0] != distro || f[2] != arch {
			continue
		}
		score := 0
		switch f[1] {
		case osVersion:
			score = 3
		case strings.Split(osVersion, ".")[0]:
			score = 2
		case "*":
			score = 1
		}
		if score == 0 {
			continue
		}
		name := strings.ReplaceAll(f[3], "{version}", version)
		if !validName.MatchString(name) {
			return "", errors.New("更新包文件名无效")
		}
		if score == best && name != asset {
			return "", errors.New("安装清单存在冲突")
		}
		if score > best {
			best, asset = score, name
		}
	}
	if s.Err() != nil || asset == "" {
		return "", errors.New("最新版本没有匹配当前系统和架构的更新包")
	}
	return asset, nil
}
func verify(data []byte, sums, asset string) error {
	selected := ""
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || strings.TrimPrefix(f[1], "*") != asset {
			continue
		}
		if len(f) != 2 || selected != "" {
			return errors.New("校验清单格式无效")
		}
		decoded, err := hex.DecodeString(f[0])
		if err != nil || len(decoded) != 32 {
			return errors.New("校验清单格式无效")
		}
		selected = strings.ToLower(f[0])
	}
	sum := sha256.Sum256(data)
	if selected == "" || selected != hex.EncodeToString(sum[:]) {
		return errors.New("更新包 SHA-256 校验失败，已取消更新")
	}
	return nil
}
