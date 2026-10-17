import { useEffect, useState } from 'react';
import appIcon from '../assets/app-icon.svg';
import { useTheme } from '../theme/ThemeContext';
import { desktop } from '../tauri';

export function About({ version: given }: { version?: string }) {
  const { t } = useTheme();
  const [version, setVersion] = useState(given ?? '');
  useEffect(() => { if (!given) void desktop.version().then(setVersion); }, [given]);
  useEffect(() => { void desktop.setTitle(`关于 ${t('app.title')}`); }, [t]);
  return (
    <div className="about drag" data-tauri-drag-region>
      <img src={appIcon} alt="" />
      <div className="name">{t('app.title')}</div>
      <div className="hint">iML Switchboard · 版本 {version}</div>
      <div style={{ margin: '12px 0 4px' }}>iML Studio 出品</div>
      <div className="hint">© 2026 imoling · imoling.cn@gmail.com</div>
    </div>
  );
}
