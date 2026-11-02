// 宿主型号表的纯函数：选中的那部分按顺序排前面，没选的按来源分组跟在后面；第一个是宿主的默认型号

import type { HostModel } from './api/types';

/** 把第 from 个挪到第 to 个；越界原样返回 */
export function moveItem<T>(list: T[], from: number, to: number): T[] {
  if (from === to || from < 0 || to < 0 || from >= list.length || to >= list.length) return list;
  const out = list.slice();
  const [x] = out.splice(from, 1);
  out.splice(to, 0, x);
  return out;
}

/** 勾上就排到选中的末尾，取消就移出 */
export function toggleId(selected: string[], id: string): string[] {
  return selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id];
}

/** 选中的按 selected 的顺序；其余按来源分组，保持后台给的顺序 */
export function splitModels(models: HostModel[], selected: string[]): { chosen: HostModel[]; rest: { group: string; items: HostModel[] }[] } {
  const byId = new Map(models.map((m) => [m.id, m]));
  const chosen = selected.map((id) => byId.get(id)).filter((m): m is HostModel => !!m);
  const rest: { group: string; items: HostModel[] }[] = [];
  for (const m of models) {
    if (selected.includes(m.id)) continue;
    const g = rest.find((r) => r.group === m.group);
    if (g) g.items.push(m); else rest.push({ group: m.group, items: [m] });
  }
  return { chosen, rest };
}

/** 两张表是不是同一个顺序 */
export function sameList(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((x, i) => x === b[i]);
}
