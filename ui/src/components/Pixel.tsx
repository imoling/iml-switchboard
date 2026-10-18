import { useEffect, useState } from 'react';
import type { ThemeId, Tier } from '../api/types';
import { useTheme } from '../theme/ThemeContext';

/**
 * 像素小人：全部在代码里画，不依赖外部素材。
 * 字符表：. 透明  K 墨  S 肤  H 发  C 衣  D 衣暗  G 金  Y 浅金  R 红  W 白  P 粉  J 玉  B 蓝  T 陶土  N 灰  L 浅灰
 */
const BASE: Record<string, string> = {
  K: '#1C1B19', S: '#F3D5B5', H: '#2B2420', G: '#D9A441', Y: '#F0D080', R: '#B3261E', W: '#FFFFFF',
  P: '#E8A0B4', J: '#5F7A4A', B: '#4A6785', T: '#C4613A', N: '#A39E94', L: '#D8D3C8', O: '#E07A2E',
};

export interface SpriteDef { map: string[]; palette: Record<string, string>; eyes?: { row: number; from: string; to: string } }

const person = (head0: string, head1: string, row8: string, C: string, D: string, extra?: Partial<Record<number, string>>): SpriteDef => {
  const map = [
    head0, head1,
    '...HSSSSH...',
    '...HKSSKH...',
    '....SSSS....',
    '.....SS.....',
    '...CCCCCC...',
    '..CCCWWCCC..',
    row8,
    '..CCCCCCCC..',
    '..CCCCCCCC..',
    '..CCCCCCCC..',
    '..CCCCCCCC..',
    '..DDDDDDDD..',
    '..CCCCCCCC..',
    '..KK....KK..',
  ];
  if (extra) for (const [i, row] of Object.entries(extra)) map[Number(i)] = row!;
  return { map, palette: { ...BASE, C, D }, eyes: { row: 3, from: 'K', to: 'S' } };
};

const soldier = (rows: Partial<Record<number, string>>, C: string, D: string, helmet = 'N'): SpriteDef => {
  const map = [
    '....NNNN....',
    '...NNNNNN...',
    '...NSSSSN...',
    '...NKSSKN...',
    '....SSSS....',
    '.....SS.....',
    '..CCCCCCCC..',
    '.CCDCCCCDCC.',
    '.CCCCGGCCCC.',
    '..CCCCCCCC..',
    '..CCCCCCCC..',
    '..DDDDDDDD..',
    '..CC....CC..',
    '..CC....CC..',
    '..KK....KK..',
    '............',
  ].map((r) => r.replaceAll('N', helmet));
  for (const [i, row] of Object.entries(rows)) map[Number(i)] = row!;
  return { map, palette: { ...BASE, C, D, N: '#8C8A84' }, eyes: { row: 3, from: 'K', to: 'S' } };
};

const fish = (top: string[], C: string, D: string): SpriteDef => ({
  map: [
    ...top,
    '......CCCC....',
    '....CCCCCCC...',
    '...CCCCKWCCC..',
    '.D.CCCCCCCCCC.',
    '.DDCCCCCCCCCC.',
    '.D.CCCCCCCCCC.',
    '...CCCCCCCC...',
    '....CCCCCC....',
    '......DD......',
  ],
  palette: { ...BASE, C, D },
  eyes: { row: top.length + 2, from: 'KW', to: 'CC' },
});

const NONE3 = ['..............', '..............', '..............'];

