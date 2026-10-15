"""Start a copied binary in a temporary directory; never use development config."""
import os
import shutil
import socket
import subprocess
import sys
import tempfile
from pathlib import Path

dsn = os.environ.get("DITING_FRESH_PG_DSN")
if not dsn:
    raise SystemExit("Set DITING_FRESH_PG_DSN to an isolated, freshly created database")
binary = Path(sys.argv[1]).resolve()
version = sys.argv[2]
with tempfile.TemporaryDirectory(prefix="diting-fresh-") as folder:
    target = Path(folder) / binary.name
    shutil.copy2(binary, target)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    env = {key: value for key, value in os.environ.items() if not key.startswith("ARTEX_") and key not in {"OPENAI_API_KEY", "ANTHROPIC_API_KEY"}}
    env.update(ARTEX_PG_DSN=dsn, ARTEX_CONFIG=str(Path(folder) / "unused.json"),
               DITING_SMOKE_URL=f"http://127.0.0.1:{port}", DITING_SMOKE_VERSION=version)
    with open(Path(folder) / "application.log", "w") as log:
        app = subprocess.Popen([str(target), "-addr", f"127.0.0.1:{port}"], cwd=folder, env=env, stdout=log, stderr=log)
        try:
            subprocess.run(["node", str(Path(__file__).with_name("fresh-install.mjs"))], env=env, check=True, timeout=90)
        finally:
            app.terminate()
            try:
                app.wait(timeout=10)
            except subprocess.TimeoutExpired:
                app.kill()
                app.wait()
