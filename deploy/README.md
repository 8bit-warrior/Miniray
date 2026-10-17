# Deployment

The [root README](../README.md) contains the Linux installation command and service controls. The installer requires systemd and supports Linux amd64 and arm64. Storage is SQLite only.

## Cloud-init

Use [cloud-init/cloud-init.yaml](cloud-init/cloud-init.yaml) on a fresh Debian or Ubuntu host. The initial administrator is `admin`; read its generated password with `sudo /usr/local/miniray/miniray initial-password`. A first install with an existing legacy panel needs an interactive terminal for migration choices.

The installer accepts `MINIRAY_UPDATE_TAG`, `MINIRAY_MAIN_FOLDER` and `MINIRAY_DB_FOLDER`. A custom database folder is persisted in `/etc/default/miniray`.

## Docker

Build and start the optional Docker image with `docker compose up -d --build` from the repository root. Persistent data is mounted from `db/`; certificates and ACME state use the mounts in `docker-compose.yml`. Publish each proxy's configured TCP or UDP port in addition to the panel port.

The Docker image runs the panel and its proxy processes within one container. Independent systemd service management applies to the Linux host installation.

## Acceptance

The release workflow tests the built Linux archive on a disposable native systemd runner. It covers fresh installation, all four legacy uninstall/import choices, corrupt-source preservation, upgrade with panel autostart disabled, proxy forwarding while the panel is stopped and core recovery after a forced process failure.
