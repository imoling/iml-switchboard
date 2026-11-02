import claude from '../assets/vendors/claude-color.svg';
import openai from '../assets/vendors/openai.svg';
import gemini from '../assets/vendors/gemini-color.svg';
import zhipu from '../assets/vendors/zhipu-color.svg';
import qwen from '../assets/vendors/qwen-color.svg';
import bailian from '../assets/vendors/bailian-color.svg';
import volcengine from '../assets/vendors/volcengine-color.svg';
import doubao from '../assets/vendors/doubao-color.svg';
import openrouter from '../assets/vendors/openrouter.svg';
import deepseek from '../assets/vendors/deepseek-color.svg';
import moonshot from '../assets/vendors/moonshot.svg';
import minimax from '../assets/vendors/minimax-color.svg';
import nous from '../assets/vendors/nousresearch.svg';
import workbuddy from '../assets/vendors/workbuddy.png';
import winwork from '../assets/vendors/winwork.png';
import agnes from '../assets/vendors/agnes.png';
import pi from '../assets/vendors/pi.svg';
import openclaw from '../assets/vendors/openclaw.svg';
import gateway from '../assets/vendors/gateway.svg';
import mark from '../assets/app-mark.svg';

const MAP: Record<string, string> = {
  anthropic: claude, claude: claude, claude_sub: claude, claude_code: claude, claude_desktop: claude,
  openai: openai, chatgpt: openai, codex: openai, codex_sub: openai,
  google: gemini, gemini: gemini, gemini_cli: gemini, agy: gemini, agy_sub: gemini,
  zhipu: zhipu, glm: zhipu,
  qwen: qwen, alibaba: qwen, bailian: bailian, dashscope: bailian,
  volcengine: volcengine, ark: volcengine, doubao: doubao, seedream: volcengine, seedance: volcengine,
  openrouter: openrouter, deepseek: deepseek, moonshot: moonshot, kimi: moonshot, minimax: minimax,
  nous: nous, nousresearch: nous, hermes: nous, workbuddy: workbuddy, winwork: winwork, agnes: agnes,
  doubao_work: doubao, pi: pi, deepseek_harness: deepseek, openclaw: openclaw, gateway, custom: gateway,
  switchboard: mark, // 路由型号（自动分档、三档、订阅优先）用应用自己的标记
};

/** 自定义服务只报型号名：按名字认是谁家的，认不出用通用的「服务」图标 */
const GUESS: [RegExp, string][] = [
  [/gpt|chatgpt|dall-e|sora|\bo[134]\b/, 'openai'], [/claude/, 'claude'], [/gemini|imagen|veo/, 'gemini'], [/deepseek/, 'deepseek'],
  [/qwen|qwq/, 'qwen'], [/glm|cogview/, 'zhipu'], [/kimi|moonshot/, 'moonshot'], [/minimax|hailuo|abab/, 'minimax'],
  [/doubao|seedream|seedance/, 'doubao'], [/agnes/, 'agnes'],
];

export function guessVendor(text: string): string {
  const t = text.toLowerCase();
  return GUESS.find(([re]) => re.test(t))?.[1] ?? 'gateway';
}

/** 自定义服务能选的图标，「自动」不在里面（空串） */
export const ICON_CHOICES: { id: string; label: string }[] = [
  { id: 'openai', label: 'OpenAI' }, { id: 'claude', label: 'Claude' }, { id: 'gemini', label: 'Gemini' }, { id: 'deepseek', label: 'DeepSeek' },
  { id: 'qwen', label: '通义千问' }, { id: 'zhipu', label: '智谱' }, { id: 'moonshot', label: 'Kimi' }, { id: 'minimax', label: 'MiniMax' },
  { id: 'doubao', label: '豆包' }, { id: 'openrouter', label: 'OpenRouter' }, { id: 'gateway', label: '通用' },
];

/** 账号用哪个图标：自定义服务先看手选的，没选就按它的型号名认 */
export function accountIcon(a: { provider: string; icon?: string; models?: string[] }): string {
  return a.provider === 'custom' ? a.icon || guessVendor((a.models ?? []).join(' ')) : a.provider;
}

const keyFor = (provider?: string, model?: string) => (provider === 'custom' ? guessVendor(model ?? '') : provider);

/** 模型名前的厂商 logo；认不出的厂商不占位 */
export const vendorSrc = (provider?: string): string | undefined => (provider ? MAP[provider.toLowerCase()] : undefined);

/** model 只对自定义服务有用：它的记录里只有 custom，要靠型号名认图标 */
export function Vendor({ provider, model, size = 'sm' }: { provider?: string; model?: string; size?: 'sm' | 'lg' }) {
  const src = vendorSrc(keyFor(provider, model));
  if (!src) return null;
  return <span className={`vi ${size === 'lg' ? 'lg' : ''}`}><img src={src} alt="" /></span>;
}

export function VendorLogo({ provider }: { provider: string }) {
  const src = MAP[provider.toLowerCase()];
  return <div className={`logo ${provider === 'workbuddy' || provider === 'winwork' || provider === 'agnes' ? 'lgimg' : ''}`}>{src ? <img src={src} alt="" /> : <span className="muted">?</span>}</div>;
}

export const Model = ({ provider, name }: { provider?: string; name: string }) => <><Vendor provider={provider} model={name} />{name}</>;