const TIER_SPRITES: Record<Exclude<ThemeId, 'standard'>, Record<Tier, SpriteDef>> = {
  dianjiang: {
    low: soldier({}, '#8C8A84', '#5C5A55'),
    mid: soldier({ 0: '....RRRR....' }, '#4A6785', '#2E4458'),
    high: soldier({ 0: '...RRRRRR...', 1: '..NNNNNNNN..', 6: '..CCCCCCCCR.', 7: '.CCDCCCCDCRR', 8: '.CCCCGGCCCRR', 9: '..CCCCCCCCR.', 10: '..CCCCCCCCR.' }, '#C4613A', '#9A4525'),
    top: soldier({ 0: '...RGGGGR.RK', 1: '...GGGGGGRRK', 2: '...GSSSSG.RK', 3: '...GKSSKG..K', 4: '....SSSS...K', 5: '.....SS....K' }, '#D9A441', '#B3261E', 'G'),
  },
  harem: {
    low: person('....HHHH....', '...HHHHHH...', '..CCCCCCCC..', '#BFCBD8', '#8FA2B8'),
    mid: person('..GGHHHH....', '...HHHHHH...', '..CCCGGCCC..', '#E8A0B4', '#C97A94'),
    high: person('..G.HHHH.G..', '..GGHHHHGG..', '..CCGGGGCC..', '#C4613A', '#9A4525'),
    top: person('..GYGYYGYG..', '..GGGGGGGG..', '..CCRRRRCC..', '#D9A441', '#B3261E'),
  },
  seaking: {
    low: fish(NONE3, '#A39E94', '#7C786F'),
    mid: fish(['..............', '..............', '..........P...'], '#E8A0B4', '#C97A94'),
    high: fish(['..............', '......G.G.G...', '......GGGGG...'], '#D9A441', '#B3261E'),
    top: fish(['.....RR.RR....', '.....RRRRR....', '......RRR.....'], '#B3261E', '#7A1A14'),
  },
};

// 吉祥物：后宫是端着牌子的敬事房太监，点将台是主帅，海王是戴金冠、手持鱼叉的海王
const MASCOTS: Record<Exclude<ThemeId, 'standard'>, SpriteDef> = {
  dianjiang: TIER_SPRITES.dianjiang.top,
  harem: person('....KKKK....', '...KKKKKK...', '..J.J.J.J...', '#4A6785', '#2E4458', { 9: '.GGGGGGGGGG.' }),
  seaking: {
    map: [
      '.G.G.G...G.G',
      '.GGGGG...GGG',
      '.HSSSSH...G.',
      '.HKSSKH...G.',
      '..SSSS....G.',
      '..LLLL....G.',
      '..CCCCCC..G.',
      '..CCCCCC.SG.',
      '..CCCWWC.SG.',
      '..CCCCCC..G.',
      '..CCCCCC..G.',
      '..CCCCCC..G.',
      '..DDDDDD..G.',
      '..CC..CC..G.',
      '..CC..CC..G.',
      '..KK..KK..G.',
    ],
    palette: { ...BASE, C: '#3F8F8A', D: '#2C6B67', H: '#D8D3C8' },
    eyes: { row: 3, from: 'K', to: 'S' },
  },
};

// 媒体小人：生图是举画笔的画师，生视频是扛摄像机的影师；配色跟各主题走
const brush = { 4: '....SSSS...R', 5: '.....SS....R', 6: '...CCCCCC..K', 7: '..CCCWWCCC.K', 8: '..CCCCCCCS.K', 9: '..CCCCCCCCK.' };
const camera = { 6: '..CCCCCC.KKK', 7: '..CCCWWC.KWK', 8: '..CCCCCCSKKK' };
const MEDIA_SPRITES: Record<Exclude<ThemeId, 'standard'>, Record<'image' | 'video', SpriteDef>> = {
  dianjiang: {
    image: person('....HHHH....', '...HHHHHH...', '..CCCCCCCC..', '#5F7A4A', '#3E5230', brush),
    video: person('....HHHH....', '...HHHHHH...', '..CCCCCCCC..', '#5F7A4A', '#3E5230', camera),
  },
  harem: {
    image: person('..G.HHHH....', '...HHHHHH...', '..CCCCCCCC..', '#8FA88A', '#5F7A4A', brush),
    video: person('..G.HHHH....', '...HHHHHH...', '..CCCCCCCC..', '#8FA88A', '#5F7A4A', camera),
  },
  seaking: {
    image: person('....HHHH....', '...HHHHHH...', '..CCCCCCCC..', '#3F8F8A', '#2C6B67', brush),
    video: person('....HHHH....', '...HHHHHH...', '..CCCCCCCC..', '#3F8F8A', '#2C6B67', camera),
  },
};

