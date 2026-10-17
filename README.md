# Miniray

Miniray is a SQLite-based proxy management panel with local inbounds, clients, subscriptions and Xray configuration. It includes Xray-core **v26.6.27**, TUIC, AmneziaWG and MTProto support. The web panel and proxy services run independently. XDNS settings are translated to the bundled core format; options unavailable in v26.6.27 are rejected with an explicit error.

## Install on Linux

Supports **amd64/x86_64** and **arm64/aarch64** Linux hosts running systemd. Run as root, with `curl`, `tar` and `sha256sum` installed:

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/8bit-warrior/Miniray/main/install.sh)
```

The installer downloads the latest release, verifies its SHA-256 checksum, installs Miniray and registers its services. To install a specific version:

```bash
MINIRAY_UPDATE_TAG=v0.0.1 bash <(curl -fsSL https://raw.githubusercontent.com/8bit-warrior/Miniray/main/install.sh)
```

Run `miniray` to open the management menu. Run the installation command again to update an existing installation. Existing database paths and disabled service autostart settings are preserved.

## Migrating from 3x-ui

On a first installation, the installer detects an existing systemd installation and asks two separate questions:

```text
Uninstall 3x-ui before installing Miniray? [y/n]:
Import supported 3x-ui configuration into Miniray? [y/n]:
```

Both answers accept uppercase or lowercase. Before removing the old installation, Miniray creates a consistent SQLite snapshot and a root-only backup under `/var/backups/miniray/`. It installs and imports successfully before deleting the old panel; an import failure preserves the old installation and restores its service.

Supported local inbounds, clients, associations, traffic counters, compatible settings and the administrator password hash are imported into a new Miniray database. Certificates beneath the old configuration directory are preserved and their paths are updated. Removed features, remote inbounds, sessions, API tokens and two-factor state are excluded. Certificates outside the old configuration directory must remain available at their original paths.

If the old installation is kept, Miniray remains stopped with autostart disabled until port conflicts are resolved. Imports require a fresh Miniray database. Legacy PostgreSQL and Docker installations require separate migration.

## Service management

| Command | Action |
| --- | --- |
| `miniray start-panel` / `miniray stop-panel` | Start or stop the web panel and subscription listeners |
| `miniray enable-panel` / `miniray disable-panel` | Enable or disable panel autostart |
| `miniray start-core` / `miniray stop-core` | Start or stop the proxy core and protocol supervisor |
| `miniray enable-core` / `miniray disable-core` | Enable or disable proxy autostart |
| `miniray status` | Show service and autostart status |

Stopping the panel closes its listening ports while proxies keep running. Start it again with `miniray start-panel`. Panel autostart and proxy autostart are independent; both are enabled on a fresh installation.

When upgrading an earlier Miniray database, a backup named `miniray-before-simplification-*.db` is saved beside it before the removed tables are retired. Local inbound settings, client usage and compatible subscription endpoint overrides are retained.

## Windows and Docker

Download the amd64 or arm64 ZIP from [Releases](https://github.com/8bit-warrior/Miniray/releases), extract it and run `miniray.exe` from the extracted directory. The Linux installation command requires systemd. Docker deployment is optional; see [deploy/README.md](deploy/README.md).

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
