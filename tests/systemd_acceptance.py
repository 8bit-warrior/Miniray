"""Exercise release archives and the real installer on a disposable CI systemd host."""

import errno
import http.server
import json
import os
from pathlib import Path
import pty
import select
import shutil
import signal
import socketserver
import sqlite3
import subprocess
import sys
import tempfile
import threading
import time

packages = Path(sys.argv[1]).resolve()
repo = Path(sys.argv[2]).resolve()
work = Path(tempfile.mkdtemp(prefix="miniray-acceptance-"))
units = ["miniray", "miniray-agent", "miniray-core", "x-ui"]


def command(*args, check=True):
    return subprocess.run(args, check=check, capture_output=True, text=True)


def clean():
    for unit in units:
        command("systemctl", "stop", unit, check=False)
        command("systemctl", "disable", unit, check=False)
        Path(f"/etc/systemd/system/{unit}.service").unlink(missing_ok=True)
    for path in ["/usr/local/miniray", "/etc/miniray", "/usr/local/x-ui", "/etc/x-ui", "/var/backups/miniray"]:
        shutil.rmtree(path, ignore_errors=True)
    for path in ["/usr/local/bin/miniray", "/usr/bin/x-ui", "/etc/default/miniray", "/etc/default/x-ui"]:
        Path(path).unlink(missing_ok=True)
    command("systemctl", "daemon-reload")
    command("systemctl", "reset-failed", check=False)


def fixture(corrupt=False):
    Path("/etc/x-ui").mkdir()
    Path("/usr/local/x-ui").mkdir()
    Path("/usr/local/x-ui/x-ui").write_text("legacy test installation\n")
    Path("/etc/systemd/system/x-ui.service").write_text(
        "[Unit]\nDescription=Legacy installer fixture\n[Service]\nExecStart=/bin/sleep infinity\n[Install]\nWantedBy=multi-user.target\n"
    )
    if corrupt:
        Path("/etc/x-ui/x-ui.db").write_bytes(b"broken sqlite fixture")
    else:
        db = sqlite3.connect("/etc/x-ui/x-ui.db")
        db.executescript("""
            CREATE TABLE users(id INTEGER PRIMARY KEY, username TEXT, password TEXT);
            INSERT INTO users VALUES(1,'legacy-admin','$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy');
            CREATE TABLE settings(id INTEGER PRIMARY KEY, key TEXT, value TEXT);
            INSERT INTO settings(key,value) VALUES('webPort','18443'),('webBasePath','/'),('subEnable','false');
            CREATE TABLE inbounds(id INTEGER PRIMARY KEY, user_id INTEGER, tag TEXT, protocol TEXT, port INTEGER, listen TEXT, settings TEXT, stream_settings TEXT, sniffing TEXT, enable INTEGER, node_id INTEGER);
            INSERT INTO inbounds VALUES(10,1,'local-socks','mixed',24442,'127.0.0.1','{"accounts":[],"udp":true}','{"network":"tcp","security":"none"}','{}',1,NULL);
            INSERT INTO inbounds VALUES(11,1,'remote-only','mixed',24443,'127.0.0.1','{"accounts":[],"udp":true}','{"network":"tcp","security":"none"}','{}',1,9);
        """)
        template = json.loads((repo / "internal/web/service/config.json").read_text())
        # The fixture's proxy must reach its loopback HTTP test server.
        template["outbounds"].append({"protocol": "freedom", "tag": "acceptance-http", "settings": {"finalRules": [{"action": "allow", "ip": ["127.0.0.1"]}, {"action": "block"}]}})
        template["routing"]["rules"].insert(1, {"type": "field", "inboundTag": ["local-socks"], "ip": ["127.0.0.1"], "outboundTag": "acceptance-http"})
        db.execute("INSERT INTO settings(key,value) VALUES('xrayTemplateConfig',?)", (json.dumps(template),))
        db.commit()
        db.close()
    command("systemctl", "daemon-reload")
    command("systemctl", "enable", "--now", "x-ui")


