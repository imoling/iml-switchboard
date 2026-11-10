import { useCallback, useEffect, useRef, useState } from 'react';
import { api, subscribeEvents } from '../api/client';
import type { Call, Job, Onboarding as Ob, Quota, Status } from '../api/types';
import { restartBackend } from '../backend';
import { ToastHost, toast } from '../components/ui';
import { num, setMoney } from '../format';
import appMark from '../assets/app-mark.svg';
import { Mascot } from '../components/Pixel';
import { Accounts } from '../pages/Accounts';
import { Calls } from '../pages/Calls';
import { Integrations } from '../pages/Integrations';
import { Media } from '../pages/Media';
import { Onboarding } from '../pages/Onboarding';
import { Overview } from '../pages/Overview';
import { Rules } from '../pages/Rules';
import { Settings } from '../pages/Settings';
import { World } from '../pages/World';
import { useTheme } from '../theme/ThemeContext';
import { desktop, inTauri } from '../tauri';
import { About } from './About';
import { Offline } from './Offline';

type Page = 'overview' | 'world' | 'calls' | 'rules' | 'accounts' | 'media' | 'integrations' | 'settings';
const NAV: { id: Page; key: string }[] = [
  { id: 'overview', key: 'nav.overview' }, { id: 'calls', key: 'nav.calls' }, { id: 'rules', key: 'nav.rules' },
  { id: 'accounts', key: 'nav.accounts' }, { id: 'media', key: 'nav.media' }, { id: 'integrations', key: 'nav.integrations' },
];

export function Console() {
  const { t, setThemeId, setProjection, reload: reloadThemes } = useTheme();
  const [page, setPage] = useState<Page>('overview');
  const [status, setStatus] = useState<Status | null | 'offline'>(null);
  // 后台换了版本（重启成新构建）时词表可能变了，按构建号重新拉一次
  const build = typeof status === 'object' && status ? status.build : '';
  useEffect(() => { if (build) void reloadThemes(); }, [build, reloadThemes]);
  const [onboarding, setOnboarding] = useState<Ob | null>(null);
  const [liveCall, setLiveCall] = useState<Call | null>(null);
  const [liveJob, setLiveJob] = useState<Job | null>(null);
  const [todayCalls, setTodayCalls] = useState<number | null>(null);
  const [activeJobs, setActiveJobs] = useState<number>(0);
  const [about, setAbout] = useState(false);

  const boot = useCallback(async () => {
    try {
      const s = await api.status();
      setStatus(s); setMoney(s.currency, s.usd_per_cny); setThemeId(s.theme); setProjection(s.projection);
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
      if (e.type === 'status') { setStatus(e.data); setMoney(e.data.currency, e.data.usd_per_cny); setThemeId(e.data.theme); setProjection(e.data.projection); }
    }, (open) => {
      if (!open) { void desktop.setTrayStatus('off', '后台未运行'); return; }
      // 断线重连（后台重启）后先恢复成正常，再按额度决定要不要变黄；不然图标会一直停在灰色
      void desktop.setTrayStatus('ok', t('app.title'));
      void api.overview('today').then((o) => tray(o.quotas)).catch(() => undefined);
    });
    return off;
  }, [status === null || status === 'offline', setThemeId, setProjection, t]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { void desktop.setTitle(t('app.title')); }, [t]);
  const online = status !== null && status !== 'offline';
  // 更新后连着的还是旧后台：等手上的活做完，自动换成安装包里的那份；每次启动只试一次，失败了设置页和菜单栏弹窗里还能手动点
  const autoRestarted = useRef(false);
  useEffect(() => {
    if (!online || autoRestarted.current) return;
    autoRestarted.current = true;
    void (async () => {
      const bundled = await desktop.bundledBuild();
      if (!bundled) return;
      for (;;) {
        const s = await api.status().catch(() => null);
        if (!s?.build || s.build === bundled) return;
        if ((s.busy ?? 0) === 0) break;
        await new Promise((r) => setTimeout(r, 10000));
      }
      const why = await restartBackend();
      if (why) toast(`后台重启失败：${why}`);
      else void boot();
    })();
  }, [online, boot]);
  useEffect(() => { if (online) void api.settings().then((s) => { if (s.hotkey) void desktop.setHotkey(s.hotkey); void desktop.setNotch(!!s.lab_notch); void desktop.setTrayStyle(s.tray_icon ?? 'tile'); }).catch(() => undefined); }, [online]);

  if (status === null) return <div className="offline"><span className="hint">正在连接后台…</span></div>;
  if (status === 'offline') return <Offline onStarted={boot} />;
  if (onboarding && !onboarding.done) return <Onboarding state={onboarding} onDone={() => setOnboarding({ ...onboarding, done: true })} />;

  return (
    <div className="app">
      <aside className="side">
        <div className="drag" data-tauri-drag-region style={{ position: 'absolute', top: 0, left: 0, right: 0, height: 38 }} />
        <div className="brand"><img src={appMark} alt="" />{t('app.title')}</div>
        <hr />
        {NAV.map((n) => (
          <button key={n.id} className={`nav ${page === n.id ? 'on' : ''}`} onClick={() => setPage(n.id)}>
            <span>{t(n.key)}</span>
            {n.id === 'calls' && todayCalls !== null && <small>{num(todayCalls)}</small>}
            {n.id === 'media' && activeJobs > 0 && <small>{activeJobs}</small>}
          </button>
        ))}
        <hr />
        <button className={`nav ${page === 'settings' ? 'on' : ''}`} onClick={() => setPage('settings')}><span>{t('nav.settings')}</span></button>
        <button className={`nav ${page === 'world' ? 'on' : ''}`} onClick={() => setPage('world')}><span>{t('nav.world')}</span></button>
        <div className="spacer" />
        <div className="mascot"><Mascot scale={4} /></div>
        {status.paused && <div className="hint" style={{ padding: '0 10px 6px' }}>已{t('pause')}</div>}
        <button className="nav" style={{ color: '#6F6A61' }} onClick={() => (inTauri ? desktop.showAbout() : setAbout(true))}><span>关于</span><small>{status.version}</small></button>
      </aside>
      <main className="content">
        {/* 顶上的留白和各页的标题行都能拖窗口：标题栏隐藏了，右半边不留拖动区就只能从侧栏拖 */}
        <div className="drag" data-tauri-drag-region style={{ position: 'absolute', top: 0, left: 0, right: 0, height: 18 }} />
        {page === 'overview' && <Overview live={liveCall} onNavigate={setPage} />}
        {page === 'world' && <World paused={status.paused} onNavigate={setPage} />}
        {page === 'calls' && <Calls live={liveCall} />}
        {page === 'rules' && <Rules />}
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
