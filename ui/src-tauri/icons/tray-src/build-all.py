#!/usr/bin/env python3
# 生成菜单栏图标的全部样式：每种样式四个状态（正常、告警、出错、暂停）的 2x 位图，给应用切换用；
# 正常状态另拷一份到 ui/src/assets/tray/ 给设置页做预览。
# 「底板」由 tray-tile.py 画，其余样式是 styles/*.svg（黑色正常态），暂停态把黑换成灰，告警 / 出错在右下角压色点。
# 用法：python3 build-all.py   （需要本机 swift，用 render.svg.swift 把 SVG 转 PNG）
import os, subprocess, shutil, sys
HERE = os.path.dirname(os.path.abspath(__file__))
ICONS = os.path.dirname(HERE)
UI_ASSETS = os.path.join(os.path.dirname(os.path.dirname(ICONS)), 'src', 'assets', 'tray')
sys.path.insert(0, HERE)
import importlib
tile = importlib.import_module('tray-tile')

STYLES = ['tile', 'frame', 'fork', 'patchbay', 'lever', 'breakout', 'pixel', 'ladder']  # 和 internal/config 的 TrayIcons 一致
GREY = '#9A958C'
BADGE = {'tray-warn': '#D98B1F', 'tray-err': '#C4463A'}
badge = lambda c: f'<circle cx="14.6" cy="14.6" r="4" fill="#E6E2D9"/><circle cx="14.6" cy="14.6" r="2.9" fill="{c}"/>'

def variants(svg):
    base = svg.replace('#000000', '#000')
    return {
        'tray': base,
        'tray-warn': base.replace('</svg>', badge(BADGE['tray-warn']) + '\n</svg>'),
        'tray-err': base.replace('</svg>', badge(BADGE['tray-err']) + '\n</svg>'),
        'tray-off': base.replace('#000', GREY),
    }

def main():
    os.makedirs(UI_ASSETS, exist_ok=True)
    jobs = []
    tmp = os.path.join(HERE, '.build')
    os.makedirs(tmp, exist_ok=True)
    for style in STYLES:
        out = os.path.join(ICONS, 'tray', style)
        os.makedirs(out, exist_ok=True)
        if style == 'tile':
            for name, kw in tile.VARIANTS.items():
                tile.render(36, **kw).save(os.path.join(out, f'{name}@2x.png'))
            continue
        svg = open(os.path.join(HERE, 'styles', f'{style}.svg'), encoding='utf-8').read()
        for name, body in variants(svg).items():
            p = os.path.join(tmp, f'{style}-{name}.svg')
            open(p, 'w', encoding='utf-8').write(body)
            jobs += [p, os.path.join(out, f'{name}@2x.png'), '36']
    subprocess.run(['swift', os.path.join(HERE, 'render.svg.swift'), *jobs], check=True, stdout=subprocess.DEVNULL)
    shutil.rmtree(tmp)
    for style in STYLES:
        shutil.copy(os.path.join(ICONS, 'tray', style, 'tray@2x.png'), os.path.join(UI_ASSETS, f'{style}.png'))
    # 根目录的 tray*.png 仍是默认样式（底板），给没传样式的老路径用
    for name, kw in tile.VARIANTS.items():
        for px, sfx in ((18, ''), (36, '@2x')):
            tile.render(px, **kw).save(os.path.join(ICONS, f'{name}{sfx}.png'))
    print('样式：', ', '.join(STYLES))

if __name__ == '__main__':
    main()
