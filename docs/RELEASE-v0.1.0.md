# Meshport v0.1.0

Meshport 首个发布版本：通过浏览器管理 Linux 服务器的 Mihomo 代理，无需图形桌面或服务器端 Node.js。

## 功能

- Go 管理服务与 Vue 3 前端，前端资源嵌入单个管理程序。
- 一次性初始化凭证、管理员密码登录与会话管理。
- Mihomo / Clash YAML URL 订阅与手动导入，多订阅单独启用。
- 策略组与节点查看、手动选择持久化。
- 代理启停、重启，规则 / 全局 / 直连模式。
- HTTP / SOCKS5 端口配置，外部代理独立认证。
- TUN 开关及独立的 60 秒未确认自动恢复机制。
- SQLite 状态保存、配置失败恢复、Web 端口迁移。
- 原生 systemd 安装与卸载，默认卸载保留数据。

## 安装

在服务器上执行一条命令即可自动下载最新稳定版、校验安装包并完成安装：

```bash
curl -fsSL https://raw.githubusercontent.com/shushu594088/meshport/main/install.sh | sudo bash
```

当前支持 Ubuntu / Debian 的 amd64、arm64。命令保持不变，后续系统适配通过各 Release 的 `INSTALL-TARGETS.tsv` 清单选择对应安装包；当前尚未提供 CentOS 安装包。

需要指定端口时，末尾改为 `sudo bash -s -- --port 8889`。需要固定本版本时，末尾改为 `sudo bash -s -- --version v0.1.0`。

一键安装后，执行 `sudo bash /usr/libexec/meshport-uninstall` 可卸载并保留数据；加 `--purge` 清除数据。已有安装会被拒绝覆盖，升级前先保留数据卸载。

也可以手动安装：

从本 Release 下载与你服务器架构对应的文件：

| 架构 | 文件 |
| --- | --- |
| amd64 / x86_64 | `meshport-0.1.0-linux-amd64.tar.gz` |
| arm64 / aarch64 | `meshport-0.1.0-linux-arm64.tar.gz` |

同时下载 `SHA256SUMS`，在服务器上执行：

```bash
sudo apt update
sudo apt install -y curl gzip coreutils iproute2 sudo ca-certificates
sha256sum --ignore-missing -c SHA256SUMS
mkdir -p meshport-install
# arm64 请替换为对应文件名
tar -xzf meshport-0.1.0-linux-amd64.tar.gz -C meshport-install
cd meshport-install
sudo bash install.sh
```

安装脚本会下载并验证固定版本 Mihomo v1.19.32。安装后访问 `http://服务器IP:8888`，使用安装输出的一次性凭证初始化；凭证也可通过 `sudo cat /var/lib/meshport/init-token` 获取。

**默认：Web 后台开机自启；代理内核不开机自启，代理与 TUN 均关闭。** 添加并启用订阅后，再在后台手动启动代理。

完整的自动识别架构下载命令、首次初始化与日常使用方法见[项目首页](https://github.com/shushu594088/meshport#安装)。

## 验证范围

Ubuntu 22.04 amd64 已验证真实内核、浏览器交互、节点与模式持久化、端口冲突保护、Web 端口迁移、TUN 独立回滚及卸载重装。GitHub Actions 的 Ubuntu 24.04 构建、测试和双架构打包通过；一键入口从 GitHub 下载、校验、安装、默认自启状态、重复安装保护及卸载也已通过验证。

Debian、arm64 硬件和整机重启尚未进行完整实机验收。第一版不包含通用订阅转换、Docker、多用户或内置证书管理。

运行服务器只需安装包和系统依赖，不需要 Go / Node.js。第三方许可随安装包提供；Mihomo 由安装脚本从上游单独下载。
