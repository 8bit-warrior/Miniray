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
