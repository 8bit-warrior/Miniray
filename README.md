# Miniray

Miniray 是一款基于 SQLite 的代理管理面板，支持本地入站、客户端、订阅和 Xray 配置管理。内置 Xray-core **v26.6.27**，并支持 TUIC、AmneziaWG 和 MTProto。Web 面板与代理服务独立运行。XDNS 设置会转换为内置核心支持的配置格式；对于 v26.6.27 不支持的选项，程序会明确报错。

## Linux 安装

支持 **amd64/x86_64** 和 **arm64/aarch64** 架构，适用于采用 systemd 的 Linux 系统及采用 OpenRC 的 Alpine Linux。后续 Release 仅提供这两种 Linux 架构的 `.tar.gz` 安装包。

以 root 用户执行安装命令。系统需要提供 `bash`、`curl`、`tar`、`sha256sum` 和 `install`。Alpine Linux 可先执行 `apk add bash curl tar coreutils` 安装依赖。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/8bit-warrior/Miniray/main/install.sh)
```

安装脚本会下载最新发布版本、验证 SHA-256 校验和、安装 Miniray 并注册系统服务。如需安装指定版本：

```bash
MINIRAY_UPDATE_TAG=v0.0.4 bash <(curl -fsSL https://raw.githubusercontent.com/8bit-warrior/Miniray/main/install.sh)
```

执行 `miniray` 或 `mr` 均可打开中文管理菜单。菜单采用单页分块布局：顶部首行显示当前 Miniray 版本号，随后两行显示 Web 面板和 Xray 核心的实时状态、核心版本及各自的开机自启状态；下方分为面板管理、Xray 核心和系统工具三块，每行一个功能。分组使用等宽、左右闭合的边框。Web 管理界面的桌面侧边栏和移动端抽屉也会在顶部显示当前版本号。语言切换固定为菜单 12，中文界面显示 `English`，英文界面显示 `中文`；语言选择会保存，更新后仍然保留。

面板管理提供按状态切换的启动/停止和开机自启开关、重启面板、修改 Web 端口、修改 Web Path、重置管理员用户名和密码，以及查看面板地址与用户名。输入新密码时不会显示输入内容，密码通过标准输入交给后台处理，不会出现在进程命令行中。修改 Web 端口或 Path 后，只有面板正在运行时才会重启面板，不会停止代理核心。修改 Path 时会补齐首尾斜杠，并显示修改后的面板地址。

执行 `miniray update` 会先输出当前已安装版本号、下载目标版本和更新后的版本号。安装器核对 Release 标签与二进制版本，更新时保留原有数据库路径和已禁用的服务开机自启设置。全新安装检测到遗留 Miniray 数据库时，会提示选择复用原数据库、清除旧数据库重新安装或取消。选择清除时会暂存旧数据库，安装失败则恢复。

管理菜单的第一行显示当前版本号。“系统工具”提供完整卸载，或执行 `miniray uninstall`。卸载先确认操作，再询问是否清除数据库；默认保留数据库，以便重新安装复用。卸载会停止并删除 Miniray 的面板、核心、代理管理服务及其自启配置，同时删除管理命令、程序、运行文件和日志。选择清除数据库会额外删除 Miniray 数据库及同目录配置；不会删除 3x-ui 或 `/var/backups/miniray` 中的迁移备份。

## 从 3x-ui 迁移

当安装脚本检测到已有 3x-ui 时，会提供以下三种安装策略（下方为脚本当前的实际英文提示）：

```text
1) Coexist: install and run Miniray alongside 3x-ui
2) Migrate: uninstall 3x-ui and import supported data into Miniray
3) Cancel this installation
Select [1/2/3]:
```

**1 — 共存：** 保留 3x-ui 及其数据库不变。全新安装的 Miniray 使用独立的 SQLite 数据库，并为面板、Xray API 和监控指标分配独立端口，避开 3x-ui 已配置的端口和当前被占用的 TCP 端口。此策略需要 Python 3，不会继承 3x-ui 的用户或入站配置。若 Miniray 已安装，则保留其现有配置。

**2 — 迁移：** 卸载 3x-ui 前，安装脚本会先停止旧服务，生成一致性的 SQLite 数据库快照，并在 `/var/backups/miniray/` 下创建仅 root 可访问的备份，然后选择性导入受支持的数据。只有在 Miniray 新服务成功运行后，才会移除 3x-ui；如果安装失败，则保留旧服务及数据库。为避免覆盖已有数据，该策略要求使用全新的 Miniray 数据库。

迁移范围包括受支持的本地入站、客户端、关联关系、流量计数器、兼容设置以及管理员密码哈希。旧配置目录中的证书会被复制并更新引用路径。已移除的功能、远程入站、会话、API 令牌和双重验证状态不会导入。旧配置目录外的证书仍需在其原有路径保持可访问。

**3 — 取消：** 在下载软件包或修改现有安装前退出。输入无效选项时会重新提示。迁移策略不支持使用 PostgreSQL 数据库或通过 Docker 部署的旧版 3x-ui。

## 服务管理

| 命令 | 功能 |
| --- | --- |
| `miniray start-panel` / `miniray stop-panel` | 启动或停止 Web 面板及订阅监听服务 |
| `miniray enable-panel` / `miniray disable-panel` | 启用或禁用面板开机自启 |
| `miniray start-core` / `miniray stop-core` | 启动或停止代理核心及协议管理进程 |
| `miniray enable-core` / `miniray disable-core` | 启用或禁用代理服务开机自启 |
| `miniray web-info` | 输出 Web 面板地址和管理员用户名（共两行） |
| `miniray change-port` | 交互式修改 Web 面板端口 |
| `miniray change-path` | 交互式修改 Web 面板路径 |
| `miniray reset-credentials` | 交互式重置管理员用户名与密码 |

在 Alpine Linux 中，OpenRC 服务脚本位于 `/etc/init.d/`，环境配置位于 `/etc/conf.d/miniray`，日志位于 `/var/log/miniray/`。

面板地址优先使用本机默认路由对应的 IPv4 地址；若配置了 Web 证书，则使用 `https://`，否则使用 `http://`。本机无法检测到非回环 IPv4 时将回退到 `127.0.0.1`。

