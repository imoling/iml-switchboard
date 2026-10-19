import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { api } from '../api/client';
import type { Theme, ThemeId } from '../api/types';
import { DEFAULT_WORDS } from './defaults';

interface ThemeCtx {
  themes: Theme[];
  themeId: ThemeId;          // 后台记录的主题
  effectiveId: ThemeId;      // 投屏模式下强制 standard
  projection: boolean;
  t: (key: string, vars?: Record<string, string | number>) => string;
  setThemeId: (id: ThemeId) => void;
  setProjection: (on: boolean) => void;
  reload: () => Promise<void>;
}

const Ctx = createContext<ThemeCtx | null>(null);

function fill(s: string, vars?: Record<string, string | number>) {
  if (!vars) return s;
  return s.replace(/\{(\w+)\}/g, (_, k) => (k in vars ? String(vars[k]) : `{${k}}`));
}

export function ThemeProvider({ children, initialTheme = 'dianjiang', initialProjection = false }: { children: ReactNode; initialTheme?: ThemeId; initialProjection?: boolean }) {
  const [themes, setThemes] = useState<Theme[]>([]);
  const [themeId, setThemeId] = useState<ThemeId>(initialTheme);
  const [projection, setProjection] = useState(initialProjection);

  const reload = useCallback(async () => {
    try { setThemes(await api.themes()); } catch { /* 后台没起：用代码兜底词 */ }
  }, []);
  useEffect(() => { void reload(); }, [reload]);
  // 后台还没起来时取不到词表；每 3 秒再试，直到拿到为止
  useEffect(() => {
    if (themes.length) return;
    const id = setInterval(() => void reload(), 3000);
    return () => clearInterval(id);
  }, [themes.length, reload]);

  const effectiveId: ThemeId = projection ? 'standard' : themeId;
  const t = useMemo(() => {
    const cur = themes.find((x) => x.id === effectiveId)?.words ?? {};
    const std = themes.find((x) => x.id === 'standard')?.words ?? {};
    return (key: string, vars?: Record<string, string | number>) => fill(cur[key] ?? std[key] ?? DEFAULT_WORDS[key] ?? key, vars);
  }, [themes, effectiveId]);

  const value = useMemo<ThemeCtx>(() => ({ themes, themeId, effectiveId, projection, t, setThemeId, setProjection, reload }), [themes, themeId, effectiveId, projection, t, reload]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useTheme(): ThemeCtx {
  const c = useContext(Ctx);
  if (!c) throw new Error('ThemeProvider missing');
  return c;
}
