#!/usr/bin/env python3
"""在项目内构建 Vue 单页和 Windows x64 单文件；不安装全局依赖。"""
from pathlib import Path
import argparse
import os
import shutil
import subprocess


def main():
    root = Path(__file__).resolve().parent.parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, default=root / "dist" / "MouseControl.exe")
    parser.add_argument("--skip-ui", action="store_true", help="使用已经构建好的 Vue 单页")
    args = parser.parse_args()
    subprocess.run([os.sys.executable, str(root / "scripts" / "update-ui-font.py")], check=True)
    if not args.skip_ui:
        for command in (["bun", "install", "--frozen-lockfile"], ["bun", "test"], ["bun", "run", "build"]):
            subprocess.run(command, cwd=root / "frontend", check=True)
    html = root / "frontend" / "dist" / "index.html"
    if not html.is_file() or html.stat().st_size > 1_900_000:
        raise SystemExit("缺少前端单页或文件超过 WebView2 内存页面大小限制")
    target = root / "go" / "gui_assets" / "index.html"
    target.parent.mkdir(parents=True, exist_ok=True)
    shutil.copyfile(html, target)
    env = dict(os.environ, GOOS="windows", GOARCH="amd64", CGO_ENABLED="0", GOTOOLCHAIN="local")
    args.output = args.output.resolve()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["go", "build", "-tags", "gui", "-trimpath", "-ldflags=-s -w -H windowsgui", "-o", str(args.output), "."], cwd=root / "go", env=env, check=True)
    print(f"已生成：{args.output}（{args.output.stat().st_size:,} 字节）")


if __name__ == "__main__":
    main()