停止面板会关闭面板监听端口，但代理服务继续运行。执行 `miniray start-panel` 即可重新启动面板。面板与代理服务的开机自启相互独立，首次安装时默认均为启用状态。

升级早期版本的 Miniray 数据库时，程序会先在数据库同目录下创建 `miniray-before-simplification-*.db` 备份，再清理已移除的数据表。原有本地入站配置、客户端用量及兼容的订阅端点覆盖设置会被保留。

## 版本发布

项目版本统一维护在 `internal/config/version`，管理界面显示的是当前安装的程序版本，与 Xray 核心版本分开。

每次发版时，在 `docs/releases/vX.Y.Z.md` 创建该版本的中文更新说明，每项改动单独列为一条。将版本号更新为 `X.Y.Z` 后，以 `chore(release): vX.Y.Z` 为提交标题推送至 `main`，GitHub Actions 会依次执行前端检查、后端测试、Linux 双架构构建及安装验收。全部通过后，使用对应的中文说明文件创建 GitHub Release，并上传 `miniray-linux-amd64.tar.gz`、`miniray-linux-arm64.tar.gz` 和 `SHA256SUMS`，不再构建 Windows 安装包。

## Docker

Docker 部署为可选方式，参见 [deploy/README.md](deploy/README.md)。

## 源码构建

需要 Go 1.27.1、C 编译器、Node.js 26，以及 npm 11 或更新版本。

```bash
cd frontend
npm ci
npm run build
cd ..
CGO_ENABLED=1 go build -trimpath -o miniray .
```

本项目采用 [GPL-3.0](LICENSE) 许可证，基于 [3x-ui 上游 v3.9.0 源码](https://github.com/MHSanaei/3x-ui/tree/v3.9.0) 开发。
