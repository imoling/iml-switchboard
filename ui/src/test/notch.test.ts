import { describe, expect, it } from 'vitest';
import { NOTCH_IDLE, notchDone, notchDrop, notchStart, notchTone } from '../notch';
import { earX, routePoint } from '../notchFx';

const a = { id: 'a', host: 'codex', lane: 'api', provider: 'agnes' } as const;
const b = { id: 'b', host: 'claude_code', lane: 'claude_sub', provider: 'anthropic' } as const;

describe('刘海动画', () => {
  it('开始后显示这个请求，完成后停在它身上等收回', () => {
    const s = notchStart(NOTCH_IDLE, a);
    expect(s.active).toHaveLength(1);
    const d = notchDone(s, { ...a, ok: true });
    expect(d.active).toHaveLength(0);
    expect(d.shown?.provider).toBe('agnes');
  });

  it('换候选时不多算一个请求，右耳换成新渠道', () => {
    const s = notchStart(notchStart(NOTCH_IDLE, a), { ...a, lane: 'claude_sub', provider: 'anthropic' });
    expect(s.active).toHaveLength(1);
    expect(s.shown?.provider).toBe('anthropic');
  });

  it('并发时一个做完，接着显示还在路上的', () => {
    const s = notchDone(notchStart(notchStart(NOTCH_IDLE, a), b), { ...b, ok: true });
    expect(s.active.map((x) => x.id)).toEqual(['a']);
    expect(s.shown?.id).toBe('a');
  });

  it('失败只在全部结束时变红：别的请求还在跑就不抢', () => {
    const both = notchStart(notchStart(NOTCH_IDLE, a), b);
    expect(notchDone(both, { ...b, ok: false }).ok).toBe(true);
    expect(notchDone(notchStart(NOTCH_IDLE, a), { ...a, ok: false }).ok).toBe(false);
  });

  it('没有开始事件的失败（路由就没选到渠道）也要有东西显示', () => {
    const s = notchDone(NOTCH_IDLE, { id: 'x', host: 'codex', lane: '' as never, provider: '', ok: false });
    expect(s.shown?.host).toBe('codex');
    expect(s.ok).toBe(false);
  });

  it('颜色分得清渠道，失败优先', () => {
    expect(new Set([notchTone(true, 'claude_sub'), notchTone(true, 'api'), notchTone(true, 'media'), notchTone(false, 'api')]).size).toBe(4);
    expect(notchTone(false, 'media')).toBe(notchTone(false, 'api'));
  });

  it('粒子路线从左耳出发，经刘海下方，到右耳结束', () => {
    const g = { width: 208, height: 37.5, centerX: 200, band: 13, logoY: 18.75 };
    expect(routePoint(g, 0).x).toBeCloseTo(earX(g, -1));
    expect(routePoint(g, 0).y).toBeCloseTo(32.75);
    expect(routePoint(g, 1).x).toBeCloseTo(earX(g, 1));
    expect(routePoint(g, 1).y).toBeCloseTo(32.75);
    const mid = routePoint(g, 0.5);
    expect(mid.x).toBeCloseTo(200);
    expect(mid.y).toBeGreaterThan(g.height); // 刘海本身没有像素，中段必须走在它下面
  });

  it('完成事件丢了也能收回', () => {
    const s = notchDrop(notchStart(NOTCH_IDLE, a), 'a');
    expect(s.active).toHaveLength(0);
  });
});
