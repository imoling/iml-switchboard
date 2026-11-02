import { describe, expect, it } from 'vitest';
import { moveItem, sameList, splitModels, toggleId } from '../hostModels';
import type { HostModel } from '../api/types';

const models: HostModel[] = [
  { id: 'router-auto', name: 'Switchboard 自动分档', group: 'Switchboard', on: true, default: true },
  { id: 'router-high', name: 'Switchboard 高档', group: 'Switchboard', on: true, default: true },
  { id: 'gpt-5.5', name: 'GPT-5.5', group: 'ChatGPT 网关', on: true, default: true },
  { id: 'gpt-5.5-mini', name: 'GPT-5.5 mini', group: 'ChatGPT 网关', on: false },
  { id: 'winwork-snt-5.5', name: 'WinWork Sonnet 5.5', group: 'WinWork', on: false, default: true },
];

describe('宿主型号表', () => {
  it('挪动：越界不动，其余照挪', () => {
    expect(moveItem(['a', 'b', 'c'], 0, 2)).toEqual(['b', 'c', 'a']);
    expect(moveItem(['a', 'b', 'c'], 2, 1)).toEqual(['a', 'c', 'b']);
    expect(moveItem(['a', 'b', 'c'], 0, -1)).toEqual(['a', 'b', 'c']);
    expect(moveItem(['a', 'b', 'c'], 1, 3)).toEqual(['a', 'b', 'c']);
  });

  it('勾上排到末尾，取消移出', () => {
    expect(toggleId(['a'], 'b')).toEqual(['a', 'b']);
    expect(toggleId(['a', 'b'], 'a')).toEqual(['b']);
  });

  it('选中的按给的顺序，其余按来源分组且不重复', () => {
    const { chosen, rest } = splitModels(models, ['gpt-5.5', 'router-auto', 'ghost']);
    expect(chosen.map((m) => m.id)).toEqual(['gpt-5.5', 'router-auto']);
    expect(rest.map((r) => [r.group, r.items.map((m) => m.id)])).toEqual([
      ['Switchboard', ['router-high']],
      ['ChatGPT 网关', ['gpt-5.5-mini']],
      ['WinWork', ['winwork-snt-5.5']],
    ]);
  });

  it('同一个顺序才算没改', () => {
    expect(sameList(['a', 'b'], ['a', 'b'])).toBe(true);
    expect(sameList(['a', 'b'], ['b', 'a'])).toBe(false);
    expect(sameList(['a'], ['a', 'b'])).toBe(false);
  });
});
