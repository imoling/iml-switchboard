import { describe, expect, it } from 'vitest';
import fs from 'node:fs';
import path from 'node:path';

/**
 * 界面文案的护栏（移植自 iml-markdown-editor/src/test/uiCopy.test.ts）。
 *
 * 规矩写在全局 CLAUDE.md「界面文案」与本仓库 CLAUDE.md 的术语表，这里只负责执行：
 * 省略号只用「…」、中文里不用半角冒号、禁用词、副文案长度、同一句不抄两份。
 */

const ROOT = path.resolve(__dirname, '../..');
const SCAN_DIRS = ['src'];

/** 这些文件里的中文不是界面文案 */
const NOT_UI = [
  /\.test\.tsx?$/,
  /[/\\]test[/\\]/,
  /api[/\\]types\.ts$/,          // 接口注释
];

interface UiString { file: string; line: number; text: string }

const CJK = /[一-鿿]/;
const STRING_LITERAL = /(['"`])((?:\\.|(?!\1)[^\\])*?)\1/g;
// JSX 里的正文可能跨好几行，所以整篇一起匹配
const JSX_TEXT = />([^<>{}]*[一-鿿][^<>{}]*)</gs;

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, entry.name);
    if (entry.isDirectory()) walk(p, out);
    else if (/\.tsx?$/.test(entry.name)) out.push(p);
  }
  return out;
}

function collect(): UiString[] {
  const out: UiString[] = [];
  for (const dir of SCAN_DIRS) {
    for (const file of walk(path.join(ROOT, dir))) {
      const rel = path.relative(ROOT, file);
      if (NOT_UI.some((re) => re.test(rel))) continue;
      const src = fs.readFileSync(file, 'utf8');
      // 注释不是界面文案：块注释整段去掉，行注释按行去掉
      const code = src.replace(/\/\*[\s\S]*?\*\//g, '').replace(/^[ \t]*\/\/.*$/gm, '');
      code.split('\n').forEach((line, i) => {
        if (!CJK.test(line)) return;
        for (const m of line.matchAll(STRING_LITERAL)) if (CJK.test(m[2])) out.push({ file: rel, line: i + 1, text: m[2].trim() });
      });
      for (const m of code.matchAll(JSX_TEXT)) {
        const text = m[1].replace(/\s+/g, ' ').trim();
        if (CJK.test(text)) out.push({ file: rel, line: code.slice(0, m.index ?? 0).split('\n').length, text });
      }
    }
  }
  return out;
}

const strings = collect();
const at = (s: UiString) => `${s.file}:${s.line} 「${s.text.slice(0, 60)}」`;
/** 纯文案：没有插值、不是 HTML / 代码片段 */
const prose = strings.filter((s) => !s.text.includes('${') && !s.text.includes('<') && !s.text.includes('\\n'));

describe('界面文案', () => {
  it('扫得到东西（扫描器本身别悄悄失效）', () => {
    expect(strings.length).toBeGreaterThan(150);
  });

  it('省略号只用「…」，不用三个点', () => {
    const bad = strings.filter((s) => s.text.includes('...') && !/sk-|eyJ|\.\.\.[a-zA-Z{([]/.test(s.text));
    expect(bad.map(at)).toEqual([]);
  });

  it('中文句子里不用半角冒号', () => {
    const bad = prose.filter((s) => /[一-鿿][^:]{0,24}: /.test(s.text));
    expect(bad.map(at)).toEqual([]);
  });

  it('术语表外的词不出现，也不用「您」', () => {
    // 本仓库 CLAUDE.md：渠道不叫泳道 / lane / provider；档位不叫 Tier / 第四档 / 旗舰；实现细节词不上界面
    const banned = ['泳道', 'lane', 'provider', 'Tier', '第四档', '旗舰', 'MCP', 'stdio', 'OAuth', '钥匙串', 'LiteLLM', 'JSONL', 'SSE', 'base_url', '您'];
    const bad = strings.filter((s) => banned.some((w) => (/^[a-zA-Z_]+$/.test(w) ? new RegExp(`(^|[^a-zA-Z_])${w}([^a-zA-Z_]|$)`).test(s.text) : s.text.includes(w))));
    expect(bad.map(at)).toEqual([]);
  });

  it('副文案不超过 40 字（超过说明一句话塞了好几件事）', () => {
    // 按「看起来多长」算：一个汉字顶一个字，英文和数字两个顶一个
    const width = (t: string) => {
      const cjk = (t.match(/[一-鿿]/g) || []).length;
      return cjk + Math.ceil((t.length - cjk) / 2);
    };
    const bad = prose.filter((s) => width(s.text) > 40);
    expect(bad.map(at)).toEqual([]);
  });

  it('同一句话不抄在两个文件里（要复用就抽常量 / 组件）', () => {
    const byText = new Map<string, Set<string>>();
    for (const s of strings) {
      if (s.text.length < 12) continue;
      if (s.text.includes('\\n') || s.text.includes('${')) continue;
      if (!byText.has(s.text)) byText.set(s.text, new Set());
      byText.get(s.text)!.add(s.file);
    }
    const dup = [...byText.entries()].filter(([, files]) => files.size > 1);
    const allowed = new Set<string>([]);
    expect(dup.filter(([text]) => !allowed.has(text)).map(([text, files]) => `「${text.slice(0, 40)}」← ${[...files].join(', ')}`)).toEqual([]);
  });
});
