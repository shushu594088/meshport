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

发布前重新生成第三方许可汇总：`python scripts/generate-notices.py`（需要已安装前端依赖和 Go）。然后运行构建脚本，将两个架构的安装包和 `SHA256SUMS` 上传到对应版本的 GitHub Release。构建脚本只将本版本的两个安装包写入校验清单。
