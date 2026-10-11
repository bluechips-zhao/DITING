#!/usr/bin/env python3
"""Render the DITING sonar logo (rounded dark tile + three sonar arcs + dot) to
icon.png (512x512, replaces web/src/app/icon.png) and favicon.ico (16/32/48)."""
from PIL import Image, ImageDraw
from pathlib import Path

APP_DIR = Path(__file__).resolve().parents[1] / "web" / "src" / "app"

S = 512                      # canvas
RADIUS = int(S * 0.20)       # corner radius of the tile
BG = (26, 20, 66, 255)       # #1a1442
VIOLET = (139, 92, 246)      # #8b5cf6
DOT = (167, 139, 250)        # #a78bfa

def render(size: int) -> Image.Image:
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    # rounded tile
    d.rounded_rectangle([0, 0, size - 1, size - 1], radius=int(size * RADIUS / S), fill=BG)
    # sonar arcs: centered on the dot near the bottom-center, opening upward
    cx, cy = size * 0.5, size * 0.667
    widths = int(size * 0.055)
    # (radius, alpha) — three arcs from small/bright to large/faint
    for r_frac, alpha in ((0.17, 255), (0.26, 170), (0.35, 100)):
        r = size * r_frac
        bbox = [cx - r, cy - r, cx + r, cy + r]
        # upper semicircle (180deg..360deg in PIL's angle space = top half)
        d.arc(bbox, start=180, end=360, fill=VIOLET[:3] + (alpha,), width=widths)
    # dot at arc center
    dr = size * 0.054
    d.ellipse([cx - dr, cy - dr, cx + dr, cy + dr], fill=DOT)
    return img

# app icon (Next.js serves app/icon.png as the site icon)
render(512).save(APP_DIR / "icon.png")

# favicon.ico with three embedded sizes
ico = Image.new("RGBA", (48, 48), (0, 0, 0, 0))
fav = render(48)
fav.save(
    APP_DIR / "favicon.ico",
    sizes=[(16, 16), (32, 32), (48, 48)],
)
print("icon.png + favicon.ico written")
