#!/usr/bin/env python3
"""Render the tray icons: a filled sun for the running state, an outlined
one for the stopped state, in the template (black, tinted by the system)
and the color (amber) variants.

Usage: python3 scripts/gen-tray-icons.py
"""

import math
import struct
import zlib

SIZE = 64
SCALE = 4  # supersampling, for smooth edges


def write_png(path, pixels, width, height):
    """pixels: a list of rows, each a list of (r, g, b, a)."""
    raw = b""
    for row in pixels:
        raw += b"\x00" + b"".join(struct.pack("BBBB", *px) for px in row)

    def chunk(tag, data):
        c = struct.pack(">I", len(data)) + tag + data
        return c + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)

    png = b"\x89PNG\r\n\x1a\n"
    png += chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 6, 0, 0, 0))
    png += chunk(b"IDAT", zlib.compress(raw, 9))
    png += chunk(b"IEND", b"")
    open(path, "wb").write(png)


def distance_to_segment(px, py, ax, ay, bx, by):
    """The distance from a point to a segment, for the round caps."""
    dx, dy = bx - ax, by - ay
    if dx == 0 and dy == 0:
        return math.hypot(px - ax, py - ay)
    t = max(0.0, min(1.0, ((px - ax) * dx + (py - ay) * dy) / (dx * dx + dy * dy)))
    return math.hypot(px - (ax + t * dx), py - (ay + t * dy))


def render(color, filled):
    """The sun: eight rays and a disc, filled or outlined."""
    rays = [(32, 5, 32, 14), (32, 50, 32, 59), (5, 32, 14, 32), (50, 32, 59, 32),
            (13, 13, 19, 19), (45, 45, 51, 51), (51, 13, 45, 19), (19, 45, 13, 51)]
    half = 2.5  # the rays are 5 wide
    rows = []
    for y in range(SIZE):
        row = []
        for x in range(SIZE):
            hits = 0
            for sy in range(SCALE):
                for sx in range(SCALE):
                    px = x + (sx + 0.5) / SCALE
                    py = y + (sy + 0.5) / SCALE
                    inside = False
                    for ax, ay, bx, by in rays:
                        if distance_to_segment(px, py, ax, ay, bx, by) <= half:
                            inside = True
                            break
                    if not inside:
                        d = math.hypot(px - 32, py - 32)
                        if filled:
                            inside = d <= 10
                        else:
                            inside = 7.5 <= d <= 12.5
                    if inside:
                        hits += 1
            alpha = int(round(255 * hits / (SCALE * SCALE)))
            row.append((color[0], color[1], color[2], alpha))
        rows.append(row)
    return rows


def main():
    black = (0, 0, 0)
    amber = (255, 176, 32)
    for name, color in (("tray-template", black), ("tray-color", amber)):
        write_png(f"assets/{name}.png", render(color, True), SIZE, SIZE)
        write_png(f"assets/{name}-outline.png", render(color, False), SIZE, SIZE)
        print("assets/%s.png, assets/%s-outline.png" % (name, name))


if __name__ == "__main__":
    main()