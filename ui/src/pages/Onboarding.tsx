import { useEffect, useState } from 'react';
import { api, ApiError } from '../api/client';
import type { Account, Decision, Onboarding as Ob } from '../api/types';
import { Dot, GroupChip, TierChip } from '../components/ui';
import { Vendor, VendorLogo } from '../components/Vendor';
import { useTheme } from '../theme/ThemeContext';
import { AddAccount } from './Accounts';

const TOTAL = 4;

export function Onboarding({ state, onDone }: { state: Ob; onDone: () => void }) {
  const { t } = useTheme();
  const firstUndone = state.steps.cli_detected ? state.steps.api_added ? state.steps.claude_connected ? 3 : 2 : 1 : 0;
  const [step, setStep] = useState(firstUndone);
  const next = () => (step + 1 >= TOTAL ? finish() : setStep(step + 1));
  const finish = async () => { try { await api.onboardingDone(); } catch { /* 后台没记上也先进主界面 */ } onDone(); };

  return (
    <div style={{ height: '100vh', overflow: 'auto' }}>
      <div className="onb">
        <div className="tb drag" data-tauri-drag-region>欢迎使用 {t('app.title')}</div>
        <div className="body">
          <div className="steps">{Array.from({ length: TOTAL }, (_, i) => <div key={i} className={i <= step ? 'on' : ''} />)}</div>
          <div className="hint">第 {step + 1} 步，共 {TOTAL} 步 · iML Studio 出品</div>
          {step === 0 && <StepCli onNext={next} />}
          {step === 1 && <StepApi onBack={() => setStep(0)} onNext={next} />}
          {step === 2 && <StepClaude onBack={() => setStep(1)} onNext={next} />}
          {step === 3 && <StepTry onBack={() => setStep(2)} onDone={finish} />}
        </div>
      </div>
    </div>
  );
}

function StepCli({ onNext }: { onNext: () => void }) {
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const detect = async () => { setErr(null); try { setAccounts((await api.detectAccounts()).filter((a) => a.kind === 'subscription')); } catch (e) { setErr((e as ApiError).message); } };
  useEffect(() => { void detect(); }, []);
  return (
    <>
      <h4>检测本机命令</h4>
      <p className="muted" style={{ margin: '0 0 12px' }}>找到的订阅账号会直接用上，登录信息不会被读取</p>
      {!accounts && !err && <div className="hint">正在检测…</div>}
      {err && <div className="hint err-text">{err}</div>}
      {accounts?.map((a) => (
        <div key={a.id} className="acct"><VendorLogo provider={a.provider} /><div className="grow"><b>{a.label}</b><div className="hint">{a.detail}</div></div><span><Dot s={a.status === 'ok' ? 'ok' : a.status === 'warn' ? 'warn' : 'no'} />{a.status === 'ok' ? '可用' : a.status === 'warn' ? '可用' : '未连接'}</span></div>
      ))}
      <div className="nav"><span /><span><button className="btn" style={{ marginRight: 6 }} onClick={detect}>重新检测</button><button className="btn pri" onClick={onNext}>继续</button></span></div>
    </>
  );
}

function StepApi({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
  return (
    <>
      <h4>添加一个按量付费账号</h4>
      <p className="muted" style={{ margin: '0 0 12px' }}>杂活会交给它，通常每月几块钱</p>
      <AddAccount embedded onAdded={onNext} onSkip={onNext} />
      <div style={{ marginTop: -30 }}><button className="btn" onClick={onBack}>上一步</button></div>
    </>
  );
}

function StepClaude({ onBack, onNext }: { onBack: () => void; onNext: () => void }) {
  const { t } = useTheme();
  const [busy, setBusy] = useState(false);
  const [done, setDone] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const connect = async () => { setBusy(true); setErr(null); try { await api.connect('claude_code', { write_claude_md: true }); setDone(true); } catch (e) { setErr((e as ApiError).message); } finally { setBusy(false); } };
  return (
    <>
      <h4>接入 Claude Code</h4>
      <p className="muted" style={{ margin: '0 0 12px' }}>之后 Claude Code 里就能生图、生视频、把杂活交给便宜{t('model')}</p>
      <div className="acct"><VendorLogo provider="claude_code" /><div className="grow"><b>Claude Code</b><div className="hint">会加一条调用约定到全局 CLAUDE.md</div></div><span><Dot s={done ? 'ok' : 'no'} />{done ? '已接入' : '未接入'}</span>{!done && <button className="btn pri sm" disabled={busy} onClick={connect}>{busy ? '正在接入…' : '接入'}</button>}</div>
      {err && <div className="hint err-text">{err}</div>}
      <div className="nav"><button className="btn" onClick={onBack}>上一步</button><span>{!done && <button className="btn" style={{ marginRight: 6 }} onClick={onNext}>跳过</button>}<button className="btn pri" disabled={!done} onClick={onNext}>继续</button></span></div>
    </>
  );
}

function StepTry({ onBack, onDone }: { onBack: () => void; onDone: () => void }) {
  const [text, setText] = useState('');
  const [d, setD] = useState<Decision | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const run = async () => { if (!text.trim()) return; setErr(null); try { setD(await api.tryRule({ prompt: text.trim() })); } catch (e) { setErr((e as ApiError).message); } };
  const first = d?.candidates.find((c) => !c.skipped) ?? d?.candidates[0];
  return (
    <>
      <h4>试一句</h4>
      <p className="muted" style={{ margin: '0 0 12px' }}>只看会派给谁，不真的发出去</p>
      <div className="row"><input className="input" placeholder="帮我把这段日志按错误类型归类" value={text} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => e.key === 'Enter' && run()} autoFocus /><button className="btn" onClick={run}>看看</button></div>
      {err && <div className="hint err-text mt8">{err}</div>}
      {d && first && <div className="mt12" style={{ fontSize: 12.5 }}>命中 <b>{d.task_type}</b> → <TierChip tier={d.tier} /> → <GroupChip group={first.lane === 'media' ? 'media' : first.lane.endsWith('_sub') ? 'sub' : 'api'} /> <Vendor provider={first.provider} />{first.model}</div>}
      <div className="nav"><button className="btn" onClick={onBack}>上一步</button><button className="btn pri" onClick={onDone}>开始使用</button></div>
    </>
  );
}
