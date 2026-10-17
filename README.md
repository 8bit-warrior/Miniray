# Miniray

Miniray is a SQLite-based proxy management panel with local inbounds, clients, subscriptions and Xray configuration. It includes Xray-core **v26.6.27**, TUIC, AmneziaWG and MTProto support. The web panel and proxy services run independently. XDNS settings are translated to the bundled core format; options unavailable in v26.6.27 are rejected with an explicit error.

## Install on Linux

Supports **amd64/x86_64** and **arm64/aarch64** Linux hosts running systemd or Alpine Linux with OpenRC. Run as root, with `bash`, `curl`, `tar`, `sha256sum` and `install` installed. On Alpine install dependencies using `apk add bash curl tar coreutils`:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/8bit-warrior/Miniray/main/install.sh)
```

The installer downloads the latest release, verifies its SHA-256 checksum, installs Miniray and registers its services. To install a specific version:

```bash
MINIRAY_UPDATE_TAG=v0.0.1 bash <(curl -fsSL https://raw.githubusercontent.com/8bit-warrior/Miniray/main/install.sh)
```

Run `miniray` to open the management menu. Run the installation command again to update an existing installation. Existing database paths and disabled service autostart settings are preserved.

## Migrating from 3x-ui

When an existing 3x-ui installation is detected, the Linux installer offers exactly three strategies:

```text
1) Coexist: install and run Miniray alongside 3x-ui
2) Migrate: uninstall 3x-ui and import supported data into Miniray
3) Cancel this installation
Select [1/2/3]:
```

**1 — Coexist:** 3x-ui and its database remain untouched. A fresh Miniray installation starts alongside it with an independent SQLite database and isolated panel, Xray API, and metrics ports. Configured 3x-ui ports and currently occupied TCP ports are excluded. This strategy requires Python 3. Miniray has no inherited 3x-ui users or inbounds. If Miniray is already installed, its existing configuration is preserved.

**2 — Migrate:** Before uninstalling 3x-ui, the installer stops the legacy service, creates a consistent SQLite snapshot and a root-only backup under `/var/backups/miniray/`, and selectively imports supported data. 3x-ui is removed only after the new Miniray services are running; on failure, the old service and database are preserved. This strategy requires a fresh Miniray database to avoid overwriting existing Miniray data.

Supported local inbounds, clients, associations, traffic counters, compatible settings, and the administrator password hash are imported. Certificates under the legacy configuration directory are copied and their paths updated. Removed features, remote inbounds, sessions, API tokens, and two-factor state are excluded. Certificates outside that directory must remain accessible at their original paths.

**3 — Cancel:** Exits before downloading packages or modifying the installation. Invalid responses prompt again. Legacy PostgreSQL and Docker installations are not supported by the migration strategy.


## Service management

| Command | Action |
| --- | --- |
| `miniray start-panel` / `miniray stop-panel` | Start or stop the web panel and subscription listeners |
| `miniray enable-panel` / `miniray disable-panel` | Enable or disable panel autostart |
| `miniray start-core` / `miniray stop-core` | Start or stop the proxy core and protocol supervisor |
| `miniray enable-core` / `miniray disable-core` | Enable or disable proxy autostart |
| `miniray status` | Show service and autostart status |

On Alpine Linux, OpenRC units are installed under `/etc/init.d/`; environment is stored in `/etc/conf.d/miniray`. Logs are written under `/var/log/miniray/`.\n\nStopping the panel closes its listening ports while proxies keep running. Start it again with `miniray start-panel`. Panel autostart and proxy autostart are independent; both are enabled on a fresh installation.

When upgrading an earlier Miniray database, a backup named `miniray-before-simplification-*.db` is saved beside it before the removed tables are retired. Local inbound settings, client usage and compatible subscription endpoint overrides are retained.

## Windows and Docker

Download the amd64 or arm64 ZIP from [Releases](https://github.com/8bit-warrior/Miniray/releases), extract it and run `miniray.exe` from the extracted directory. The Linux installation command supports systemd and OpenRC. Docker deployment is optional; see [deploy/README.md](deploy/README.md).

## Build

Requires Go 1.27.1, a C compiler, Node.js 26 and npm 11 or newer:

```bash
cd frontend
npm ci
npm run build
cd ..
CGO_ENABLED=1 go build -trimpath -o miniray .
```

Licensed under [GPL-3.0](LICENSE). Based on the [upstream v3.9.0 source](https://github.com/MHSanaei/3x-ui/tree/v3.9.0).
