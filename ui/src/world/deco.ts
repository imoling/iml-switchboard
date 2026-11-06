/** 主题背景上会动的小东西：树叶闪、水面光、水草摆、烟、太阳。都是在整图上点几个像素，不重画背景 */
import { hash } from './model';
import type { Painter } from './scene';

type Box = [number, number, number, number];

/** 树冠、花丛里有几片叶子在翻：每个框里 n 个点，轮流亮 */
export function flutter(p: Painter, t: number, boxes: Box[], cols: string[], n = 5) {
  boxes.forEach(([x, y, w, h], b) => {
    for (let i = 0; i < n; i++) {
      const s = hash(`${b}|${i}`);
      if ((Math.floor(t * 2.5) + i + b) % 4 !== 0) continue;
      p.R(x + (s % w), y + ((s >>> 5) % h), 1, 1, cols[(s >>> 9) % cols.length]);
    }
  });
}

/** 水面的光：短横线慢慢漂 */
export function ripples(p: Painter, t: number, [x, y, w, h]: Box, col = 'rgba(255,255,255,.6)', n = 8, speed = 6) {
  for (let i = 0; i < n; i++) {
    const s = hash(`r|${i}`);
    const px = x + ((s % w) + t * speed * (1 + (i % 3) * 0.3)) % w, py = y + ((s >>> 6) % h);
    const len = 2 + ((Math.floor(t * 1.5) + i) % 3) * 2;
    p.R(px, py, Math.min(len, x + w - px), 1, col);
  }
}

/** 水草、柳条：一根根竖线随风摆 */
export function sway(p: Painter, t: number, stalks: [number, number, number][], col: string) {
  stalks.forEach(([x, y, h], i) => {
    for (let k = 0; k < h; k += 2) p.R(x + Math.round(Math.sin(t * 1.6 + i + k / 5) * (k / h) * 2), y - k, 1, 2, col);
  });
}

/** 烟：从一个点往上飘散 */
export function smoke(p: Painter, t: number, x: number, y: number, col = 'rgba(232,228,220,.55)') {
  for (let i = 0; i < 5; i++) {
    const q = ((t * 0.5) + i / 5) % 1;
    p.alpha(0.6 * (1 - q)); p.R(x + Math.sin((t + i) * 1.3) * 3 * q, y - q * 22, 2 + q * 3, 2, col);
  }
  p.alpha(1);
}

export function sun(p: Painter, x: number, y: number) {
  p.R(x, y, 9, 9, '#F7E7B8'); p.R(x + 1, y - 1, 7, 11, '#F7E7B8'); p.R(x - 1, y + 1, 11, 7, '#F7E7B8');
}

/** 灯：灯芯一闪一闪，外面一圈暖光在呼吸 */
export function lamp(p: Painter, t: number, pts: [number, number][], col = '#FFF2B0') {
  pts.forEach(([x, y], i) => {
    const breath = 0.16 + 0.1 * Math.sin(t * 2.2 + i * 1.7) + (Math.floor(t * 7 + i) % 9 === 0 ? 0.12 : 0);
    p.alpha(breath); p.R(x - 3, y - 2, 7, 5, col); p.R(x - 2, y - 3, 5, 7, col);
    p.alpha(breath * 1.6); p.R(x - 1, y - 1, 3, 3, col);
    p.alpha(1);
    const k = Math.floor(t * 5 + i) % 3;
    p.R(x, y - (k === 2 ? 1 : 0), 1, 1, k ? col : '#FFFFFF');
  });
}

/** 显示器：屏幕里的内容在变，光标在闪，隔几秒整屏亮一下 */
export function screen(p: Painter, t: number, boxes: [number, number, number, number][], col = 'rgba(214,236,245,.9)') {
  boxes.forEach(([x, y, w, h], b) => {
    const step = Math.floor(t * 2.5) + b;
    const lines = Math.max(1, Math.floor(h / 2));
    for (let i = 0; i < lines; i++) {
      const len = 1 + ((step * 7 + i * 13 + b * 5) % Math.max(1, w - 2));
      p.alpha(0.55); p.R(x + 1, y + 1 + i * 2, len, 1, col);
    }
    p.alpha(1);
    if (Math.floor(t * 2) % 2 === 0) p.R(x + 1 + ((step * 3) % Math.max(1, w - 2)), y + h - 2, 1, 1, '#FFFFFF');
    if ((Math.floor(t * 10) + b * 11) % 37 === 0) { p.alpha(0.3); p.R(x, y, w, h, '#FFFFFF'); p.alpha(1); }
  });
}
