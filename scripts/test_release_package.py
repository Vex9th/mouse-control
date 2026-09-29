"""发布包的版本、内容及校验和回归；不运行程序或连接鼠标。"""
from pathlib import Path
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
import zipfile


SCRIPT = Path(__file__).with_name("release-package.py")


class ReleasePackageTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.files = {
            "go/main.go": b'package main\nconst version = "1.4.2"\n',
            "frontend/package.json": b'{"version":"1.4.2"}',
            "dist/MouseControl.exe": b"MZ GUI fixture",
            "dist/RazerBattery.exe": b"MZ CLI fixture",
            "dist/CLI_HELP.txt": "命令行帮助\n".encode(),
            "docs/GUI_USAGE.txt": "桌面版使用说明\n".encode(),
            "docs/THIRD_PARTY_NOTICES.txt": b"Third-party notices\n",
        }
        for name, data in self.files.items():
            path = self.root / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)

    def run_script(self, command="package", tag="v1.4.2"):
        env = dict(os.environ, RELEASE_TAG=tag, PYTHONUTF8="1")
        return subprocess.run(
            [sys.executable, str(SCRIPT), command, "--root", str(self.root)],
            env=env, capture_output=True, text=True, encoding="utf-8",
        )

    def test_packages_have_expected_version_contents_and_checksums(self):
        result = self.run_script()
        self.assertEqual(result.returncode, 0, result.stderr)
        output = self.root / "dist" / "release"
        expected = {
            "MouseControl-1.4.2-windows-x64.zip": {
                "MouseControl.exe": "dist/MouseControl.exe",
                "使用说明.txt": "docs/GUI_USAGE.txt",
            },
            "RazerBattery-1.4.2-windows-x64.zip": {
                "RazerBattery.exe": "dist/RazerBattery.exe",
                "CLI_HELP.txt": "dist/CLI_HELP.txt",
            },
        }
        for filename, contents in expected.items():
            prefix = filename.removesuffix("-windows-x64.zip") + "/"
            contents["THIRD_PARTY_NOTICES.txt"] = "docs/THIRD_PARTY_NOTICES.txt"
            with zipfile.ZipFile(output / filename) as archive:
                self.assertIsNone(archive.testzip())
                self.assertEqual(set(archive.namelist()), {prefix + name for name in contents})
                for name, source in contents.items():
                    self.assertEqual(archive.read(prefix + name), self.files[source])
        notices = output / "THIRD_PARTY_NOTICES.txt"
        self.assertEqual(notices.read_bytes(), self.files["docs/THIRD_PARTY_NOTICES.txt"])
        checksums = (output / "SHA256SUMS.txt").read_text().splitlines()
        self.assertEqual(len(checksums), 3)
        for line in checksums:
            digest, name = line.split("  ")
            self.assertEqual(digest, hashlib.sha256((output / name).read_bytes()).hexdigest())
        self.assertEqual({p.name for p in output.iterdir()}, set(expected) | {"THIRD_PARTY_NOTICES.txt", "SHA256SUMS.txt"})

    def test_mismatched_or_unsafe_tag_does_not_create_packages(self):
        for tag in ("v1.4.3", "1.4.2", "v1.4.2/extra", "v1.4.2\nother=unsafe"):
            with self.subTest(tag=tag):
                result = self.run_script(tag=tag)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("标签", result.stderr)
                self.assertFalse((self.root / "dist" / "release").exists())

    def test_frontend_and_go_versions_must_match(self):
        (self.root / "frontend" / "package.json").write_text(json.dumps({"version": "1.4.3"}))
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("版本不一致", result.stderr)
        self.assertFalse((self.root / "dist" / "release").exists())

    def test_branch_build_accepts_no_tag(self):
        result = self.run_script(command="version", tag="")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stdout.strip(), "1.4.2")

    def test_missing_input_does_not_leave_partial_packages(self):
        (self.root / "dist" / "RazerBattery.exe").unlink()
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("RazerBattery.exe", result.stderr)
        self.assertFalse((self.root / "dist" / "release").exists())

    def test_existing_output_is_not_overwritten_or_mixed_with_new_release(self):
        output = self.root / "dist" / "release"
        output.mkdir()
        previous = output / "old.zip"
        previous.write_bytes(b"previous artifact")
        result = self.run_script()
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("非空", result.stderr)
        self.assertEqual(list(output.iterdir()), [previous])
        self.assertEqual(previous.read_bytes(), b"previous artifact")

    def test_invalid_source_version_cannot_be_used_as_path(self):
        (self.root / "go" / "main.go").write_text('const version = "../../escape"\n')
        (self.root / "frontend" / "package.json").write_text('{"version":"../../escape"}')
        result = self.run_script(tag="")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("版本格式", result.stderr)


if __name__ == "__main__":
    unittest.main()
