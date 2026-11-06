/** 五套主题的登记处：界面的风格主题 → 学院页的画面 */
import type { ThemeId } from '../../api/types';
import type { ThemeDef, ThemeKey } from '../theme';
import { ACADEMY } from './academy';
import { CAMP } from './camp';
import { HAREM } from './harem';
import { OFFICE } from './office';
import { SEA } from './sea';

export const THEMES: Record<ThemeKey, ThemeDef> = { academy: ACADEMY, camp: CAMP, office: OFFICE, harem: HAREM, sea: SEA };

const BY_UI: Record<ThemeId, ThemeKey> = { standard: 'office', dianjiang: 'camp', academy: 'academy', harem: 'harem', seaking: 'sea' };

/** 投屏模式下界面主题已经是 standard，画面就跟着成写字楼 */
export function themeFor(id: ThemeId): ThemeDef { return THEMES[BY_UI[id] ?? 'academy']; }
