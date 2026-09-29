#!/usr/bin/env python3
"""校验源码版本，打包 Windows GUI / CLI 和第三方许可；不运行设备查询。"""
from pathlib import Path
import argparse
import hashlib
import json
import os
import re
import shutil
import zipfile


def release_version(root, tag):
    source = (root / "go" / "main.go").read_text(encoding="utf-8")
    versions = re.findall(r'^const version = "([^"\r\n]+)"$', source, re.MULTILINE)
    if len(versions) != 1:
        raise ValueError("go/main.go 必须包含唯一的 const version 字符串")
    version = versions[0]
    number = r"(?:0|[1-9][0-9]*)"
    if not re.fullmatch(rf"{number}\.{number}\.{number}(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?", version):
        raise ValueError("版本格式无效：需要主版本.次版本.修订版本，可附加预发布后缀")
    frontend = json.loads((root / "frontend" / "package.json").read_text(encoding="utf-8"))
    if frontend.get("version") != version:
        raise ValueError("Go 与 frontend/package.json 版本不一致")
    if tag and tag != "v" + version:
        raise ValueError(f"标签与源码版本不一致：期望 v{version}")
    return version


def package(root, output, version):
    notices = root / "docs" / "THIRD_PARTY_NOTICES.txt"
    bundles = {
        "MouseControl": {
            "MouseControl.exe": root / "dist" / "MouseControl.exe",
            "使用说明.txt": root / "docs" / "GUI_USAGE.txt",
            "THIRD_PARTY_NOTICES.txt": notices,
        },
        "RazerBattery": {
            "RazerBattery.exe": root / "dist" / "RazerBattery.exe",
            "CLI_HELP.txt": root / "dist" / "CLI_HELP.txt",
            "THIRD_PARTY_NOTICES.txt": notices,
        },
    }
    # 先检查全部输入，避免缺少 CLI 或许可时留下看似完整的 GUI 发布包。
    for files in bundles.values():
        for path in files.values():
            if not path.is_file() or path.stat().st_size == 0:
                raise ValueError(f"缺少打包文件或文件为空：{path}")
    if output.exists() and any(output.iterdir()):
        raise ValueError(f"输出目录非空，请使用新的空目录，避免混入旧版本：{output}")
    output.mkdir(parents=True, exist_ok=True)
    assets = []
    for name, files in bundles.items():
        prefix = f"{name}-{version}/"
        archive = output / f"{name}-{version}-windows-x64.zip"
        with zipfile.ZipFile(archive, "x", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as stream:
            for filename, path in files.items():
                stream.write(path, prefix + filename)
        with zipfile.ZipFile(archive) as stream:
            if stream.testzip() is not None:
                raise ValueError(f"ZIP 完整性检查失败：{archive.name}")
            for filename, path in files.items():
                if stream.read(prefix + filename) != path.read_bytes():
                    raise ValueError(f"ZIP 内容不一致：{archive.name}/{filename}")
        assets.append(archive)
    notice_copy = output / notices.name
    shutil.copyfile(notices, notice_copy)
    assets.append(notice_copy)
    sums = "".join(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n" for path in sorted(assets))
    (output / "SHA256SUMS.txt").write_bytes(sums.encode("utf-8"))
    return assets


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("version", "package"))
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parent.parent)
    parser.add_argument("--output", type=Path, help="发布包输出目录，必须为空")
    parser.add_argument("--tag", default=os.environ.get("RELEASE_TAG", ""), help="发布标签，默认读取 RELEASE_TAG")
    args = parser.parse_args()
    try:
        version = release_version(args.root, args.tag)
        if args.command == "version":
            print(version)
        else:
            output = args.output or args.root / "dist" / "release"
            for path in package(args.root, output, version):
                print(f"{path.name}（{path.stat().st_size:,} 字节）")
            print(f"校验和：{output / 'SHA256SUMS.txt'}")
    except (OSError, ValueError, zipfile.BadZipFile) as error:
        raise SystemExit(str(error)) from error


if __name__ == "__main__":
    main()
