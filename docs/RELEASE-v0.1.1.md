# Meshport v0.1.1

本次更新增加节点延迟测速、网站连通性与地区支持测试，以及 Web / 命令行更新。

## 项目更新内容

- **节点 → 一键测速**：测试全部实际节点，覆盖其他策略组、筛选外和分页外节点；每个节点也可单独测速。显示进度、失败结果，支持取消；不会切换当前节点。使用 Google 204 端点，超时 5 秒，最多 4 项并发，测试的是连接延迟而非下载带宽。
- **测试 → 测试全部**：新增 12 个网站卡片，覆盖哔哩哔哩大陆/港澳台、Bahamut Anime、ChatGPT Web、Claude、Disney+、Gemini、Netflix、Prime Video、Spotify、TikTok、YouTube Premium。支持单项重测，显示状态、可识别的地区、耗时和测试时间。
- **设置 → 版本与更新**：检查 GitHub 最新稳定版本，一键下载并更新，更新确认后自动等待后台重启与重新连接。
- **新增 `sudo meshport update`**：在 SSH 终端检查并更新到最新稳定版，复用同一套校验及安装流程。
- **更新保护**：匹配系统和处理器架构，验证 SHA-256、包内路径、文件类型及程序版本；保留密码、订阅、设置和当前 Mihomo 二进制。下载/校验失败不替换程序，安装或后台健康检查失败时尝试恢复原程序。
- **交互与构建**：新增批量进度、取消、部分失败重试、手机布局及自动无障碍检查；对话框标签使用独立 ID，构建脚本直接读取程序版本生成对应安装包。

网站测试通过服务器当前代理模式与规则发起，各网站可能使用不同出口。结果区分“支持”“不支持”“可访问”“测试失败”；部分站点只确认网页连通性，不将网页可访问自动视为账号、会员或视频播放解锁。切换节点、订阅或模式后需重新测试。

## 安装

新安装可执行：

```bash
curl -fsSL https://raw.githubusercontent.com/shushu594088/meshport/main/install.sh | sudo bash
```

固定安装本版本：

```bash
curl -fsSL https://raw.githubusercontent.com/shushu594088/meshport/main/install.sh | sudo bash -s -- --version v0.1.1
```

支持 Ubuntu / Debian 的 amd64、arm64。默认 Web 后台开机自启，代理不开机自启，代理和 TUN 均关闭。运行服务器无需安装 Go 或 Node.js。

| 架构 | 安装包 |
| --- | --- |
| amd64 / x86_64 | `meshport-0.1.1-linux-amd64.tar.gz` |
| arm64 / aarch64 | `meshport-0.1.1-linux-arm64.tar.gz` |

本 Release 同时提供 `SHA256SUMS` 和 `INSTALL-TARGETS.tsv`。手动下载后，先执行 `sha256sum --ignore-missing -c SHA256SUMS`，解压对应包并运行其中的 `sudo bash install.sh`。

## 从 v0.1.0 升级

v0.1.0 没有更新命令和新辅助程序权限，首次升级需保留数据卸载后重装。通过一键脚本安装的 v0.1.0 可执行：

```bash
# 默认卸载保留管理员密码、订阅与设置；不要加 --purge。
sudo bash /usr/libexec/meshport-uninstall
curl -fsSL https://raw.githubusercontent.com/shushu594088/meshport/main/install.sh | sudo bash -s -- --version v0.1.1
```

手动安装的用户使用原安装包中的 `uninstall.sh` 默认卸载，再安装新包。重装保留原密码和配置；安装后需要手动启动代理。

从 v0.1.1 起，后续版本可通过 Web 一键更新或执行：

```bash
sudo meshport update
```

一键更新仅支持 Linux systemd 安装。更新会短暂重启管理后台，独立运行的代理不主动停止；TUN 待确认或设置未保存时，请先处理再更新。

## 验证范围

Go 单元测试、`go vet`、TypeScript 检查和前端生产构建通过；amd64、arm64 安装包已交叉编译。7 项 Playwright 浏览器测试通过，覆盖测速、取消、网站结果/错误、更新确认与断线重连、原有订阅和设置流程。手机宽度网站页与设置页的 axe 自动检查无违规，静态 UI 审计无错误。

新增更新流程尚未完成真实 systemd 升级验收；这 12 个网站未宣称全部实际解锁。Debian、arm64 硬件和整机重启的完整验收仍待完成。详见[验证记录](https://github.com/shushu594088/meshport/blob/main/docs/VERIFICATION.md)。
