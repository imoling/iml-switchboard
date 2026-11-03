#!/usr/bin/env python3
# 菜单栏图标「实心底板」版：圆角黑底，挖空一个叉路（左起点、三条分支，选中的那条更粗）。
# 模板图只认透明度，挖空就是透明；三张状态图在右下角压一个色点，不做模板。
# 用法：python3 tray-tile.py  →  写出上一级目录的 tray*.png（18 px）和 tray*@2x.png（36 px）
import os
from PIL import Image, ImageDraw, ImageChops

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.dirname(HERE)
SS = 16  # 超采样倍数

# 18 pt 视图盒里的几何：叉路按原标志缩到七成放在底板中央
def pt(x, y, s=0.7):
    return (9 + (x - 9) * s, 9 + (y - 9) * s)

L, T0, C1 = pt(3.1, 9), pt(8, 9), pt(11, 9)
C2T, ET, NT = pt(10.6, 3.8), pt(13.6, 3.8), pt(15, 3.8)
EM, NM = pt(13.6, 9), pt(15, 9)
C2B, EB, NB = pt(10.6, 14.2), pt(13.6, 14.2), pt(15, 14.2)


def bezier(p0, p1, p2, p3, n=120):
    out = []
    for i in range(n + 1):
        t = i / n
        a, b, c, d = (1 - t) ** 3, 3 * (1 - t) ** 2 * t, 3 * (1 - t) * t ** 2, t ** 3
        out.append((a * p0[0] + b * p1[0] + c * p2[0] + d * p3[0], a * p0[1] + b * p1[1] + c * p2[1] + d * p3[1]))
    return out


def stamp(draw, pts, width, k):
    """沿路径盖圆章画粗线：天然圆头圆角，边缘干净"""
    r = width * k / 2
    prev = None
    for x, y in pts:
        x, y = x * k, y * k
        if prev is not None:
            dx, dy = x - prev[0], y - prev[1]
            steps = max(1, int((dx * dx + dy * dy) ** 0.5 / (k * 0.04)))
            for i in range(1, steps + 1):
                px, py = prev[0] + dx * i / steps, prev[1] + dy * i / steps
                draw.ellipse([px - r, py - r, px + r, py + r], fill=255)
        else:
            draw.ellipse([x - r, y - r, x + r, y + r], fill=255)
        prev = (x, y)


def dot(draw, p, r, k):
    x, y = p[0] * k, p[1] * k
    draw.ellipse([x - r * k, y - r * k, x + r * k, y + r * k], fill=255)


def render(px, color=(0, 0, 0), badge=None):
    n = px * SS
    k = n / 18.0
    tile = Image.new('L', (n, n), 0)
    ImageDraw.Draw(tile).rounded_rectangle([0.5 * k, 0.5 * k, 17.5 * k, 17.5 * k], radius=4.6 * k, fill=255)
    hole = Image.new('L', (n, n), 0)
    h = ImageDraw.Draw(hole)
    # 次要两条分支细一点、点小一点；主分支和起点、选中节点粗
    stamp(h, [T0, EM], 1.3, k)
    stamp(h, bezier(T0, C1, C2B, EB), 1.3, k)
    dot(h, NM, 1.2, k)
    dot(h, NB, 1.2, k)
    stamp(h, [L, T0] + bezier(T0, C1, C2T, ET)[1:], 1.9, k)
    dot(h, L, 1.8, k)
    dot(h, NT, 1.8, k)
    alpha = ImageChops.subtract(tile, hole)
    img = Image.new('RGBA', (n, n), color + (0,))
    img.putalpha(alpha)
    if badge:
        b = ImageDraw.Draw(img)
        x, y = 14.6 * k, 14.6 * k
        b.ellipse([x - 4 * k, y - 4 * k, x + 4 * k, y + 4 * k], fill=(230, 226, 217, 255))
        b.ellipse([x - 2.9 * k, y - 2.9 * k, x + 2.9 * k, y + 2.9 * k], fill=badge + (255,))
    return img.resize((px, px), Image.LANCZOS)


VARIANTS = {
    'tray': dict(),
    'tray-warn': dict(badge=(217, 139, 31)),
    'tray-err': dict(badge=(196, 70, 58)),
    'tray-off': dict(color=(154, 149, 140)),
}
if __name__ == '__main__':
    for name, kw in VARIANTS.items():
        for px, sfx in ((18, ''), (36, '@2x')):
            render(px, **kw).save(os.path.join(OUT, f'{name}{sfx}.png'))
            print('写出', f'{name}{sfx}.png', px, 'px')