export function mediaSprite(theme: ThemeId, kind: 'image' | 'video'): SpriteDef | null {
  if (theme === 'standard') return null;
  return MEDIA_SPRITES[theme][kind];
}

export function tierSprite(theme: ThemeId, tier: Tier | null | undefined): SpriteDef | null {
  if (theme === 'standard' || !tier) return null;
  return TIER_SPRITES[theme][tier];
}

export function mascotSprite(theme: ThemeId): SpriteDef | null {
  if (theme === 'standard') return null;
  return MASCOTS[theme];
}

/** 把字符地图画成 SVG 方块，像素边缘不抗锯齿 */
export function Sprite({ def, scale = 2, blink = false, className, title }: { def: SpriteDef; scale?: number; blink?: boolean; className?: string; title?: string }) {
  const rows = def.map;
  const cols = rows[0].length;
  const rects: { x: number; y: number; c: string }[] = [];
  rows.forEach((row, y) => {
    let line = row;
    if (blink && def.eyes && y === def.eyes.row) {
      for (let i = 0; i < def.eyes.from.length; i++) line = line.replaceAll(def.eyes.from[i], def.eyes.to[i] ?? def.eyes.to[0]);
    }
    for (let x = 0; x < line.length; x++) {
      const ch = line[x];
      if (ch === '.') continue;
      const c = def.palette[ch];
      if (c) rects.push({ x, y, c });
    }
  });
  return (
    <svg className={className} width={cols * scale} height={rows.length * scale} viewBox={`0 0 ${cols} ${rows.length}`} shapeRendering="crispEdges" aria-hidden={title ? undefined : true} role={title ? 'img' : undefined}>
      {title && <title>{title}</title>}
      {rects.map((r, i) => <rect key={i} x={r.x} y={r.y} width={1} height={1} fill={r.c} />)}
    </svg>
  );
}

/** 当前主题的档位小人；标准主题不显示 */
export function TierSprite({ tier, scale = 1 }: { tier: Tier | null | undefined; scale?: number }) {
  const { effectiveId } = useTheme();
  const def = tierSprite(effectiveId, tier);
  if (!def) return null;
  return <Sprite def={def} scale={scale} className="px" />;
}

/** 当前主题的媒体小人；标准主题不显示 */
export function MediaSprite({ kind, scale = 1 }: { kind: 'image' | 'video'; scale?: number }) {
  const { effectiveId } = useTheme();
  const def = mediaSprite(effectiveId, kind);
  if (!def) return null;
  return <Sprite def={def} scale={scale} className="px" />;
}

/** 吉祥物：轻微上下浮动，隔几秒眨一次眼 */
export function Mascot({ scale = 3, className }: { scale?: number; className?: string }) {
  const { effectiveId } = useTheme();
  const [blink, setBlink] = useState(false);
  useEffect(() => {
    let alive = true;
    let t2: ReturnType<typeof setTimeout>;
    const tick = () => {
      if (!alive) return;
      setBlink(true);
      t2 = setTimeout(() => { setBlink(false); schedule(); }, 140);
    };
    let t1: ReturnType<typeof setTimeout>;
    const schedule = () => { t1 = setTimeout(tick, 2500 + Math.random() * 3000); };
    schedule();
    return () => { alive = false; clearTimeout(t1); clearTimeout(t2); };
  }, []);
  const def = mascotSprite(effectiveId);
  if (!def) return null;
  return <Sprite def={def} scale={scale} blink={blink} className={`px px-bob ${className ?? ''}`} />;
}

export const ALL_SPRITES: SpriteDef[] = [...Object.values(TIER_SPRITES).flatMap((m) => Object.values(m)), ...Object.values(MASCOTS), ...Object.values(MEDIA_SPRITES).flatMap((m) => Object.values(m))];
