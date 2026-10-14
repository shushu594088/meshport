# 开发与运维

## 目录

- `cmd/meshport`：命令行入口、HTTP 服务、独立 TUN 恢复命令。
- `cmd/meshport-helper`：仅接受固定操作的 root 辅助程序，不执行 shell，不接受路径或 unit 参数。
- `internal/meshport`：状态、配置生成、登录、API、服务控制与恢复事务。
- `web/src`：Vue 3 页面及共享表单、对话框、模式控件。
- `internal/ui/dist`：前端生产构建，直接嵌入 Go 程序。
- `deploy` / `scripts`：systemd 单元、构建、安装、卸载和服务器验收。

## 状态与恢复

SQLite 的单行状态包含订阅原文、设置、当前订阅、按订阅区分的节点选择、密码哈希及内核密钥；会话单独存储哈希与到期时间。`state` 的 JSON 不直接作为 API 响应。订阅 URL、缓存内容、代理密码和内核密钥不能通过 `/api/state` 读取。

所有状态更改由应用内互斥锁和 Linux `flock` 串行化。打开已有数据库不获取操作锁，避免 systemd 启动前检查等待调用者锁的死锁。配置更新写 `recovery.json`、原子替换 `runtime.yaml`、应用内核并恢复选择、提交 SQLite，最后删除恢复日志。错误恢复原配置；后台重新启动时处理未完成的恢复日志。

代理监听变化通过独立内核服务重启应用，其他订阅或模式更改通过本机 Mihomo API 应用。内核异常不会结束后台进程，退出后台也不停止内核。

## TUN

默认关闭。开启时生成绕行配置、检查设备与网络、记录原设置并先启动独立的 systemd 回滚计时器，然后应用配置。开启期间禁止其他配置和订阅修改。必须在 60 秒内通过 `/api/tun/confirm` 确认；否则恢复原设置和运行状态。计时器直接执行 `meshport rollback`，不依赖 Web 进程。

`meshport-mihomo.service` 的 `ExecStartPre` 运行 `meshport guard`：已确认的 TUN 在每次内核启动时重新建立确认窗口；过期的未确认配置在启动前恢复。内核 `ExecStopPost` 只清理保留的路由表 20220，正常 TUN 网卡关闭由 Mihomo 完成。不修改系统 resolver 文件或清空全局规则。

systemd 单元不能添加使非 root 进程隐式启用 `NoNewPrivileges` 的 seccomp 选项（例如 `RestrictAddressFamilies`），否则固定 sudo 辅助程序无法运行。管理程序保持普通用户权限；sudoers 逐一列出允许的固定动作。内核仅通过 systemd 获得网络能力，工作目录与配置仍归专用用户。

内核日志级别为 silent，systemd 关闭其标准输出与错误输出，避免第三方集合启动错误回显 URL 凭证。后台仅返回固定的验证错误；后台和 systemd 服务生命周期事件仍可在 journald 查看。

## API

所有 API 响应禁止缓存。除 `/api/auth`、`/api/setup`、`/api/login` 外需要 HttpOnly 会话 cookie。变更请求必须带 `X-Meshport-Request: 1`，提供 Origin 时必须匹配 Host；服务不开放 CORS。登录按来源地址限流。

| 方法 | 路径 | 功能 |
|---|---|---|
| GET | `/api/auth` | 初始化和登录状态 |
| POST | `/api/setup` | 一次性凭证设置密码 |
| POST | `/api/login`、`/api/logout` | 登录、退出 |
| GET | `/api/state` | 脱敏状态、设置、订阅与服务状态 |
| POST | `/api/subscriptions` | 添加 URL 或 YAML 订阅 |
| POST | `/api/subscriptions/{id}/activate` | 启用订阅 |
| POST | `/api/subscriptions/{id}/update` | 更新 URL 订阅 |
| DELETE | `/api/subscriptions/{id}` | 删除订阅 |
| PUT | `/api/settings` | 带 revision 保存设置 |
| POST | `/api/service` | start、stop、restart |
| GET | `/api/nodes` | 内核策略组、节点和选择 |
| PUT | `/api/nodes/select` | 选择手动策略组的合法成员 |
| GET | `/api/tests/sites` | 固定网站测试清单 |
| POST | `/api/tests/delay` | 按 name 测试节点延迟，固定 Google 204、5s 超时 |
| POST | `/api/tests/site` | 按 id 测试网站，走本机 HTTP 代理与当前规则 |
| GET | `/api/update` | 检查 GitHub 最新稳定版本，不安装 |
| GET | `/api/update/status` | 本机版本与更新任务状态 |
| POST | `/api/update` | 固定 sudo helper 启动独立 systemd 更新任务 |
| POST | `/api/tun/confirm` | 在期限内确认可访问 |
| PUT | `/api/password` | 校验原密码、设置新密码并撤销所有会话 |

## 构建与测试

```bash
npm --prefix web ci
npm --prefix web run build
go test ./...
go vet ./...
bash scripts/build.sh
```

浏览器验收针对已完成初始化的测试实例，默认地址 `http://127.0.0.1:18888`。可设置 `MESHPORT_TEST_URL` 和 `MESHPORT_TEST_PASSWORD`。测试会创建、启用和删除验收订阅，只应对测试数据运行。

