# Cloud deployment (unattended install)

Tooling to ship the miniray panel via unattended install, with **per-instance
credentials generated on first boot** (never `admin/admin`, never a shared
session secret). Works on amd64 and arm64.

| Path | What it is | Use when |
| --- | --- | --- |
| [`cloud-init/`](cloud-init/) | Generic cloud-init user-data (unattended `install.sh`) | Any cloud, no image build |
| [`marketplace/hetzner/`](marketplace/hetzner/) | Hetzner Cloud notes | Hetzner deployments |
| [`test/`](test/) | Container smoke test | Verifying the install path |

## How it works

`install.sh` runs unattended when `MINIRAY_NONINTERACTIVE=1` or stdin is not a TTY.
Each instance installs and configures itself with random credentials. See
[`cloud-init/README.md`](cloud-init/README.md).

## Unattended install knobs

`install.sh` reads these env vars in non-interactive mode (all optional; unset ⇒
secure random / default):

`MINIRAY_USERNAME`, `MINIRAY_PASSWORD`, `MINIRAY_PANEL_PORT`, `MINIRAY_WEB_BASE_PATH`,
`MINIRAY_SSL_MODE` (`none`|`ip`|`domain`, default `none`), `MINIRAY_DOMAIN`,
`MINIRAY_ACME_EMAIL`, `MINIRAY_ACME_HTTP_PORT` (ACME HTTP-01 listener port, default `80`),
`MINIRAY_SSL_IPV6` (optional IPv6 address to add to an `ip`-mode cert),
`MINIRAY_SERVER_IP` (fallback IP for the displayed access URL when auto-detection fails),
`MINIRAY_DB_TYPE` (`sqlite`|`postgres`), `MINIRAY_DB_DSN`.

The resulting credentials are written to `/etc/miniray/install-result.env` (mode 600).
