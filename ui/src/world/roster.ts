/** 花名册：楼层上住谁、左边立几座信塔，全从真实配置来 */
import type { Account, Integration, Kind, Rules } from '../api/types';
import { HOST_LABEL, laneGroup } from '../format';
import { hostNodes } from '../topology';
import { type Floor, hash, type LaneGroup, type Tower, type Wizard } from './model';
import { TOWER_MAX } from './scene';

const TOWER_COLOR: Record<string, string> = {
  claude_code: '#C98A3B', claude_desktop: '#B5835A', codex: '#3A3935', workbuddy: '#7D8F5E', gemini_cli: '#4A6785',
  hermes: '#8C5A7A', openclaw: '#A4473A', winwork: '#3E7CB1', pi: '#6F6A61', doubao_work: '#3E6BB0', deepseek_harness: '#4D6BFE',
};
const SPARE = ['#8C5A7A', '#3E7CB1', '#A4473A', '#6F6A61', '#7D8F5E'];
export const OTHER_TOWER = 'other';

/** 脚下写得下的短名：去掉厂商和网关前缀，长了把常见长词缩写，再按词截；结尾括号里的「低」「高」留着好区分 */
const NAME_MAX = 13;
export function shortName(full: string): string {
  let s = full.replace(/^(Claude|Google|OpenAI|Anthropic|WinWork)\s+/i, '').trim();
  const tailM = s.match(/[（(][^）)]*[）)]$/);
  if (tailM) s = s.slice(0, -tailM[0].length).trim();
  // 括号里的「低」「高」改成空格后缀：全角括号太占地方，脚下挤不下
  const tail = tailM ? ` ${tailM[0].slice(1, -1)}` : '';
  const fits = (x: string) => x.length + tail.length <= NAME_MAX;
  if (!fits(s)) s = s.replace(/DeepSeek/g, 'DS').replace(/Gemini/g, 'Gem');
  if (!fits(s)) s = s.replace(/\s+(Flash|Preview|Latest|Instruct)$/i, '');
  if (!fits(s)) {
    let out = '';
    for (const w of s.split(/\s+/)) { const next = out ? `${out} ${w}` : w; if (!fits(next)) break; out = next; }
    s = out || s.slice(0, NAME_MAX - 1) + '…';
  }
  return s + tail;
}

const TIER_FLOOR: Record<string, Floor> = { low: 'low', mid: 'mid', high: 'high', top: 'top' };

/** 按档位候选表排楼层：同一个型号在好几档都有时住最高的那层 */
export function buildRoster(rules: Rules | null, accounts: Account[]): Wizard[] {
  const out: Wizard[] = [];
  const seen = new Set<string>();
  const add = (w: Wizard) => { if (!w.id || seen.has(w.id)) return; seen.add(w.id); out.push(w); };
  // 顶配：账号里配了 top 的型号
  for (const a of accounts) {
    if (a.status === 'disabled' || !a.enabled) continue;
    const m = a.tiers?.top;
    if (m) add({ id: m, name: shortName(a.labels?.[m] ?? m), full: a.labels?.[m] ?? m, lane: a.kind === 'subscription' ? 'sub' : 'api', floor: 'top', acct: a.id });
  }
  for (const tier of ['high', 'mid', 'low'] as const) {
    const table = rules?.tiers?.[tier];
    for (const p of table?.picks ?? []) {
      if (!p.on) continue;
      add({ id: p.model, name: shortName(p.name), full: p.name, lane: laneGroup(p.lane) as LaneGroup, floor: TIER_FLOOR[tier], acct: p.account_id || p.lane });
    }
  }
  // 媒体：出图的住画室，出视频的住魔药地窖
  for (const a of accounts) {
    if (a.status === 'disabled' || !a.enabled) continue;
    for (const m of a.media ?? []) add({ id: m.model, name: shortName(m.display), full: m.display, lane: 'media', floor: m.kind === 'image' ? 'paint' : 'potion', acct: a.id });
  }
  return out;
}

/** 花名册里没有的型号（应用点名、提档补进来的）：当场加一位 */
export function guestWizard(model: string, display: string | undefined, lane: string, kind: Kind, tier?: string): Wizard {
  const full = display || model;
  const floor: Floor = kind === 'image' ? 'paint' : kind === 'video' || kind === 'animation' ? 'potion' : TIER_FLOOR[tier ?? ''] ?? 'mid';
  return { id: model, name: shortName(full), full, lane: kind !== 'text' && lane === 'media' ? 'media' : (laneGroup(lane || 'api') as LaneGroup), floor, acct: lane };
}

export function towerName(id: string): string { return HOST_LABEL[id] ?? (id === OTHER_TOWER ? HOST_LABEL.mcp : id); }

export function towerColor(id: string): string { return TOWER_COLOR[id] ?? SPARE[hash(id) % SPARE.length]; }

/** 信塔：接入了的、近 7 天派过活的应用，最多四座；第五座留给「其他应用」 */
export function buildTowers(list: Integration[]): Tower[] {
  return hostNodes(list).slice(0, TOWER_MAX - 1).map((h) => ({ id: h.id, name: towerName(h.id), color: towerColor(h.id) }));
}

const otherTower = (): Tower => ({ id: OTHER_TOWER, name: towerName(OTHER_TOWER), color: '#8C867C' });

/** 这单从哪座塔出发：没有就新立一座；立满了的落到「其他应用」那座，不借别的应用的塔 */
export function towerFor(towers: Tower[], host: string | undefined): { id: string; add?: Tower } {
  const id = host || OTHER_TOWER;
  if (towers.some((t) => t.id === id)) return { id };
  if (towers.some((t) => t.id === OTHER_TOWER) || towers.length >= TOWER_MAX - 1) {
    if (towers.some((t) => t.id === OTHER_TOWER)) return { id: OTHER_TOWER };
    return { id: OTHER_TOWER, add: otherTower() };
  }
  return { id, add: id === OTHER_TOWER ? otherTower() : { id, name: towerName(id), color: towerColor(id) } };
}
