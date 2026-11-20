#!/usr/bin/env python3
"""Build a self-contained systemd installer for Debian / Ubuntu.

Each installer embeds all three Linux binaries (amd64, arm64, armv7) as
gzipped base64. The installer detects the architecture at run time.

Usage:
  python3 scripts/make_systemd_installer.py \
      --variant nogui --version 0.20.19 \
      --amd64 dist/sun-ray-nogui-linux-amd64 \
      --arm64 dist/sun-ray-nogui-linux-arm64 \
      --armv7 dist/sun-ray-nogui-linux-armv7 \
      --template scripts/systemd-installer.sh.template \
      --output dist/sun-ray-systemd-debian-ubuntu-nogui.sh
"""

import argparse
import base64
import gzip
import os
import sys

TOKENS = ("__VARIANT__", "__VERSION__", "__B64_AMD64__", "__B64_ARM64__", "__B64_ARMV7__")


def embedded(path):
    with open(path, "rb") as f:
        return base64.b64encode(gzip.compress(f.read(), compresslevel=9)).decode("ascii")


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--variant", required=True, choices=("nogui", "webui"))
    p.add_argument("--version", required=True)
    p.add_argument("--amd64", required=True, help="amd64 binary")
    p.add_argument("--arm64", required=True, help="arm64 binary")
    p.add_argument("--armv7", required=True, help="armv7 binary")
    p.add_argument("--template", required=True)
    p.add_argument("--output", required=True)
    args = p.parse_args()

    with open(args.template, "r", encoding="utf-8") as f:
        template = f.read()

    script = (
        template
        .replace("__VARIANT__", args.variant)
        .replace("__VERSION__", args.version)
        .replace("__B64_AMD64__", embedded(args.amd64))
        .replace("__B64_ARM64__", embedded(args.arm64))
        .replace("__B64_ARMV7__", embedded(args.armv7))
    )
    left = [t for t in TOKENS if t in script]
    if left:
        sys.exit("template has unfilled tokens: " + ", ".join(left))

    with open(args.output, "w", encoding="utf-8") as f:
        f.write(script)
    os.chmod(args.output, 0o755)
    print(f"wrote {args.output} ({os.path.getsize(args.output)} bytes)")


if __name__ == "__main__":
    main()
