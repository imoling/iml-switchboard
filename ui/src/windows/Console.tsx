import { useCallback, useEffect, useState } from 'react';
import { api, subscribeEvents } from '../api/client';
import type { Call, Job, Onboarding as Ob, Quota, Status } from '../api/types';
import { ToastHost } from '../components/ui';
import appIcon from '../assets/app-icon.svg';
import { Accounts } from '../pages/Accounts';
import { Calls } from '../pages/Calls';
import { Evaluation } from '../pages/Evaluation';
import { Integrations } from '../pages/Integrations';
import { Media } from '../pages/Media';
import { Onboarding } from '../pages/Onboarding';
import { Overview } from '../pages/Overview';
import { Rules } from '../pages/Rules';
import { Settings } from '../pages/Settings';
import { useTheme } from '../theme/ThemeContext';
import { desktop, inTauri } from '../tauri';
import { About } from './About';
import { Offline } from './Offline';

type Page = 'overview' | 'calls' | 'rules' | 'eval' | 'accounts' | 'media' | 'integrations' | 'settings';
const NAV: { id: Page; key: string }[] = [
  { id: 'overview', key: 'nav.overview' }, { id: 'calls', key: 'records' }, { id: 'rules', key: 'nav.rules' }, { id: 'eval', key: 'nav.eval' },
  { id: 'accounts', key: 'nav.accounts' }, { id: 'media', key: 'nav.media' }, { id: 'integrations', key: 'nav.integrations' },
];

export function Console() {
  const { t, setThemeId, setProjection } = useTheme();
  const [page, setPage] = useState<Page>('overview');
  const [status, setStatus] = useState<Status | null | 'offline'>(null);
  const [onboarding, setOnboarding] = useState<Ob | null>(null);
  const [liveCall, setLiveCall] = useState<Call | null>(null);
  const [liveJob, setLiveJob] = useState<Job | null>(null);
  const [todayCalls, setTodayCalls] = useState<number | null>(null);
  const [activeJobs, setActiveJobs] = useState<number>(0);
  const [about, setAbout] = useState(false);

  const boot = useCallback(async () => {
    try {
      const s = await api.status();
      setStatus(s); setThemeId(s.theme); setProjection(s.projection);
      const [ob, ov, media] = await Promise.all([api.onboarding().catch(() => null), api.overview('today').catch(() => null), api.media('all').catch(() => null)]);
      if (ob) setOnboarding(ob);
      if (ov) setTodayCalls(ov.calls);
      if (media) setActiveJobs(media.jobs.filter((j) => j.status === 'running' || j.status === 'queued' || j.status === 'submitted').length);
    } catch { setStatus('offline'); }
  }, [setThemeId, setProjection]);
  useEffect(() => { void boot(); }, [boot]);

  // 后台没起来：每 3 秒探一次
  useEffect(() => {
    if (status !== 'offline') return;
    const id = setInterval(() => void boot(), 3000);
    return () => clearInterval(id);
  }, [status, boot]);

  // 事件流：主窗口是唯一发系统通知和改托盘状态的地方
  useEffect(() => {
    if (status === null || status === 'offline') return;
    const tray = (quotas: Quota[]) => {
      const hot = quotas.some((q) => !q.sub && q.pct >= 80);
      void desktop.setTrayStatus(hot ? 'warn' : 'ok', hot ? '额度紧张' : t('app.title'));
    };
    const off = subscribeEvents((e) => {
      if (e.type === 'notice') void desktop.notify(e.data.title, e.data.body);
      if (e.type === 'call') { setLiveCall(e.data.call); setTodayCalls((n) => (n === null ? n : n + 1)); }
      if (e.type === 'job') { setLiveJob(e.data.job); void api.media('all').then((m) => setActiveJobs(m.jobs.filter((j) => j.status === 'running' || j.status === 'queued' || j.status === 'submitted').length)).catch(() => {}); }
      if (e.type === 'quota') tray(e.data.quotas);
      if (e.type === 'status') { setStatus(e.data); setThemeId(e.data.theme); setProjection(e.data.projection); }
    }, (open) => { if (!open) void desktop.setTrayStatus('off', '后台未运行'); });
    return off;
  }, [status === null || status === 'offline', setThemeId, setProjection, t]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { void desktop.setTitle(t('app.title')); }, [t]);

  if (status === null) return <div className="offline"><span className="hint">正在连接后台…</span></div>;
  if (status === 'offline') return <Offline onStarted={boot} />;
  if (onboarding && !onboarding.done) return <Onboarding state={onboarding} onDone={() => setOnboarding({ ...onboarding, done: true })} />;

  return (
    <div className="app">
      <aside className="side">
        <div className="drag" data-tauri-drag-region style={{ position: 'absolute', top: 0, left: 0, right: 0, height: 38 }} />
        <div className="brand"><img src={appIcon} alt="" />{t('app.title')}</div>
        <hr />
        {NAV.map((n) => (
          <button key={n.id} className={`nav ${page === n.id ? 'on' : ''}`} onClick={() => setPage(n.id)}>
            <span>{t(n.key)}</span>
            {n.id === 'calls' && todayCalls !== null && <small>{todayCalls}</small>}
            {n.id === 'media' && activeJobs > 0 && <small>{activeJobs}</small>}
          </button>
        ))}
        <hr />
        <button className={`nav ${page === 'settings' ? 'on' : ''}`} onClick={() => setPage('settings')}><span>{t('nav.settings')}</span></button>
        <div className="spacer" />
        {status.paused && <div className="hint" style={{ padding: '0 10px 6px' }}>已{t('pause')}</div>}
        <button className="nav" style={{ color: '#6F6A61' }} onClick={() => (inTauri ? desktop.showAbout() : setAbout(true))}><span>关于</span><small>{status.version}</small></button>
      </aside>
      <main className="content">
        {page === 'overview' && <Overview />}
        {page === 'calls' && <Calls live={liveCall} />}
        {page === 'rules' && <Rules />}
        {page === 'eval' && <Evaluation />}
        {page === 'accounts' && <Accounts />}
        {page === 'media' && <Media liveJob={liveJob} />}
        {page === 'integrations' && <Integrations />}
        {page === 'settings' && <Settings />}
      </main>
      <ToastHost />
      {about && <div className="modal-bg" onMouseDown={() => setAbout(false)}><div className="modal" style={{ width: 360 }}><About version={status.version} /></div></div>}
    </div>
  );
}
