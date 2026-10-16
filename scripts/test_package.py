import importlib.util
import stat
import tempfile
import unittest
import zipfile
from pathlib import Path

spec = importlib.util.spec_from_file_location("check_package", Path(__file__).with_name("check-package.py"))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


class PackageTests(unittest.TestCase):
    def package(self, extra=None):
        folder = tempfile.TemporaryDirectory()
        self.addCleanup(folder.cleanup)
        path = Path(folder.name) / "release.zip"
        files = {
            "artex.exe": b"fixture", "README.md": b"readme", "LICENSE": b"license",
            "THIRD_PARTY_NOTICES.md": b"notices", "config.example.json": b'{"database":{"password":""}}',
            "screenshots/dashboard.jpg": b"screenshot", "licenses/font.txt": b"license",
        }
        files.update(extra or {})
        with zipfile.ZipFile(path, "w") as archive:
            for name, data in files.items():
                archive.writestr("release/" + name, data)
        return path

    def test_clean_distribution(self):
        checker.validate(self.package())

    def test_private_data_rejected(self):
        for name in ["config.json", "data/tasks.json", ".env.local", "auth.key", "secrets.key", "config.json.before-setup-123.backup", "database.dump", "snapshot.dbak", "state.sqlite3", "state.db-wal", "stdout.log", "recording.har", "private.pfx", "scheduler_state.json", "UI_ACCEPTANCE.md", "skills/demo/validation-test.json", "../outside"]:
            with self.subTest(name=name), self.assertRaises(ValueError):
                checker.validate(self.package({name: b"private"}))

    def test_archive_links_rejected(self):
        path = self.package()
        with zipfile.ZipFile(path, "a") as archive:
            link = zipfile.ZipInfo("release/skills/link.txt")
            link.create_system = 3
            link.external_attr = (stat.S_IFLNK | 0o777) << 16
            archive.writestr(link, "../../config.json")
        with self.assertRaises(ValueError):
            checker.validate(path)

    def test_template_credentials_rejected(self):
        with self.assertRaises(ValueError):
            checker.validate(self.package({"config.example.json": b'{"database":{"password":"secret"}}'}))

    def test_blank_docker_template_allowed_but_secrets_rejected(self):
        template = b'POSTGRES_PASSWORD=\nDITING_IMAGE=diting:2.5.0-local\nDITING_PORT=8787\nDITING_PROXY_PORT=8788\n'
        checker.validate(self.package({'.env.example': template}))
        for altered in (template.replace(b'POSTGRES_PASSWORD=', b'POSTGRES_PASSWORD=secret'), template+b'API_KEY=private\n', template+b'POSTGRES_PASSWORD=\n'):
            with self.assertRaises(ValueError):
                checker.validate(self.package({'.env.example': altered}))


if __name__ == "__main__":
    unittest.main()
