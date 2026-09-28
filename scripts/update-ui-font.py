#!/usr/bin/env python3
"""检查内嵌字体覆盖；--refresh 使用已下载的公开字体在本地更新子集。"""
from pathlib import Path
import argparse
import hashlib
import json
import sys

ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / "frontend/src/assets"
FONT = ASSETS / "MouseUISans.woff2"
MANIFEST = ASSETS / "font-manifest.json"
LICENSE = ASSETS / "NotoSansSC-OFL.txt"
CSS = ROOT / "frontend/src/fonts.css"


def project_characters():
    # ASCII 覆盖动态英文型号；中文包含前端文字及后端返回的消息。
    characters = set(chr(value) for value in range(32, 127))
    for folder, patterns in [(ROOT / "frontend/src", ["*.vue", "*.ts"]), (ROOT / "go", ["*.go"])]:
        for pattern in patterns:
            for path in folder.rglob(pattern):
                if not path.name.endswith("_test.go"):
                    characters.update(c for c in path.read_text(encoding="utf-8") if ord(c) > 127 and not c.isspace())
    return "".join(sorted(characters))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--refresh", action="store_true", help="从本地完整字体更新资源；整个脚本不发出网络请求")
    parser.add_argument("--source", type=Path, default=ROOT / ".cache/fonts/NotoSansSC.ttf")
    args = parser.parse_args()
    characters = project_characters()
    if args.refresh:
        # 裁剪依赖仅用于维护资源，普通检查和程序运行不需要它们。
        sys.path.insert(0, str(ROOT / ".cache/font-tools"))
        from fontTools import subset
        from fontTools.ttLib import TTFont
        from fontTools.varLib.instancer import instantiateVariableFont
        license_data = (args.source.parent / "OFL.txt").read_bytes()
        if b"SIL OPEN FONT LICENSE Version 1.1" not in license_data:
            raise SystemExit("未取得有效字体许可")
        font = TTFont(args.source, recalcTimestamp=False)
        missing = set(characters) - {chr(c) for c in font.getBestCmap()}
        if missing:
            raise SystemExit(f"完整字体也不包含这些字符：{''.join(sorted(missing))}")
        options = subset.Options()
        options.recalc_timestamp = False
        sub = subset.Subsetter(options)
        sub.populate(text=characters)
        sub.subset(font)
        instantiateVariableFont(font, {"wght": (400, 700)}, inplace=True)
        font.flavor = "woff2"
        ASSETS.mkdir(parents=True, exist_ok=True)
        font.save(FONT)
        data = FONT.read_bytes()
        LICENSE.write_bytes(license_data)
        MANIFEST.write_text(json.dumps({"family": "Noto Sans SC", "weights": [400, 700],
            "characters": characters, "sha256": hashlib.sha256(data).hexdigest(),
            "source": "https://github.com/google/fonts/tree/main/ofl/notosanssc",
            "sourceSha256": hashlib.sha256(args.source.read_bytes()).hexdigest(),
            "generator": "fonttools 4.60.1; brotli 1.1.0; local subset"}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        CSS.write_text("/* Noto Sans SC 官方可变字体子集；本地内嵌，无网络请求。许可见 assets/NotoSansSC-OFL.txt。 */\n"
            "@font-face {\n  font-family: 'Mouse UI Sans';\n  src: url('./assets/MouseUISans.woff2') format('woff2');\n"
            "  font-style: normal;\n  font-weight: 400 700;\n  font-display: block;\n}\n", encoding="utf-8")
    if not FONT.is_file() or not MANIFEST.is_file() or not LICENSE.is_file():
        raise SystemExit("缺少已保存的字体资源，请运行本脚本 --refresh")
    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    if hashlib.sha256(FONT.read_bytes()).hexdigest() != manifest["sha256"]:
        raise SystemExit("字体资源与记录的哈希不一致")
    missing = set(characters) - set(manifest["characters"])
    if missing:
        raise SystemExit(f"字体子集缺少新增文字：{''.join(sorted(missing))}；请运行本脚本 --refresh")
    print(f"字体校验通过：{len(manifest['characters'])} 个字符，{FONT.stat().st_size:,} 字节，权重 400–700")


if __name__ == "__main__":
    main()