本机没有真实 Linux 内核时，可在终端设置 `MESHPORT_BROWSER_FIXTURE=1` 后运行 `go test ./internal/meshport -run '^TestBrowserFixtureServer$' -v -timeout 12m`，它提供仅监听 127.0.0.1:18888 的临时后台，十分钟后结束。模拟内核和服务不执行系统命令、不发起公网探测；浏览器使用测试密码 `meshport-test-password-2026`。新增 diagnostics.spec.ts 模拟各网站响应和更新重连，以验证 UI 状态；这不能代替真实网站与 systemd 更新验收。

## 诊断与更新

诊断 POST 在完成登录与同源检查后独立处理，最多四个并发请求，不持有配置变更锁；请求取消会传递到内核延迟 API 或网站 HTTP 请求。结果携带配置 revision，测试期间 revision 变化返回 409。站点目的地址由服务器固定，用户不能指定任意 URL。网站请求必须通过 127.0.0.1 的 HTTP 代理；开启外部认证时使用已有凭证，不回退到直连，不返回凭证或原始网络错误。结果只在浏览器内存中暂存。

更新代码位于 internal/update。CLI `sudo meshport update` 检查稳定版、下载系统清单/校验和/安装包、拒绝路径穿越、链接、重复文件和错误架构，验证新程序的版本后才停止管理后台。文件替换使用原子 rename；保存原文件及权限，失败时恢复原程序并启动原后台。代理内核二进制与数据目录均不更新。该更新格式不支持数据库不兼容迁移；需要迁移的未来版本应扩展迁移/备份协议后再发布。

Web POST 通过固定 `meshport-helper update` 操作运行 `systemd-run --unit=meshport-update /usr/bin/meshport update`。任务独立于 Web 服务，/run/meshport-update.lock 阻止同时更新；状态保存在 /var/lib/meshport/update-status.json，供重连页面读取。sudoers 只增加这个固定操作，不接受路径、URL 或任意参数。首次从 v0.1.0 升级需保留数据卸载重装，因为旧辅助程序及命令行没有更新功能。

```bash
cd web
npx playwright install chromium
npm run test:e2e
```

服务器端 `scripts/acceptance.py` 要求全新的已安装实例、测试账号可免密 sudo。它验证真实内核、后台独立性、监听迁移、失败保护和 TUN 自动恢复，会更改 Meshport 测试设置。运行时不要同时执行浏览器测试，因为脚本会短暂关闭后台以验证独立恢复。

安装包默认在线下载固定版本内核。如需离线安装，可以将官方的 `mihomo-linux-amd64-compatible-v1.19.32.gz` 或 `mihomo-linux-arm64-v1.19.32.gz` 放在解压目录；安装脚本仍会验证固定 SHA-256。

## 反向代理与备份

后台支持 Nginx/Caddy 终止 HTTPS。上游连接指向实际 Web 端口，保留正确的 Host，关闭缓存。设置 `/etc/meshport/environment` 中的 `MESHPORT_HOST=127.0.0.1` 和 `MESHPORT_SECURE_COOKIE=true`，然后重启后台。变更 Web 端口时需同步修改反向代理上游。

备份前停止管理服务；使用 SQLite backup API 导出数据库，或同时复制数据库及 WAL（不能只复制可能尚未 checkpoint 的主数据库）。保存 `/etc/meshport` 与 `/var/lib/meshport`，并保持严格权限；这些目录包含订阅凭证和代理认证信息。不要将其提交到 Git。

内核 API 和 TUN 配置依据 [Mihomo 官方 API 文档](https://wiki.metacubex.one/api/) 与 [TUN 文档](https://wiki.metacubex.one/config/inbound/tun/)。

## 发布

发布前重新生成第三方许可汇总：`python scripts/generate-notices.py`（需要已安装前端依赖和 Go）。然后运行构建脚本，将安装包、`SHA256SUMS` 和 `INSTALL-TARGETS.tsv` 上传到对应版本的 GitHub Release。构建脚本只将本版本的安装包写入校验清单。

仓库根目录 `install.sh` 是固定的一键入口，与安装包内的 `scripts/install.sh` 有不同职责：前者检测系统并下载最新对应安装包，后者完成该版本的实际安装。用户命令始终指向 `main/install.sh`，不用随着版本或发行版变化修改。

`deploy/install-targets.tsv` 是发行版选择的唯一来源，四列分别为发行版 ID、系统版本、架构和安装包文件名模板。系统版本优先级为精确版本 > 大版本 > `*`；`{version}` 替换为 Release 的版本号。发布时复制为 `INSTALL-TARGETS.tsv`。例如以后增加 CentOS 9 包，可增加 `centos<TAB>9<TAB>amd64<TAB>meshport-{version}-linux-centos9-amd64.tar.gz`，并实现对应包内的发行版安装逻辑，无需更改用户安装命令。当前清单只声明 Ubuntu 和 Debian。

执行 `bash scripts/bootstrap-test.sh` 可检查架构选择、未来发行版与版本路由、路径约束和校验失败保护。CI 另在独立 Ubuntu runner 中从 GitHub Release 下载安装，验证默认自启、拒绝重复覆盖和卸载。不会对用户已安装的服务器执行重装测试。
