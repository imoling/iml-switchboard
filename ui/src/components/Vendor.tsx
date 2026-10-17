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

const MAP: Record<string, string> = {
  anthropic: claude, claude: claude, claude_sub: claude, claude_code: claude,
  openai: openai, chatgpt: openai, codex: openai, codex_sub: openai,
  google: gemini, gemini: gemini, agy: gemini, agy_sub: gemini,
  zhipu: zhipu, glm: zhipu,
  qwen: qwen, alibaba: qwen, bailian: bailian, dashscope: bailian,
  volcengine: volcengine, ark: volcengine, doubao: doubao, seedream: volcengine, seedance: volcengine,
  openrouter: openrouter, deepseek: deepseek, moonshot: moonshot, kimi: moonshot, minimax: minimax,
  nous: nous, nousresearch: nous, hermes: nous, workbuddy: workbuddy,
};

/** 模型名前的厂商 logo；认不出的厂商不占位 */
export function Vendor({ provider, size = 'sm' }: { provider?: string; size?: 'sm' | 'lg' }) {
  const src = provider ? MAP[provider.toLowerCase()] : undefined;
  if (!src) return null;
  return <span className={`vi ${size === 'lg' ? 'lg' : ''}`}><img src={src} alt="" /></span>;
}

export function VendorLogo({ provider }: { provider: string }) {
  const src = MAP[provider.toLowerCase()];
  return <div className={`logo ${provider === 'workbuddy' ? 'lgimg' : ''}`}>{src ? <img src={src} alt="" /> : <span className="muted">?</span>}</div>;
}

export const Model = ({ provider, name }: { provider?: string; name: string }) => <><Vendor provider={provider} />{name}</>;
