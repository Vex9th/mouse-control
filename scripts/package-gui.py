#!/usr/bin/env python3
"""将已经验证的 GUI exe、说明与第三方许可打成便携 ZIP。"""
from pathlib import Path
import hashlib
import zipfile
import subprocess
import sys


def main():
    root = Path(__file__).resolve().parent.parent
    dist = root / "dist"
    version = subprocess.check_output([sys.executable, str(root / "scripts/release-package.py"), "version"], cwd=root, text=True).strip()
    files = {
        "MouseControl.exe": dist / "MouseControl.exe",
        "使用说明.txt": root / "docs" / "GUI_USAGE.txt",
        "THIRD_PARTY_NOTICES.txt": root / "docs" / "THIRD_PARTY_NOTICES.txt",
    }
    for path in files.values():
        if not path.is_file():
            raise SystemExit(f"缺少打包文件：{path}")
    archive = dist / f"MouseControl-{version}-windows-x64.zip"
    with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as output:
        for name, path in files.items():
            output.write(path, f"MouseControl-{version}/" + name)
    with zipfile.ZipFile(archive) as output:
        if output.testzip() is not None:
            raise SystemExit("ZIP 完整性检查失败")
        for name, path in files.items():
            if output.read(f"MouseControl-{version}/" + name) != path.read_bytes():
                raise SystemExit(f"ZIP 内容不一致：{name}")
    for path in [files["MouseControl.exe"], archive]:
        print(f"{path.name}: {path.stat().st_size:,} 字节；SHA256 {hashlib.sha256(path.read_bytes()).hexdigest()}")


if __name__ == "__main__":
    main()
