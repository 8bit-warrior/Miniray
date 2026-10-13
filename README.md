# Miniray

Miniray is a panel based on the GPL-licensed v3.9.0 upstream project, undergoing a standalone-core and SQLite-only refactor.

## Operational lifecycle (Linux systemd)

The web panel, Xray core, and additional proxy protocol supervisor are independent systemd services. The **panel offline** setting is intended to stop only the HTTP interface and subscription listeners. The Xray service remains running. After going offline, `miniray start-panel` starts the panel again. This does not disable normal system boot start.

- `miniray start-panel` / `miniray stop-panel`: independently control the web panel.
- `miniray start-core` / `miniray stop-core`: explicitly control proxy traffic.
- `miniray enable-panel` / `miniray disable-panel`: control panel boot autostart without changing proxy boot autostart.
- `miniray enable-core` / `miniray disable-core`: independently control the proxy core and protocol supervisor boot settings.
- `miniray status`: show each service and its boot autostart state.

**Work in progress:** full Linux/Windows tests and a successful CI build are required before publishing `v0.0.1`. Do not deploy this in production yet.

Licensed under the included [LICENSE](LICENSE). The upstream source tag is `v3.9.0`.

## Migrating an existing 3x-ui installation (Linux)

The Linux installer detects a systemd-managed 3x-ui installation and asks two separate case-insensitive `y/n` questions: whether to uninstall it, and whether to import its supported configuration. The installer does not uninstall anything without an explicit `y`. Existing Miniray installations are updated without prompting for another migration.

For SQLite, the installer creates a consistent snapshot of `/etc/x-ui/x-ui.db` (or the configured `XUI_DB_FOLDER`) before removing the old installation. If uninstalling, it retains a root-only permanent backup under `/var/backups/miniray/`, including the old panel's configuration directory and environment file. When importing, the new Miniray binary reads the temporary snapshot **after installation** and creates a separate SQLite database. The original database is never mounted as Miniray's active database.

Imported: supported local inbounds, associated clients and usage counters, inbound fallbacks and compatible panel, subscription and Xray settings. One legacy administrator's username/password hash is transferred, but existing sessions, API tokens, two-factor state, node/group/host records and unsupported protocols are excluded. References to certificates previously stored beneath `/etc/x-ui/` are rebased to preserved files beneath `/etc/miniray/imported-legacy-files/` when uninstalling the old panel. Certificate paths outside that directory must still exist on the target host.

If the user keeps 3x-ui installed, Miniray remains stopped and boot-disabled to avoid conflicting listeners; start and enable Miniray only after resolving those conflicts. Imports require a fresh Miniray database. PostgreSQL legacy databases are not supported and will not be removed automatically as part of migration. The installer operates on host systemd services; Docker-based legacy installations require a separate migration procedure.
