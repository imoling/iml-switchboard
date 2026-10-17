import { useState } from 'react';
import { desktop, inTauri } from '../tauri';

/** 后台不可达：整个窗口只说一件事 */
export function Offline({ onStarted, compact }: { onStarted: () => void; compact?: boolean }) {
  const [busy, setBusy] = useState(false);
  const start = async () => { setBusy(true); await desktop.startBackend(); setTimeout(() => { setBusy(false); onStarted(); }, 1500); };
  return (
    <div className={compact ? '' : 'offline'} style={compact ? { padding: '20px 0', textAlign: 'center' } : undefined}>
      <div><span className="dot no" />后台未运行</div>
      {inTauri
        ? <button className="btn pri" disabled={busy} onClick={start}>{busy ? '正在启动…' : '启动'}</button>
        : <div className="hint">在终端运行 imls serve，或 npm run mock</div>}
    </div>
  );
}
