# 菜单栏图标源文件

应用里能切换八种样式（设置 → 实验功能 → 状态栏图标），默认是「底板」：圆角黑底挖空一个叉路。

- `tray-tile.py`：底板版，用 PIL 画。
- `styles/*.svg`：其余七种的黑色正常态矢量源（线框、无框、插线板、拨杆开关、破框、像素主帅、三档阶梯），18 pt 视图盒。
- `build-all.py`：把全部样式出成 `../tray/<样式>/tray*.png`（四个状态、2x），并把正常态拷到 `ui/src/assets/tray/` 给设置页预览。改完源文件在本目录运行 `python3 build-all.py`。
- `render.svg.swift`：SVG 转 PNG，用系统的渲染器。

正常态是单色模板图（只看透明度，菜单栏自己上色）；告警、出错压了色点，不做模板。对比页在 `docs/design/tray-icon.html`。
