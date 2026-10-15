"""Validate a distribution archive without unpacking or starting it."""
import json
import re
import stat
import sys
import zipfile
from pathlib import PurePosixPath


def validate(path):
    with zipfile.ZipFile(path) as archive:
        files = [entry for entry in archive.infolist() if not entry.is_dir()]
        roots = {PurePosixPath(entry.filename).parts[0] for entry in files}
        if len(roots) != 1:
            raise ValueError("Archive must contain exactly one distribution directory")
        names = set()
        for entry in files:
            if stat.S_ISLNK(entry.external_attr >> 16):
                raise ValueError(f"Archive link is not allowed: {entry.filename}")
            name = entry.filename.replace("\\", "/")
            parts = PurePosixPath(name).parts
            if name.startswith("/") or ".." in parts or ":" in name:
                raise ValueError(f"Unsafe archive path: {name}")
            relative = "/".join(parts[1:])
            lower = relative.lower()
            if lower in names:
                raise ValueError(f"Duplicate archive path: {relative}")
            names.add(lower)
            if any(part.lower() in {"data", "logs", "backups", "node_modules", ".git", "mock"} for part in parts[1:]):
                raise ValueError(f"Runtime or development directory: {relative}")
            if re.search(r"(^|/)(config\.json|scheduler_state\.json|\.env(?:\..*)?|.*\.(?:key|pem|p12|pfx|db|sqlite|sqlite3|db-wal|db-shm|dump|backup|dbak|har|pcap|pcapng|log|pid)|handover\.md|implementation\.md|ui_acceptance\.md|validation-.*\.json)$", lower):
                raise ValueError(f"Private runtime file: {relative}")
            if lower == "config.example.json":
                config = json.loads(archive.read(entry))
                database = config.get("database", {})
                if database.get("password") or database.get("dsn") or config.get("skill_dir"):
                    raise ValueError("Configuration template contains credentials or a machine path")
        for required in {"config.example.json", "readme.md", "license", "third_party_notices.md"}:
            if required not in names:
                raise ValueError(f"Missing required file: {required}")
        if not {"artex", "artex.exe"}.intersection(names):
            raise ValueError("Missing application binary")
        if not any(name.startswith("screenshots/") for name in names):
            raise ValueError("Missing README screenshots")
        if not any(name.startswith("licenses/") for name in names):
            raise ValueError("Missing bundled licenses")
    print(f"Package checks passed: {len(files)} files")


if __name__ == "__main__":
    validate(sys.argv[1])