def install(answers="", succeeds=True):
    pid, fd = pty.fork()
    if pid == 0:
        env = os.environ.copy()
        env["PATH"] = str(work / "bin") + ":" + env["PATH"]
        os.execvpe("bash", ["bash", str(repo / "install.sh")], env)
    os.write(fd, answers.encode())
    output = bytearray()
    deadline = time.monotonic() + 150
    try:
        while time.monotonic() < deadline:
            readable, _, _ = select.select([fd], [], [], 1)
            if not readable:
                continue
            try:
                part = os.read(fd, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    break
                raise
            if not part:
                break
            output.extend(part)
        else:
            os.kill(pid, signal.SIGKILL)
            raise AssertionError("installer timed out")
        _, status = os.waitpid(pid, 0)
        passed = os.waitstatus_to_exitcode(status) == 0
        assert passed == succeeds, output.decode(errors="replace")[-6000:]
    finally:
        os.close(fd)


def wait_active(unit):
    for _ in range(30):
        if command("systemctl", "is-active", "--quiet", unit, check=False).returncode == 0:
            return
        time.sleep(0.5)
    raise AssertionError(command("journalctl", "-u", unit, "-n", "40", "--no-pager").stdout)


def assert_schema(imported):
    db = sqlite3.connect("/etc/miniray/miniray.db")
    tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
    assert not tables.intersection({"nodes", "hosts", "client_groups", "node_client_ips", "node_client_traffics", "node_pending_resets", "client_global_traffics"}), tables
    if imported:
        assert db.execute("SELECT id,tag FROM inbounds").fetchall() == [(10, "local-socks")]
        assert db.execute("SELECT username,password FROM users").fetchone() == ("legacy-admin", "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
    else:
        assert db.execute("SELECT count(*) FROM inbounds").fetchone()[0] == 0
    db.close()


def assert_proxy():
    for _ in range(30):
        result = command("/usr/bin/curl", "--noproxy", "", "--socks5-hostname", "127.0.0.1:24442", "--max-time", "2", "-fsS", f"http://127.0.0.1:{httpd.server_address[1]}/", check=False)
        if result.returncode == 0 and result.stdout == "miniray-proxy-ok":
            return
        time.sleep(0.5)
    raise AssertionError("proxy forwarding did not recover: " + result.stderr)


class Handler(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b"miniray-proxy-ok")

    def log_message(self, *_):
        pass


(work / "bin").mkdir()
curl = work / "bin/curl"
curl.write_text("""#!/usr/bin/env python3
import os, shutil, sys
from pathlib import Path
args=sys.argv[1:]
url=args[-1]
if '/releases/' in url and url.rsplit('/',1)[-1] in {'miniray-linux-amd64.tar.gz','SHA256SUMS'}:
    shutil.copyfile(Path(os.environ['MINIRAY_TEST_PACKAGES']) / url.rsplit('/',1)[-1], args[args.index('-o')+1])
else:
    os.execv('/usr/bin/curl',['/usr/bin/curl',*args])
""")
curl.chmod(0o755)
os.environ["MINIRAY_TEST_PACKAGES"] = str(packages)
httpd = socketserver.TCPServer(("127.0.0.1", 0), Handler)
threading.Thread(target=httpd.serve_forever, daemon=True).start()

try:
    for name, answers, remove, imported in [
        ("fresh", "", True, False),
        ("remove-import", "Y\ny\n", True, True),
        ("remove-only", "y\nN\n", True, False),
        ("keep-import", "n\nY\n", False, True),
        ("keep-only", "N\nn\n", False, False),
    ]:
        clean()
        if name != "fresh":
            fixture()
        install(answers)
        assert_schema(imported)
        if remove:
            wait_active("miniray")
            wait_active("miniray-agent")
            if name != "fresh":
                assert not Path("/usr/local/x-ui").exists()
                assert list(Path("/var/backups/miniray").glob("**/*.db"))
        else:
            wait_active("x-ui")
            for unit in units[:3]:
                assert command("systemctl", "is-active", "--quiet", unit, check=False).returncode != 0
                assert command("systemctl", "is-enabled", "--quiet", unit, check=False).returncode != 0
        if name == "remove-import":
            wait_active("miniray-core")
            assert_proxy()
            command("/usr/local/bin/miniray", "disable-panel")
            command("/usr/local/bin/miniray", "stop-panel")
            assert_proxy()
            assert command("systemctl", "is-enabled", "--quiet", "miniray-core").returncode == 0
            assert command("systemctl", "is-active", "--quiet", "miniray", check=False).returncode != 0
            # Keep a real executable open while the installer replaces its path.
            mtg_path = Path("/usr/local/miniray/bin/mtg-linux-amd64")
            shutil.copyfile("/bin/sleep", mtg_path)
            sidecar = subprocess.Popen([str(mtg_path), "120"])
            try:
                install()
                assert sidecar.poll() is None, "upgrade terminated an existing sidecar"
            finally:
                sidecar.terminate()
                sidecar.wait()
            assert command("systemctl", "is-enabled", "--quiet", "miniray", check=False).returncode != 0
            assert_proxy()
            command("/usr/local/bin/miniray", "start-panel")
            wait_active("miniray")
            old_pid = command("systemctl", "show", "miniray-core", "-p", "MainPID", "--value").stdout.strip()
            command("systemctl", "kill", "--signal=SIGKILL", "miniray-core")
            for _ in range(30):
                pid = command("systemctl", "show", "miniray-core", "-p", "MainPID", "--value").stdout.strip()
                if pid not in {"0", old_pid}:
                    break
                time.sleep(0.5)
            assert pid not in {"0", old_pid}, "core did not restart after SIGKILL"
            assert_proxy()
            wait_active("miniray-core")
        print(name + ": passed", flush=True)
    clean()
    fixture(corrupt=True)
    install("y\nY\n", succeeds=False)
    wait_active("x-ui")
    assert Path("/usr/local/x-ui/x-ui").exists()
    assert Path("/etc/x-ui/x-ui.db").read_bytes() == b"broken sqlite fixture"
    print("corrupt-source-preserves-legacy: passed", flush=True)
finally:
    clean()
    httpd.shutdown()
    shutil.rmtree(work)
