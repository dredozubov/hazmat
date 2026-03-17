#!/usr/bin/env python3
# /// script
# requires-python = ">=3.9"
# dependencies = ["pillow"]
# ///
"""
Homer Hazmat ANSI Art Generator
Closely follows Simpsons intro reference: Homer in yellow hazmat suit
holding glowing Claude Code logo (instead of uranium) with orange tongs.

Technique
---------
Renders a scene programmatically with PIL at 8× scale (440×528 px) for
anti-aliasing, then downsamples to 55×66 pixels and encodes each pair of
vertical pixels as one Unicode half-block character (▀) with 24-bit ANSI
true-color:  ESC[38;2;R;G;Bm sets the foreground (upper pixel),
             ESC[48;2;R;G;Bm sets the background (lower pixel).
Result: 55 chars wide × 33 lines tall — two pixels of vertical resolution
per terminal row, requires COLORTERM=truecolor (iTerm2 / any modern term).

Usage
-----
Preview in terminal (requires truecolor support):
    uv run art/gen_homer.py

Inspect the intermediate full-resolution render:
    uv run art/gen_homer.py        # also writes art/homer_render.png

Regenerate the Go constant (run from repo root or art/):
    uv run art/gen_homer.py --go \\
      | sed 's/^package ui$/package main/' \\
      | sed 's/HomerHazmatArt/homerHazmatArt/' \\
      > sandbox/homer_art.go
    cd sandbox && go build ./...

Tweaking the art
----------------
All colors are named constants at the top of this file (SUIT, VISOR_G,
TONG, CL_* for the Claude glow, etc.).  Layer comments mark each drawn
element so you can find and adjust individual shapes.  After any change,
re-run the regenerate command above and rebuild.
"""

from PIL import Image, ImageDraw
import math, sys

# ── Terminal art dimensions ──────────────────────────────────────────────────
TW, TH = 55, 33
PW, PH = TW, TH * 2          # pixel grid: 55 × 66
SCALE  = 8                    # internal render scale (anti-aliasing)
IW, IH = PW * SCALE, PH * SCALE  # 440 × 528

def s(n): return int(n * SCALE)

# ── Color palette ────────────────────────────────────────────────────────────
# The Simpsons uses warm near-black outlines — NOT pure black.
OL      = (14,  8,  2)    # outline / line art colour

BG      = (22, 21, 13)    # very dark background (makes suit pop)
BG_LT   = (36, 34, 21)    # slightly lighter panel (behind sign)

SUIT    = (230, 218, 148)  # main suit (warm pale yellow)
SUIT_LT = (248, 238, 175)  # suit highlight — right-facing surfaces
SUIT_DK = (178, 162, 98)   # suit mid-shadow
SUIT_DK2= (128, 112, 62)   # deep shadow / underside

VISOR_F = (164, 148, 100)  # visor plastic frame (tan)
VISOR_G = (88,  138, 168)  # glass (cool gray-blue)
VISOR_G2= (68,  118, 148)  # glass shadow band
VISOR_HL= (192, 214, 228)  # glass highlight / glare streak
VISOR_HL2=(155, 182, 200)  # secondary glare

TONG    = (202,  96,  40)  # main tong orange-brown
TONG_LT = (230, 132,  68)  # tong lit face
TONG_DK = (145,  60,  14)  # tong shadow face

# Claude Code glow — orange/amber (brand coral, NOT uranium green)
CL_WHITE= (255, 255, 250)
CL_CORE = (255, 255, 220)
CL_WARM = (255, 238, 160)
CL_MID  = (255, 178,  60)
CL_OUTER= (210, 100,  38)
CL_DIM  = (138,  56,  20)
CL_EDGE = ( 76,  28,   8)

SIGN_BG = ( 52,  44,  28)
SIGN_BD = ( 84,  70,  46)
SIGN_TX = (185,  48,  48)

