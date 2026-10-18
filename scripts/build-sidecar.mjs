// 构建桌面应用前，按目标平台编译一份 imls，放到 src-tauri/binaries/imls-<目标三元组>[.exe]，
// Tauri 把它当附带程序打进安装包：用户装完不用自己把 imls 放进 PATH。
// 目标三元组优先用 Tauri 给的 TAURI_ENV_TARGET_TRIPLE，没有就问本机 rustc。
import { execFileSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const triple = process.env.TAURI_ENV_TARGET_TRIPLE
  || execFileSync('rustc', ['-vV'], { encoding: 'utf8' }).match(/host: (\S+)/)[1];

const GOOS = { 'apple-darwin': 'darwin', 'pc-windows-msvc': 'windows', 'pc-windows-gnu': 'windows', 'unknown-linux-gnu': 'linux' };
const GOARCH = { x86_64: 'amd64', aarch64: 'arm64' };
const [arch, ...rest] = triple.split('-');
const goos = GOOS[rest.join('-')];
const goarch = GOARCH[arch];
if (!goos || !goarch) {
  console.error(`不认识的目标平台：${triple}`);
  process.exit(1);
}
const out = join(root, 'ui', 'src-tauri', 'binaries', `imls-${triple}${goos === 'windows' ? '.exe' : ''}`);
mkdirSync(dirname(out), { recursive: true });
execFileSync('go', ['build', '-trimpath', '-o', out, './cmd/imls'], {
  cwd: root,
  stdio: 'inherit',
  env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' },
});
console.log(`imls → ${out}`);
