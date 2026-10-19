"""Choose isolated Miniray listeners when installing alongside an existing legacy panel."""

import errno
import json
import secrets
import socket
import sqlite3
import sys
from pathlib import Path


def legacy_ports(path):
    reserved = set()
    if not Path(path).is_file():
        return reserved
    try:
        with sqlite3.connect(Path(path).resolve().as_uri() + "?mode=ro", uri=True) as db:
            for (port,) in db.execute("SELECT port FROM inbounds"):
                if isinstance(port, int):
                    reserved.add(port)
            settings = dict(db.execute(
                "SELECT key,value FROM settings WHERE key IN "
                "('webPort','subPort','xrayTemplateConfig')"
            ))
            for key in ("webPort", "subPort"):
                if str(settings.get(key, "")).isdigit():
                    reserved.add(int(settings[key]))
            if "xrayTemplateConfig" in settings:
                cfg = json.loads(settings["xrayTemplateConfig"])
                for inbound in cfg.get("inbounds", []):
                    if isinstance(inbound.get("port"), int):
                        reserved.add(inbound["port"])
                listen = cfg.get("metrics", {}).get("listen", "")
                if listen.rsplit(":", 1)[-1].isdigit():
                    reserved.add(int(listen.rsplit(":", 1)[-1]))
    except (sqlite3.Error, ValueError, TypeError):
        print("Cannot read legacy configured ports; probing current listeners instead.")
    return reserved


def is_free(port):
    sockets = []
    try:
        sock4 = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sockets.append(sock4)
        sock4.bind(("0.0.0.0", port))
        try:
            sock6 = socket.socket(socket.AF_INET6, socket.SOCK_STREAM)
            sockets.append(sock6)
            sock6.setsockopt(socket.IPPROTO_IPV6, socket.IPV6_V6ONLY, 1)
            sock6.bind(("::", port))
        except OSError as exc:
            if exc.errno not in (errno.EAFNOSUPPORT, errno.EPROTONOSUPPORT, errno.EADDRNOTAVAIL):
                return False
        return True
    except OSError:
        return False
    finally:
        for sock in sockets:
            sock.close()


def allocate(preferred, reserved):
    if preferred not in reserved and is_free(preferred):
        reserved.add(preferred)
        return preferred
    for _ in range(4096):
        port = 20000 + secrets.randbelow(45536)
        if port not in reserved and is_free(port):
            reserved.add(port)
            return port
    raise RuntimeError("No available Miniray coexistence listener port")


def main(db_path, template_path, legacy_path):
    reserved = legacy_ports(legacy_path)
    with sqlite3.connect(db_path) as db:
        (panel_port,) = db.execute("SELECT value FROM settings WHERE key='webPort'").fetchone()
    panel, api, metrics = (allocate(port, reserved) for port in (int(panel_port), 62790, 11112))
    config = json.loads(Path(template_path).read_text(encoding="utf-8"))
    for inbound in config["inbounds"]:
        if inbound.get("tag") == "api":
            inbound["port"] = api
    config["metrics"]["listen"] = f"127.0.0.1:{metrics}"
    with sqlite3.connect(db_path) as db:
        db.execute("DELETE FROM settings WHERE key IN ('webPort','xrayTemplateConfig')")
        db.executemany("INSERT INTO settings(key,value) VALUES(?,?)", (
            ("webPort", str(panel)),
            ("xrayTemplateConfig", json.dumps(config, separators=(",", ":"))),
        ))
    print(f"Coexistence: Miniray panel port {panel}, core API port {api}, metrics port {metrics}")


if __name__ == "__main__":
    main(*sys.argv[1:])