# ── Canvas ───────────────────────────────────────────────────────────────────
img = Image.new('RGB', (IW, IH), BG)
d   = ImageDraw.Draw(img)

# ── Drawing primitives ───────────────────────────────────────────────────────
OW = s(1.4)  # default outline width in internal pixels

def E(x0,y0,x1,y1,c):  d.ellipse([x0,y0,x1,y1], fill=c)
def R(x0,y0,x1,y1,c):  d.rectangle([x0,y0,x1,y1], fill=c)

def Eo(x0,y0,x1,y1,fill, ow=None):
    """Ellipse with outline (draw outline first, fill on top)."""
    w = int(ow if ow is not None else OW)
    d.ellipse([x0-w, y0-w, x1+w, y1+w], fill=OL)
    d.ellipse([x0,   y0,   x1,   y1  ], fill=fill)

def Ro(x0,y0,x1,y1,fill, ow=None):
    """Rectangle with outline."""
    w = int(ow if ow is not None else OW)
    d.rectangle([x0-w, y0-w, x1+w, y1+w], fill=OL)
    d.rectangle([x0,   y0,   x1,   y1  ], fill=fill)

def bezier(p0,p1,p2, n=240):
    pts = []
    for i in range(n+1):
        t = i/n
        x = (1-t)**2*p0[0] + 2*(1-t)*t*p1[0] + t**2*p2[0]
        y = (1-t)**2*p0[1] + 2*(1-t)*t*p1[1] + t**2*p2[1]
        pts.append((int(x), int(y)))
    return pts

def tube(p0,p1,p2, color, thick, n=240):
    r = thick // 2
    for (x,y) in bezier(p0,p1,p2, n):
        d.ellipse([x-r, y-r, x+r, y+r], fill=color)

def glow(cx,cy, layers, ax=1.35):
    for (r,c) in sorted(layers, key=lambda x: -x[0]):
        ry = int(r / ax)
        d.ellipse([cx-r, cy-ry, cx+r, cy+ry], fill=c)

def fist(cx,cy, rx,ry, base,lt,dk):
    """Outlined gloved fist with knuckles."""
    w = int(OW * 1.3)
    # outline
    d.ellipse([cx-rx-w, cy-ry-w, cx+rx+w, cy+ry+w], fill=OL)
    # shadow side
    E(cx-rx, cy-ry, cx+rx, cy+ry, dk)
    # lit upper area
    E(cx-rx+s(0.4), cy-ry+s(0.3), cx+rx, cy+ry, base)
    # highlight
    E(cx-int(rx*0.6), cy-int(ry*0.7), cx+int(rx*0.25), cy-int(ry*0.1), lt)
    # 3 knuckle segments (vertical lines)
    for i in range(3):
        kx = cx - int(rx*0.45) + int(rx*0.38)*i
        R(kx, cy-int(ry*0.62), kx+int(rx*0.12), cy+int(ry*0.62), OL)


# ════════════════════════════════════════════════════════════════════════════
# LAYER 1 — BACKGROUND
# ════════════════════════════════════════════════════════════════════════════
R(0, 0, IW, IH, BG)
R(0, 0, s(23), IH, BG_LT)           # left panel behind sign
R(s(28), 0, IW, s(23), (36, 35, 22)) # upper-right panel

# ════════════════════════════════════════════════════════════════════════════
# LAYER 2 — AMBIENT GLOW (Homer's tong tip illuminates the background)
# ════════════════════════════════════════════════════════════════════════════
gl_cx, gl_cy = s(52), s(5)
for r,c in [(s(36),(38,18,5)),(s(29),(50,25,8)),(s(23),(64,32,12)),
            (s(17),(80,42,16)),(s(12),(99,52,20)),(s( 8),(120,65,26))]:
    ry = int(r / 1.65)
    d.ellipse([gl_cx-r, gl_cy-ry, gl_cx+r, gl_cy+ry], fill=c)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 3 — AUT SIGN (upper left)
