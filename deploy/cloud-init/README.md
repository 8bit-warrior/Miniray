# Cloud-init

Paste [cloud-init.yaml](cloud-init.yaml) into the user-data field of a fresh Debian or Ubuntu instance. It installs `curl` and CA certificates, downloads the installer and installs the latest release.

The panel listens on port 2053. Read the generated administrator password over SSH:

```bash
sudo /usr/local/miniray/miniray initial-password
```

Use the management menu or settings to configure the panel address, TLS and credentials. The legacy import prompts require an interactive terminal; cloud-init is intended for fresh hosts.
