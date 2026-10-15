"""Exercise database bootstrap -> password -> model using an isolated new database.

No browser state, real provider request, production config or existing binary is used.
"""
import json
import os
import re
import secrets
import shutil
import socket
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

dsn = urllib.parse.urlsplit(os.environ.get("DITING_SETUP_PG_DSN", ""))
if dsn.scheme not in {"postgres", "postgresql"} or dsn.hostname not in {"localhost", "127.0.0.1"}:
    raise SystemExit("Set DITING_SETUP_PG_DSN to an explicit local PostgreSQL test database")
database_name = urllib.parse.unquote(dsn.path.removeprefix("/"))
if not database_name.startswith("diting_setup_"):
    raise SystemExit("Use a newly created database named diting_setup_<unique suffix>")
database = {
    "host": dsn.hostname, "port": dsn.port or 5432,
    "user": urllib.parse.unquote(dsn.username or ""),
    "password": urllib.parse.unquote(dsn.password or ""),
    "dbname": database_name,
    "sslmode": urllib.parse.parse_qs(dsn.query).get("sslmode", ["disable"])[0],
}
binary = Path(sys.argv[1]).resolve()

with tempfile.TemporaryDirectory(prefix="diting-setup-") as folder:
    root = Path(folder)
    target = root / binary.name
    shutil.copy2(binary, target)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    base = f"http://127.0.0.1:{port}"
    env = {key: value for key, value in os.environ.items() if not key.startswith("ARTEX_") and key not in {"OPENAI_API_KEY", "ANTHROPIC_API_KEY"}}
    env["ARTEX_CONFIG"] = str(root / "config.json")

    def request(path, body=None, headers=None, expected=200):
        req = urllib.request.Request(base + path, data=None if body is None else json.dumps(body).encode(), headers={"Content-Type": "application/json", **(headers or {})})
        try:
            with urllib.request.urlopen(req, timeout=15) as response:
                assert response.status == expected, path
                return response.read()
        except urllib.error.HTTPError as error:
            if error.code != expected:
                raise AssertionError(f"Unexpected status {error.code}: {path}") from None
            return error.read()

    with open(root / "application.log", "w", encoding="utf-8") as log:
        app = subprocess.Popen([str(target), "-addr", f"127.0.0.1:{port}", "-proxy", ""], cwd=root, env=env, stdout=log, stderr=log)
        try:
            deadline = time.monotonic() + 30
            while True:
                try:
                    page = request("/").decode()
                    break
                except (OSError, AssertionError):
                    if app.poll() is not None or time.monotonic() > deadline:
                        raise AssertionError("Bootstrap did not become available") from None
                    time.sleep(.1)
            match = re.search(r'const token="([a-f0-9]{64})"', page)
            assert match, "Bootstrap page missing"
            setup_headers = {"X-Diting-Setup": match[1]}
            assert not (root / "config.json").exists()
            request("/api/bootstrap/save", database, expected=403)
            status = json.loads(request("/api/bootstrap/status", headers=setup_headers))
            assert not status["environment_dsn"]
            assert all(c["ok"] for c in status["environment"]["checks"] if c["required"])
            bad_database = {**database, "dbname": "diting_missing_setup_fixture"}
            request("/api/bootstrap/check", bad_database, setup_headers, expected=422)
            assert not (root / "config.json").exists(), "Failed check wrote config"
            request("/api/bootstrap/check", database, setup_headers)
            assert not (root / "config.json").exists(), "Test-only request wrote config"
            request("/api/bootstrap/save", database, setup_headers)
            deadline = time.monotonic() + 60
            while True:
                try:
                    initialized = json.loads(request("/api/auth/status"))["initialized"]
                    break
                except (OSError, AssertionError, ValueError, KeyError):
                    if app.poll() is not None or time.monotonic() > deadline:
                        raise AssertionError("Application did not start after setup") from None
                    time.sleep(.1)
            assert not initialized, "Refusing to initialize an existing installation"
            assert app.poll() is None, "Startup replaced or exited the process"
            request("/api/system/environment", expected=401)
            password = secrets.token_hex(24)
            session = json.loads(request("/api/auth/init", {"password": password}))["token"]
            headers = {"Authorization": "Bearer " + session}
            ready = json.loads(request("/api/system/environment", headers=headers))
            assert ready["credential_key_ready"]
            assert (root / "secrets.key").stat().st_size == 32
            assert not (root / "data" / "secrets.key").exists()
            original_key = (root / "secrets.key").read_bytes()
            replacement = b"x" * 32
            try:
                (root / "secrets.key").write_bytes(replacement)
                wrong_key_status = json.loads(request("/api/system/environment", headers=headers))
                assert not wrong_key_status["credential_key_ready"], "Wrong disk key reported ready"
                assert (root / "secrets.key").read_bytes() == replacement, "Health check rewrote key"
            finally:
                (root / "secrets.key").write_bytes(original_key)
            assert json.loads(request("/api/system/environment", headers=headers))["credential_key_ready"]
            for route, key in [("/api/tasks", "tasks"), ("/api/assets", "assets"), ("/api/conversations", "conversations"), ("/api/companies", "companies"), ("/api/exploration/findings", "findings"), ("/api/llm/profiles", "profiles")]:
                value = json.loads(request(route, headers=headers))
                items = value if value is None or isinstance(value, list) else value.get(key)
                assert not items, f"Existing data: {route}"
            assert not json.loads(request("/api/llm", headers=headers))["configured"]
            profile = json.loads(request("/api/llm/profiles", {"name": "setup fixture", "format": "openai", "model": "fixture-only", "api_key": "fixture-setup-secret", "base_url": "http://127.0.0.1:1/v1", "streaming": True}, headers))
            request("/api/llm/profiles/active", {"id": profile["id"]}, headers)
            assert json.loads(request("/api/llm", headers=headers))["configured"]
            assert len(json.loads(request("/api/llm/profiles", headers=headers))["profiles"]) == 1
            request("/system/setup/")
            print("Setup installation passed: bootstrap, failed/successful connection checks, same-process startup, password initialization, environment auth, disk key mismatch/recovery, blank business state and model save/activation; no provider test invoked")
        finally:
            app.terminate()
            try:
                app.wait(timeout=10)
            except subprocess.TimeoutExpired:
                app.kill()
                app.wait()
