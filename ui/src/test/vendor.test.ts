import { describe, expect, it } from 'vitest';
import { accountIcon, guessVendor } from '../components/Vendor';

describe('自定义服务的图标', () => {
  it('按型号名认厂商，认不出用通用图标', () => {
    expect(guessVendor('gpt-6.1-sol')).toBe('openai');
    expect(guessVendor('GPT Image 2')).toBe('openai');
    expect(guessVendor('claude-opus-5-5')).toBe('claude');
    expect(guessVendor('deepseek-v4-pro')).toBe('deepseek');
    expect(guessVendor('glm-5.3')).toBe('zhipu');
    expect(guessVendor('foo-bar')).toBe('gateway');
    expect(guessVendor('pro-max')).toBe('gateway'); // 别把 pro 里的 o 当成 o1
  });
  it('手选优先，没选按账号的型号认；预设厂商不受影响', () => {
    expect(accountIcon({ provider: 'custom', models: ['gpt-6-luna', 'gpt-6.1-sol'] })).toBe('openai');
    expect(accountIcon({ provider: 'custom', icon: 'claude', models: ['gpt-6-luna'] })).toBe('claude');
    expect(accountIcon({ provider: 'custom' })).toBe('gateway');
    expect(accountIcon({ provider: 'agnes', icon: 'openai' })).toBe('agnes');
  });
});
