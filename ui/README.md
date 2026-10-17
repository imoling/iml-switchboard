# iML Switchboard · 界面

Tauri 2 + React 19 + TypeScript + Vite。菜单栏弹窗、控制台窗口、快速面板、关于窗口都在这一个前端里，按窗口 label 分流。
所有数据来自后台 `imls serve`（`http://127.0.0.1:18081`，见 `../docs/API.md`），前端不碰任何凭据。

## 怎么跑

```sh
cd ui
unset HTTP_PROXY HTTPS_PROXY ALL_PROXY http_proxy https_proxy   # 这台机器的代理会把下载卡到 30 KB/s
npm install

npm run mock          # 开发期 mock 后台，监听 127.0.0.1:18081（MOCK_ONBOARDING=1 npm run mock 可看首次引导）
npm run dev           # 浏览器里开发：http://127.0.0.1:1420
                      #   ?win=popover 菜单栏弹窗 · ?win=quick 快速面板 · ?win=about 关于
npm run tauri dev     # 桌面壳（会自己拉起 vite；若 vite 已在跑，加 --config '{"build":{"beforeDevCommand":""}}'）
npm run check         # tsc + vitest（文案护栏）+ vite build
cd src-tauri && cargo check
```

真后台需要给 `/api/*` 加 CORS（`Access-Control-Allow-Origin` 回显请求的 Origin，`Allow-Headers: Content-Type, X-IMLS-Client`），
因为 Tauri 里页面的 Origin 是 `tauri://localhost`，浏览器开发时是 `http://127.0.0.1:1420`；mock 已经这么做了。

## 目录

```
ui/
  index.html, vite.config.ts, tsconfig.json
  mock/server.mjs            开发期 mock 后台（Node 内置 http，不装依赖；主题词表直接读 ../internal/themes）
  src/
    main.tsx                 按窗口 label 挑根组件
    styles.css               全部样式（Cloud Dancer 配色，没有组件库）
    api/types.ts             接口数据模型（与 docs/API.md 对应）
    api/client.ts            fetch 封装（带 X-IMLS-Client: ui）、/api/quick/run 流式读取、/api/events 事件流
    theme/                   词表：当前主题 → 标准 → 代码默认；投屏模式强制标准
    tauri.ts                 Tauri 桥（浏览器里安全降级）、快捷键写法转换
    format.ts / copy.ts      金额、时间、渠道档位标签；跨窗口复用的额度文案
    components/              Seg / Toggle / Chip / Bar / SetRow / Modal / Toast / 厂商 logo
    pages/                   控制台八个页面 + 首次引导
    windows/                 Console（主窗口）/ Popover（菜单栏弹窗）/ Quick（快速面板）/ About / Offline
    assets/vendors/          厂商 logo（Lobe Icons 静态 SVG + WorkBuddy PNG）
    test/uiCopy.test.ts      文案护栏
  src-tauri/
    src/lib.rs               托盘（含状态图）、三个窗口、失焦隐藏、全局快捷键、拉起 imls serve、剪贴板写图
    tauri.conf.json          窗口定义、标识 cn.imoling.switchboard
    capabilities/default.json
    icons/                   应用图标全套 + tray*.png（模板图、额度紧张、出错、未运行）
  screenshots/               浏览器 + mock 下每个页面的截图
```

## 页面与接口

| 页面 / 窗口 | 用到的接口 |
|---|---|
| 菜单栏弹窗 | `GET /api/status`、`GET /api/overview?range=today`、`GET /api/calls?limit=3`、`GET /api/settings`、`POST /api/mode`、`POST /api/pause`、`POST /api/projection`、`GET /api/events`（quota / status / call） |
| 快速面板 | `POST /api/quick/preview`（输入防抖 250 ms）、`POST /api/quick/run`（流式）；Enter 发送、⌘Enter 最好档 / 顶配、Tab 换模型、Esc 关闭、拖入图片 |
| 概览 | `GET /api/overview?range=today\|7d\|30d` |
| 请求记录 | `GET /api/calls`（source / lane / q）、`POST /api/calls/{id}/escalate`、`POST /api/calls/{id}/redo`；事件流 `call` 实时插入 |
| 路由规则 | `GET/PUT /api/rules`、`POST /api/rules/try`、`GET /api/rules/suggestions`、`POST /api/rules/suggestions/{task}` |
| 评估 | `GET/POST /api/evaluations`、`POST /api/evaluations/{id}/accept`、`PUT /api/settings`（自动评估开关） |
| 模型与账号 | `GET/POST /api/accounts`、`PUT/DELETE /api/accounts/{id}`、`POST /api/accounts/{id}/verify`、`POST /api/accounts/detect` |
| 媒体库 / 动画详情 | `GET /api/media?kind=`、`POST /api/jobs/{id}/cancel\|retry\|revise`；事件流 `job` 更新进度 |
| 接入 | `GET /api/integrations`、`POST /api/integrations/{id}/connect\|disconnect`、`PUT /api/integrations/{id}` |
| 设置 | `GET/PUT /api/settings`、`GET /api/themes`、`GET /api/rules/export`、`POST /api/rules/import` |
| 首次引导 | `GET /api/onboarding`、`POST /api/accounts/detect`、`POST /api/accounts`、`POST /api/integrations/claude_code/connect`、`POST /api/rules/try`、`POST /api/onboarding/done` |
| 系统通知 | 只响应事件流的 `notice`，由主窗口发（主窗口关闭只是隐藏，事件流仍在） |
| 托盘状态点 | 主窗口按 `quota` 事件判断：有订阅额度 ≥ 80% 为黄；事件流断开为灰 |

## 主题

所有可见文案经 `t(key)`：当前主题词表 → 标准词表 → `src/theme/defaults.ts`。费用、额度、报错写死不走词表。
投屏模式（菜单栏弹窗底部）打开时 `effectiveId` 强制 `standard`。

## 文案护栏

`src/test/uiCopy.test.ts` 扫描 `src` 下的中文字面量与 JSX 文本：省略号只用 `…`、中文里不用半角冒号、
禁用词（泳道、lane、provider、Tier、第四档、旗舰、MCP、stdio、OAuth、钥匙串、LiteLLM、JSONL、SSE、base_url、您）、
副文案 ≤ 40 字宽、同一句不出现在两个文件（复用抽成组件或常量）。`npm run check` 会跑。

## 已知未完成

- `npm run tauri dev` 能编译、能起进程（已验证），但这台机器的 shell 没有屏幕录制权限，没拿到 Tauri 窗口本身的截图；
  托盘点击定位、失焦隐藏、全局快捷键、系统通知这些只在真窗口里才能看到的行为没有逐个人工点过。
- 托盘带色状态图（额度紧张 / 出错 / 未运行）是非模板 PNG，深色菜单栏下描边仍是黑色；正常状态用模板图不受影响。
- 快速面板里显示图片走 `asset://` 协议，需要后台产物路径在 Tauri 的资源作用域内（capabilities 里还没放开 `core:asset` 范围，等真后台定下媒体目录再加）。
- 「存进项目…」目前只是弹保存对话框并把原图路径复制到剪贴板，真正的文件复制等后台提供接口或在 Rust 侧补一个命令。
- 「导出 GIF」只给提示，等后台接口。
- 规则导出在 Tauri 里是复制到剪贴板（浏览器里直接下载），没有写文件。
- 开机启动开关只写入设置，真正注册登录项要后台或 `tauri-plugin-autostart`。
- 没有 `tauri build` 打包（磁盘只剩约 11 GB，按要求只保证 cargo check / cargo build / vite build 通过）。
