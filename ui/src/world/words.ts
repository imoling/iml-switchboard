/**
 * 学院页不跟主题走的字：页面上的按钮、卡片，钱和报错的写法。
 * 每套主题自己的叫法（学徒还是偏将、猫头鹰还是信鸽）在 themes/*.ts 里。
 * 钱、额度、报错照样写人话：梗在前，原因在括号里。
 */

/** 旁白里说档位用术语表的叫法 */
export const TIER_TERM: Record<string, string> = { low: '低档', mid: '中档', high: '高档', top: '顶配' };
export const LANE_NAME = { sub: '订阅', api: '按量付费', media: '媒体' } as const;

/** 页面上的按钮、卡片、空状态 */
export const WORLD = {
  live: '实时', demo: '演示', play: '播放', pause: '暂停', close: '收起', redo: '重做', cancel: '算了', names: '名牌',
  redoTitle: (title: string) => `请${title}重做这一单？`,
  redoDesc: (name: string) => `${name} 这单记成判低了，按高一档再调一次，可能另外花钱`,
  redoFailed: '重做失败',
  replying: (app: string) => `正在回 ${app} 的活`,
  dragHint: '拖到更高的一层：请高一档重做',
  towerIdle: '这会儿没活',
  towerBusy: (n: number) => `${n} 件活在路上`,
  addModel: '添加模型',
  watchDemo: '看演示',
  personality: (faction: string, quirk: string) => `${faction} · ${quirk}`,
};

export const moneySaved = (s: string) => `省下 ${s}`;
export const moneySpent = (s: string) => `花 ${s}`;
export const SUB_FREE = '走订阅，不另花钱';

/** 失败原因压成两三个字，放进「梗（原因）」的括号里 */
export function shortWhy(err?: string): string {
  const s = (err ?? '').toLowerCase();
  if (/rate|limit|限流|额度|429|quota/.test(s)) return '限流';
  if (/timeout|超时/.test(s)) return '超时';
  if (/太长|context|装不下/.test(s)) return '太长';
  if (/余额|balance|insufficient/.test(s)) return '余额不足';
  if (/key|密钥|401|403|auth/.test(s)) return '密钥不对';
  return '出错';
}
