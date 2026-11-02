import { describe, expect, it } from 'vitest';
import type { Account, Call, Integration } from '../api/types';
import { callColor, channelNodes, channelOf, hostNodes, linkPoint, linkWidth, modelCount } from '../topology';

const acct = (id: string, kind: Account['kind'], provider: string, models: string[] = [], status: Account['status'] = 'ok') =>
  ({ id, kind, provider, label: id, status, detail: '', models }) as Account;
const accounts = [
  acct('agnes-1', 'api', 'agnes', ['agnes-3.0-flash']),
  acct('claude_sub', 'subscription', 'anthropic', ['sonnet', 'opus']),
  acct('codex_sub', 'subscription', 'openai', ['gpt-5.6-sol']),
  acct('winwork', 'api', 'winwork', ['winwork-snt-5.5', 'sonnet']),
  acct('off', 'api', 'zhipu', ['glm-5.3'], 'disabled'),
];
const call = (over: Partial<Call>) => ({ id: 'c', ok: true, lane: 'api', provider: 'agnes', ...over }) as Call;

describe('全貌图', () => {
  it('订阅排在前面，停用的账号不算', () => {
    expect(channelNodes(accounts).map((a) => a.id)).toEqual(['claude_sub', 'codex_sub', 'agnes-1', 'winwork']);
  });

  it('型号去重，停用账号的型号不算托管', () => {
    expect(modelCount(accounts)).toBe(5);
  });

  it('宿主按派活量排序；没接入但派过活的也要有节点', () => {
    const list = [
      { id: 'pi', status: 'connected', calls_7d: 0 }, { id: 'codex', status: 'connected', calls_7d: 46 },
      { id: 'hermes', status: 'disconnected', calls_7d: 3 }, { id: 'gemini_cli', status: 'disconnected', calls_7d: 0 },
      { id: 'doubao_work', status: 'manual' },
    ] as Integration[];
    expect(hostNodes(list).map((h) => h.id)).toEqual(['codex', 'hermes', 'pi']);
  });

  it('订阅按渠道找账号，按量付费和媒体按厂商找', () => {
    expect(channelOf(call({ lane: 'claude_sub', provider: 'anthropic' }), accounts)?.id).toBe('claude_sub');
    expect(channelOf(call({ lane: 'api', provider: 'winwork' }), accounts)?.id).toBe('winwork');
    expect(channelOf(call({ lane: 'media', provider: 'agnes' }), accounts)?.id).toBe('agnes-1');
    expect(channelOf(call({ lane: 'media', provider: 'codex' }), accounts)?.id).toBe('codex_sub');
  });

  it('路由失败没选到渠道时不指向任何节点', () => {
    expect(channelOf(call({ lane: '' as Call['lane'], provider: '' }), accounts)).toBeNull();
    expect(channelOf(call({ provider: '已经删掉的账号' }), accounts)).toBeNull();
  });

  it('光点颜色跟图例一致，失败优先', () => {
    expect(callColor(call({ lane: 'claude_sub' }))).toBe('var(--accent)');
    expect(callColor(call({ lane: 'api' }))).toBe('var(--blue)');
    expect(callColor(call({ lane: 'media' }))).toBe('var(--green)');
    expect(callColor(call({ lane: 'media', ok: false }))).toBe('var(--red)');
  });

  it('光点沿连线走：两端对上节点，正中在两点中间', () => {
    const l = { x1: 100, y1: 20, x2: 300, y2: 120 };
    expect(linkPoint(l, 0)).toEqual({ x: 100, y: 20 });
    expect(linkPoint(l, 1)).toEqual({ x: 300, y: 120 });
    expect(linkPoint(l, 0.5)).toEqual({ x: 200, y: 70 });
  });

  it('连线粗细有上限，没派活时是最细的', () => {
    expect(linkWidth(0, 0)).toBe(1);
    expect(linkWidth(0, 46)).toBe(1);
    expect(linkWidth(46, 46)).toBe(3);
  });
});
