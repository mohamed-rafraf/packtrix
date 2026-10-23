# Copyright 2026 The Packtrix Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Builds the Packtrix logo files in docs/assets/logo.

The mark is a 2x2x2 cube of packages without its front corner, in isometric
projection. The wordmark is Space Grotesk SemiBold, converted to outlines so it
renders the same everywhere.

Requirements: pip install fonttools uharfbuzz cairosvg pillow shapely, and the
Space Grotesk variable font from
https://github.com/google/fonts/tree/main/ofl/spacegrotesk (SIL Open Font
License 1.1).

Usage: python hack/logo/build.py <SpaceGrotesk[wght].ttf> docs/assets/logo
"""

import io
import math
import sys
from pathlib import Path

import cairosvg
import uharfbuzz as hb
from fontTools.pens.boundsPen import BoundsPen
from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont
from fontTools.varLib import instancer
from PIL import Image
from shapely.geometry import Polygon
from shapely.ops import unary_union

INK = "#0F1B2D"
PAPER = "#F4F6F8"
ACCENT = "#F26B1D"
FACES_LIGHT = ("#3B4B63", "#24324A", "#142033")  # top, left, right; for light backgrounds
FACES_DARK = ("#E2E8F0", "#B6C2D2", "#8A99AD")   # for dark backgrounds
TAGLINE = "#B6C2D2"


def mix(hex_a, hex_b, t):
    a = [int(hex_a[i:i + 2], 16) for i in (1, 3, 5)]
    b = [int(hex_b[i:i + 2], 16) for i in (1, 3, 5)]
    return "#" + "".join(f"{round(x + (y - x) * t):02X}" for x, y in zip(a, b))


ACCENTS = (mix(ACCENT, "#FFFFFF", 0.3), ACCENT, mix(ACCENT, "#000000", 0.22))

# --- Geometry: a 2x2x2 cube without its front corner, in a 64x64 box -------

U = 15.0
HW, HH = U * math.cos(math.radians(30)), U * math.sin(math.radians(30))
QUADS = {
    "top": [(0, 0, 1), (1, 0, 1), (1, 1, 1), (0, 1, 1)],
    "left": [(0, 1, 0), (1, 1, 0), (1, 1, 1), (0, 1, 1)],
    "right": [(1, 0, 0), (1, 1, 0), (1, 1, 1), (1, 0, 1)],
}
NEIGHBOUR = {"top": (0, 0, 1), "left": (0, 1, 0), "right": (1, 0, 0)}
INNER = {((0, 1, 1), "right"), ((1, 0, 1), "left"), ((1, 1, 0), "top")}
SIDE = {"top": 0, "left": 1, "right": 2}


def proj(x, y, z):
    return (32 + (x - y) * HW, 32 + (x + y) * HH - z * U)


def cells():
    """Visible faces as (shade index, is_accent, polygon), back to front."""
    cubes = [(i, j, k) for i in (0, 1) for j in (0, 1) for k in (0, 1) if (i, j, k) != (1, 1, 1)]
    present = set(cubes)
    out = []
    for c in sorted(cubes, key=sum):
        for side, quad in QUADS.items():
            d = NEIGHBOUR[side]
            if (c[0] + d[0], c[1] + d[1], c[2] + d[2]) in present:
                continue
            pts = [proj(c[0] + a, c[1] + b, c[2] + e) for a, b, e in quad]
            out.append((SIDE[side], (c, side) in INNER, pts))
    return out


def shrink(pts, k):
    mx = sum(p[0] for p in pts) / len(pts)
    my = sum(p[1] for p in pts) / len(pts)
    return [(mx + (x - mx) * k, my + (y - my) * k) for x, y in pts]


def points(pts):
    return " ".join(f"{x:.2f},{y:.2f}" for x, y in pts)


def mark_shapes(faces, solid):
    """SVG polygons for the mark. The solid version merges each face into one shape for small sizes."""
    shapes = []
    if not solid:
        for side, accent, pts in cells():
            fill = ACCENTS[side] if accent else faces[side]
            shapes.append(f'<polygon points="{points(shrink(pts, 0.86))}" fill="{fill}"/>')
        return shapes
    groups = {}
    for side, accent, pts in cells():
        groups.setdefault((side, accent), []).append(Polygon(pts))
    for (side, accent), polys in sorted(groups.items()):
        merged = unary_union(polys).buffer(0.01, join_style=2)
        fill = ACCENTS[side] if accent else faces[side]
        cls = ("t", "l", "r")[side]
        attr = f'fill="{fill}"' if accent else f'class="{cls}" fill="{fill}"'
        coords = list(merged.exterior.coords)[:-1]
        shapes.append(f'<polygon points="{points(coords)}" {attr}/>')
    return shapes


# --- Wordmark: Space Grotesk, outlined ----------------------------------------

class Wordmark:
    def __init__(self, font_path, weight):
        vf = TTFont(font_path)
        self.font = instancer.instantiateVariableFont(vf, {"wght": weight})
        buf = io.BytesIO()
        self.font.save(buf)
        self.hb_font = hb.Font(hb.Face(hb.Blob(buf.getvalue())))
        self.upem = self.font["head"].unitsPerEm
        self.x_height = self.font["OS/2"].sxHeight
        self.glyphs = self.font.getGlyphSet()

    def path(self, text, size, tracking=-0.03):
        """SVG path data with the baseline at y=0, and the advance width, in px."""
        buf = hb.Buffer()
        buf.add_str(text)
        buf.guess_segment_properties()
        hb.shape(self.hb_font, buf, {"kern": True, "liga": True})
        scale = size / self.upem
        order = self.font.getGlyphOrder()
        pen = SVGPathPen(self.glyphs)
        bounds = BoundsPen(self.glyphs)
        x = 0
        infos, positions = buf.glyph_infos, buf.glyph_positions
        for n, (info, pos) in enumerate(zip(infos, positions)):
            name = order[info.codepoint]
            # Flip y: font units point up, SVG points down.
            t = (scale, 0, 0, -scale, (x + pos.x_offset) * scale, -pos.y_offset * scale)
            self.glyphs[name].draw(TransformPen(pen, t))
            self.glyphs[name].draw(TransformPen(bounds, t))
            x += pos.x_advance + (tracking * self.upem if n < len(infos) - 1 else 0)
        return pen.getCommands(), bounds.bounds, self.x_height * scale


def svg(width, height, body, view=None, extra=""):
    view = view or f"0 0 {width:g} {height:g}"
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{width:g}" height="{height:g}" '
            f'viewBox="{view}" role="img" aria-label="Packtrix">{extra}{body}</svg>\n')


def lockup(word, faces, text_color, size=64, mark=78, gap=20):
    """Mark and wordmark side by side; the mark is centered on the x-height."""
    d, (x0, y0, x1, y1), xh = word.path("packtrix", size)
    base = mark / 2 + xh / 2          # baseline y so that the x-height is centered on the mark
    top = min(0, base + y0)
    bottom = max(mark, base + y1)
    tx = mark + gap - x0
    width = tx + x1
    body = (f'<g transform="scale({mark / 64:.5f})">{"".join(mark_shapes(faces, solid=False))}</g>'
            f'<path transform="translate({tx:.2f} {base:.2f})" d="{d}" fill="{text_color}"/>')
    return svg(round(width, 2), round(bottom - top, 2), body, view=f"0 {top:.2f} {width:.2f} {bottom - top:.2f}")


def main():
    font_path, out = sys.argv[1], Path(sys.argv[2])
    out.mkdir(parents=True, exist_ok=True)
    bold = Wordmark(font_path, 600)
    regular = Wordmark(font_path, 500)

    files = {
        "packtrix-mark.svg": svg(64, 64, "".join(mark_shapes(FACES_LIGHT, False))),
        "packtrix-mark-dark.svg": svg(64, 64, "".join(mark_shapes(FACES_DARK, False))),
        "packtrix-mark-small.svg": svg(64, 64, "".join(mark_shapes(FACES_LIGHT, True))),
        "packtrix-mark-small-dark.svg": svg(64, 64, "".join(mark_shapes(FACES_DARK, True))),
        "packtrix-logo.svg": lockup(bold, FACES_LIGHT, INK),
        "packtrix-logo-dark.svg": lockup(bold, FACES_DARK, PAPER),
    }
    # The favicon follows the browser's color scheme.
    style = ("<style>.t{fill:%s}.l{fill:%s}.r{fill:%s}"
             "@media (prefers-color-scheme: dark){.t{fill:%s}.l{fill:%s}.r{fill:%s}}</style>"
             % (*FACES_LIGHT, *FACES_DARK))
    files["favicon.svg"] = svg(64, 64, "".join(mark_shapes(FACES_LIGHT, True)), extra=style)
    for name, text in files.items():
        (out / name).write_text(text)

    def png(svg_text, size, name):
        cairosvg.svg2png(bytestring=svg_text.encode(), write_to=str(out / name),
                         output_width=size[0], output_height=size[1])

    # Favicons and touch icon.
    for px in (16, 32, 48):
        png(files["packtrix-mark-small.svg"], (px, px), f"favicon-{px}.png")
    Image.open(out / "favicon-48.png").save(out / "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)])
    for px in (16, 48):
        (out / f"favicon-{px}.png").unlink()
    touch = svg(180, 180, f'<rect width="180" height="180" fill="{PAPER}"/>'
                          f'<g transform="translate(26 26) scale({128 / 64})">{"".join(mark_shapes(FACES_LIGHT, False))}</g>')
    png(touch, (180, 180), "apple-touch-icon.png")

    # GitHub organization avatar.
    avatar = svg(512, 512, f'<rect width="512" height="512" fill="{INK}"/>'
                           f'<g transform="translate(96 96) scale({320 / 64})">{"".join(mark_shapes(FACES_DARK, False))}</g>')
    png(avatar, (512, 512), "avatar.png")

    # Social preview for the repository (1280x640), with the tagline.
    logo = lockup(bold, FACES_DARK, PAPER, size=112, mark=136, gap=34)
    lw = float(logo.split('width="')[1].split('"')[0])
    lh = float(logo.split('height="')[1].split('"')[0])
    view = logo.split('viewBox="')[1].split('"')[0].split()
    inner = logo.split('aria-label="Packtrix">')[1].rsplit("</svg>", 1)[0]
    tag_d, (t0, _, t1, _), _ = regular.path("Declarative offline APT and RPM repositories", 40, tracking=-0.01)
    tag_w = t1 - t0
    lx, ly = (1280 - lw) / 2, 230
    body = (f'<rect width="1280" height="640" fill="{INK}"/>'
            f'<g transform="translate({lx:.2f} {ly - float(view[1]):.2f})">{inner}</g>'
            f'<path transform="translate({(1280 - tag_w) / 2 - t0:.2f} {ly + lh + 70:.2f})" d="{tag_d}" fill="{TAGLINE}"/>')
    png(svg(1280, 640, body), (1280, 640), "social-preview.png")

    for name in sorted(p.name for p in out.iterdir()):
        print(name, (out / name).stat().st_size)


if __name__ == "__main__":
    main()