# ════════════════════════════════════════════════════════════════════════════
R(0, s(4), s(22), s(27), SIGN_BG)
for a,b,c2,dd in [(0,s(4),s(22),s(4)+s(1.5)),
                  (0,s(4),s(1.5),s(27)),
                  (0,s(27)-s(1.5),s(22),s(27)),
                  (s(22)-s(1.5),s(4),s(22),s(27))]:
    R(a,b,c2,dd, SIGN_BD)

# Pixel letters — bolder (lw=s(5), lh=s(12))
def letA(x,y,w,h,c):
    hw = w//2
    d.polygon([(x+hw,y),(x,y+h),(x+w//4,y+h)], fill=c)
    d.polygon([(x+hw,y),(x+w,y+h),(x+3*w//4,y+h)], fill=c)
    R(x+w//6, y+h//2-s(0.6), x+w-w//6, y+h//2+s(1.0), c)

def letU(x,y,w,h,c):
    t = s(1.5)
    R(x,     y, x+t,   y+h-t, c)
    R(x+w-t, y, x+w,   y+h-t, c)
    R(x,     y+h-t*2, x+w, y+h, c)

def letT(x,y,w,h,c):
    t = s(1.5)
    R(x, y, x+w, y+t, c)
    R(x+w//2-t//2, y, x+w//2+t//2, y+h, c)

lx, ly, lw, lh, sp = s(2.5), s(7), s(5), s(12), s(1.5)
letA(lx,          ly, lw, lh, SIGN_TX)
letU(lx+lw+sp,    ly, lw, lh, SIGN_TX)
letT(lx+2*(lw+sp),ly, lw, lh, SIGN_TX)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 4 — HOMER'S SUIT BODY (outlined)
#
# The hazmat suit is a LARGE egg/oval that fills the frame.
# Key: Simpsons-style means draw the OUTLINE first, fill second.
# ════════════════════════════════════════════════════════════════════════════
bcx, bcy = s(24), s(56)   # body center (very low — cut off at bottom)
brx, bry = s(23), s(19)

ow_body = int(OW * 2.0)
# Silhouette outline — this single step makes everything recognizable
d.ellipse([bcx-brx-ow_body, bcy-bry-ow_body,
           bcx+brx+ow_body, bcy+bry+ow_body], fill=OL)
# Fill
E(bcx-brx, bcy-bry, bcx+brx, bcy+bry, SUIT)
# Upper highlight (light from above-right)
E(bcx-s(14), bcy-bry+s(1), bcx+s(12), bcy-s(9), SUIT_LT)
# Right-side shadow (rounds the form)
E(bcx+s(13), bcy-s(2), bcx+brx, bcy+bry,        SUIT_DK)
# Left-side shadow
E(bcx-brx,   bcy+s(5), bcx-s(9), bcy+bry,        SUIT_DK)
# Bottom-center shadow (suit resting on ground)
E(bcx-s(10), bcy+s(13), bcx+s(6), bcy+bry+s(1),  SUIT_DK2)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 5 — HEAD (outlined)
#
# The head is the upper portion of the suit — rounder, lighter, smaller
# than the body, but seamlessly connected (no visible neck joint).
# ════════════════════════════════════════════════════════════════════════════
hcx, hcy = s(24), s(25)
hrx, hry = s(15), s(13)

ow_head = int(OW * 1.8)
d.ellipse([hcx-hrx-ow_head, hcy-hry-ow_head,
           hcx+hrx+ow_head, hcy+hry+ow_head], fill=OL)
E(hcx-hrx, hcy-hry, hcx+hrx, hcy+hry, SUIT_LT)
# Neck/chin shadow blends head into body
E(hcx-s(11), hcy+hry-s(4), hcx+s(11), hcy+hry+s(5), SUIT_DK)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 6 — LEFT ARM (outlined short stub extending lower-left)
# ════════════════════════════════════════════════════════════════════════════
for (x,y,r,c) in [
    # outline first, then fill for each arm segment
    (s(12), s(51), s(7.5), OL),   (s(12), s(51), s(6.5), SUIT_DK),
    (s(10), s(54), s(7.2), OL),   (s(10), s(54), s(6.2), SUIT),
    (s(8),  s(57), s(7.0), OL),   (s(8),  s(57), s(6.0), SUIT),
]:
    E(x-r, y-r, x+r, y+r, c)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 7 — RIGHT ARM (extends horizontally to the right, at visor height)
# In the reference Homer's right arm reaches the tong at the same height as
# the visor — to the RIGHT of the face, not below it.
# ════════════════════════════════════════════════════════════════════════════
for (x,y,r,c) in [
    (s(35), s(29), s(7.5), OL),   (s(35), s(29), s(6.5), SUIT_DK),
    (s(39), s(27), s(7.5), OL),   (s(39), s(27), s(7.0), SUIT),
    (s(43), s(25), s(7.5), OL),   (s(43), s(25), s(7.0), SUIT_LT),
]:
    E(x-r, y-r, x+r, y+r, c)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 8 — VISOR (the single most recognizable feature)
#
# A large rectangular window set into the hazmat suit hood.
# Outlined frame, cool blue-gray glass with bright horizontal glare bands.
# In the Simpsons intro this visor is UNMISTAKABLE.
# ════════════════════════════════════════════════════════════════════════════
vx0, vy0 = s(11), s(15)
vx1, vy1 = s(38), s(28)   # 27 chars wide × 13 rows tall

# Strong outer outline
ow_v = int(OW * 1.5)
R(vx0-ow_v, vy0-ow_v, vx1+ow_v, vy1+ow_v, OL)
# Plastic frame (tan-brown)
R(vx0, vy0, vx1, vy1, VISOR_F)
# Inner frame edge
m1 = s(2.0)
R(vx0+m1, vy0+m1, vx1-m1, vy1-m1, OL)   # inner outline
m2 = s(3.5)
R(vx0+m2, vy0+m2, vx1-m2, vy1-m2, VISOR_G)   # glass
# Horizontal glare streaks — the key detail that screams "hazmat visor"
gl_top = vy0+m2
R(vx0+m2,          gl_top,         vx1-m2, gl_top+s(3.5), VISOR_HL)   # top bright band
R(vx0+m2,          gl_top+s(3.5),  vx1-m2, gl_top+s(5.5), VISOR_HL2)  # second band
# Left-side vertical glare (window reflection)
R(vx0+m2, vy0+m2, vx0+m2+s(5), vy1-m2, VISOR_HL2)
# Bottom shadow band
R(vx0+m2, vy1-m2-s(3.5), vx1-m2, vy1-m2, VISOR_G2)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 9 — TONGS (the dramatic diagonal — from lower-left to upper-right)
#
# KEY: the bezier control point bows the tong slightly so it passes to the
# RIGHT of the visor without a dramatic curve.  At the visor bottom (y=28)
# the tong is at x≈43, vs visor right edge at x=38 — 5-char clearance.
#
# The math: with t0=(8,58), t1=(47,35), t2=(52,5), at y=25 (visor mid)
# the tong x≈45 — right of visor edge x=38, matching right fist pos. ✓
#
# Rendered as a 3D cylinder:
#   1. fat dark outline tube
#   2. shadow face (dark orange, offset toward lower-right)
#   3. main body (mid orange)
#   4. highlight face (bright orange, offset toward upper-left)
# ════════════════════════════════════════════════════════════════════════════
t0 = (s(8),  s(58))   # base — at left fist
t2 = (s(52), s(5))    # tip  — Claude logo
t1 = (s(47), s(35))   # control point: gentle bow, keeps tong right of visor

THICK = s(4.2)
OFF   = s(2.2)

tube(t0, t1, t2, OL, int(THICK * 2.0 + OFF * 2))          # 1. outline
tube((t0[0]+OFF, t0[1]+OFF), (t1[0]+OFF, t1[1]+OFF),
     (t2[0]+OFF, t2[1]+OFF), TONG_DK, int(THICK + OFF))    # 2. shadow face
tube(t0, t1, t2, TONG, int(THICK))                         # 3. main body
tube((t0[0]-OFF, t0[1]-OFF), (t1[0]-OFF, t1[1]-OFF),
     (t2[0]-OFF, t2[1]-OFF), TONG_LT, int(THICK * 0.45))  # 4. highlight

tip_r = s(3.5)
E(t2[0]-tip_r, t2[1]-tip_r, t2[0]+tip_r, t2[1]+tip_r, OL)
E(t2[0]-s(2.5),t2[1]-s(2.5),t2[0]+s(2.5),t2[1]+s(2.5), TONG_DK)

# ════════════════════════════════════════════════════════════════════════════
# LAYER 10 — GLOVED FISTS
# Left grips tong base; right is at right arm end (visor height, x≈45)
# ════════════════════════════════════════════════════════════════════════════
fist(s(8),  s(57), s(9),  s(6), SUIT, SUIT_LT, SUIT_DK2)  # left  — grips tong base
fist(s(45), s(25), s(10), s(7), SUIT, SUIT_LT, SUIT_DK2)  # right — at right arm end

# ════════════════════════════════════════════════════════════════════════════
# LAYER 11 — CLAUDE CODE LOGO GLOW
# ════════════════════════════════════════════════════════════════════════════
glow(gl_cx, gl_cy, [
    (s(19), CL_EDGE),
    (s(14), CL_DIM),
    (s(10), CL_OUTER),
    (s( 7), CL_MID),
    (s( 5), CL_WARM),
    (s(3.2),CL_CORE),
    (s(2.0),CL_WHITE),
], ax=1.3)

lr = int(s(3.2))
d.polygon([
    (gl_cx,              gl_cy - int(lr*1.7)),
    (gl_cx + int(lr*1.7), gl_cy),
    (gl_cx,              gl_cy + int(lr*1.7)),
    (gl_cx - int(lr*1.7), gl_cy),
], fill=CL_WHITE)
rc = s(1.0)
E(gl_cx-rc, gl_cy-rc, gl_cx+rc, gl_cy+rc, (255, 255, 255))

# ════════════════════════════════════════════════════════════════════════════
# RENDER → half-block ANSI
# ════════════════════════════════════════════════════════════════════════════
img.save("homer_render.png")

small = img.resize((PW, PH), Image.Resampling.LANCZOS)
pix   = small.load()
RESET = "\033[0m"

def hb(top, bot):
    r1,g1,b1 = top[:3]; r2,g2,b2 = bot[:3]
    return f"\033[38;2;{r1};{g1};{b1}m\033[48;2;{r2};{g2};{b2}m▀"

lines = []
for row in range(TH):
    lines.append(
        "".join(hb(pix[col,row*2][:3], pix[col,row*2+1][:3]) for col in range(TW))
        + RESET
    )

art = "\n".join(lines)

if "--go" in sys.argv:
    escaped = art.replace("\\", "\\\\").replace("`", "` + \"`\" + `")
    print(f'''package ui

// homerHazmatArt is ANSI terminal art of Homer Simpson in a hazmat suit
// holding the Claude Code logo.  55 chars wide × 33 lines, 24-bit true color.
//
// DO NOT EDIT — generated by art/gen_homer.py.  To regenerate:
//
//\tuv run art/gen_homer.py --go \\
//\t  | sed 's/^package ui$/package main/' \\
//\t  | sed 's/HomerHazmatArt/homerHazmatArt/' \\
//\t  > sandbox/homer_art.go
//\tcd sandbox && go build ./...
const homerHazmatArt = `{escaped}`
''')
else:
    print(art)
