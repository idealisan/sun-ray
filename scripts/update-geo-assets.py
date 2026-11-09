#!/usr/bin/env python3
"""Refresh the geo tables embedded by assets.go.

geoip.dat is v2fly/geoip's cn.dat (already just the CN ranges).
geosite.dat is trimmed from v2fly/domain-list-community's dlc.dat down to
the two lists the routing presets use: CN and CATEGORY-ADS-ALL.

Both files are classic protobuf GeoIPList / GeoSiteList: a sequence of
field-1 messages whose own field 1 is the list name. Filtering on that
name at the wire level keeps the upstream bytes untouched and needs no
protobuf schema or Go toolchain.
"""

from __future__ import annotations

import json
import os
import sys
import urllib.request
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
ASSETS = ROOT / "assets"

GEOIP_REPO = "v2fly/geoip"
GEOIP_ASSET = "cn.dat"
GEOSITE_REPO = "v2fly/domain-list-community"
GEOSITE_ASSET = "dlc.dat"
KEEP = {"CN", "CATEGORY-ADS-ALL"}


def api_headers() -> dict[str, str]:
    headers = {
        "User-Agent": "sun-ray-geo-update",
        "Accept": "application/vnd.github+json",
    }
    # CI always has github.token; authenticating avoids the anonymous
    # 60-requests/hour API limit shared by all runners on the same IP.
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        headers["Authorization"] = f"Bearer {token}"
    return headers


def latest_asset(repo: str, asset: str) -> tuple[str, bytes]:
    """Return (release tag, asset bytes) from a repo's latest release."""
    url = f"https://api.github.com/repos/{repo}/releases/latest"
    request = urllib.request.Request(url, headers=api_headers())
    with urllib.request.urlopen(request, timeout=60) as response:
        release = json.load(response)
    tag = release["tag_name"]
    for item in release["assets"]:
        if item["name"] == asset:
            download = item["browser_download_url"]
            break
    else:
        raise RuntimeError(f"{repo} latest release has no {asset} asset")
    request = urllib.request.Request(
        download, headers={"User-Agent": "sun-ray-geo-update"}
    )
    with urllib.request.urlopen(request, timeout=180) as response:
        return tag, response.read()


def read_varint(data: bytes, offset: int) -> tuple[int, int]:
    value = 0
    shift = 0
    while True:
        if offset >= len(data):
            raise ValueError("truncated varint")
        byte = data[offset]
        offset += 1
        value |= (byte & 0x7F) << shift
        if not byte & 0x80:
            return value, offset
        shift += 7
        if shift > 63:
            raise ValueError("varint too long")


def write_varint(value: int) -> bytes:
    out = bytearray()
    while value >= 0x80:
        out.append((value & 0x7F) | 0x80)
        value >>= 7
    out.append(value)
    return bytes(out)


def entries(data: bytes):
    """Yield (uppercase list name, raw message) for each top-level field-1 message."""
    offset = 0
    while offset < len(data):
        tag, offset = read_varint(data, offset)
        if tag != 0x0A:  # field 1, wire type 2
            raise ValueError(f"unexpected top-level field tag 0x{tag:02x}")
        length, offset = read_varint(data, offset)
        end = offset + length
        if end > len(data):
            raise ValueError("truncated message")
        message = data[offset:end]
        offset = end
        field, cursor = read_varint(message, 0)
        if field != 0x0A:
            raise ValueError("list message has no name field")
        name_length, cursor = read_varint(message, cursor)
        name = message[cursor : cursor + name_length].decode("utf-8")
        yield name.upper(), message


def build(messages) -> bytes:
    out = bytearray()
    for message in messages:
        out += b"\x0a" + write_varint(len(message)) + message
    return bytes(out)


def main() -> int:
    geoip_tag, cn = latest_asset(GEOIP_REPO, GEOIP_ASSET)
    geosite_tag, dlc = latest_asset(GEOSITE_REPO, GEOSITE_ASSET)

    geoip = build(message for name, message in entries(cn) if name == "CN")
    geosite = build(message for name, message in entries(dlc) if name in KEEP)

    names = {name for name, _ in entries(geoip)}
    if names != {"CN"}:
        raise RuntimeError(f"geoip.dat unexpectedly contains {sorted(names)}")
    names = {name for name, _ in entries(geosite)}
    if names != KEEP:
        raise RuntimeError(f"geosite.dat unexpectedly contains {sorted(names)}")

    (ASSETS / "geoip.dat").write_bytes(geoip)
    (ASSETS / "geosite.dat").write_bytes(geosite)

    print(f"geoip:   {GEOIP_REPO} {geoip_tag} -> {len(geoip)} bytes, CN")
    print(
        f"geosite: {GEOSITE_REPO} {geosite_tag} -> {len(geosite)} bytes, "
        f"{', '.join(sorted(names))}"
    )

    output = os.environ.get("GITHUB_OUTPUT")
    if output:
        with open(output, "a", encoding="utf-8") as handle:
            handle.write(f"geoip_tag={geoip_tag}\n")
            handle.write(f"geosite_tag={geosite_tag}\n")
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as error:
        print(f"error: {error}", file=sys.stderr)
        sys.exit(1)
