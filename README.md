<p align="center"><img src="assets/brand/iml-switchboard-icon.svg" width="112" alt="Switchboard"></p>

<h1 align="center">Switchboard</h1>

<p align="center"><b>订阅额度先用，难事交给强模型，杂活交给便宜模型。</b></p>

<p align="center">中文 | <a href="README.en.md">English</a></p>

<p align="center"><a href="https://github.com/imoling/iml-switchboard/actions/workflows/ci.yml"><img src="https://github.com/imoling/iml-switchboard/actions/workflows/ci.yml/badge.svg" alt="ci"></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-C4613A.svg" alt="MIT"></a></p>

Switchboard 把你订阅的模型（Claude、ChatGPT、Antigravity）、充值的按量付费模型、生图生视频模型统一管起来，让 Claude Code、Codex、WorkBuddy、Hermes 这些智能体在哪都能用：

- **按难易自动挑模型，能省就省**：杂活给便宜模型，难事给强模型，订阅额度先用。也可以打开智能分派，请本机的 Laya 小模型、Jev 或你已有的快模型按任务内容挑。
- **省了多少看得见**：每条记录都算出原价，也就是应用自己调模型要花多少（WorkBuddy 按它的积分计费，其他应用按原来用的型号和官方价），省下 = 原价 − 实际花费。
- **要图就生图，不用切换**：直接调 GPT Image、Nano Banana、火山方舟、Agnes 这些出图模型，能照参考图画，产物落到项目里；在对话里说「根据这个文案画一张」，上文会一起带给出图模型。
- **一个挂了换下一个**：订阅额度用完、限流或报错时，自动换下一个模型，带着同一段对话接着干；一整档都用不了就提一档。
- **哪个应用都能用上顶级模型**：配好对应的订阅或密钥，Opus、Fable、GPT 这些强模型哪个应用都调得到（带工具的智能体回合只走按量付费，不走订阅）。

三种用法：

- **派活**：Claude Code、Codex、Gemini CLI 保留自己的主力模型，把翻译、第二意见、生图这些子任务交给 Switchboard（MCP 工具）。主力的上下文和缓存不受影响。
- **当主力模型**：WorkBuddy、pi、OpenClaw、DeepSeek Harness 直接把 Switchboard 当成一个模型（OpenAI 兼容接口）。每次请求带着完整对话，Switchboard 按难易挑模型，一个失败就换下一个接着答。
- **接管 Claude Code、Claude Desktop 和 Codex**：应用页打开「接管模型」，它们自己的对话就改走 Switchboard（Claude Code 和 Claude Desktop 用 Anthropic 协议，Codex 用 Responses 协议，流式、工具调用、提示缓存都是原生的）。Claude Code 和 Codex 的 `/model` 里能选「自动分档」「高档 / 中档 / 低档」或任何一个按量付费型号，Claude Desktop 的模型菜单里是这四档；关掉开关原样恢复。订阅额度用完、或想用 WinWork 网关的模型接着写代码时用它。

配一个菜单栏（Windows 上是托盘）应用：看额度、看花费、看省了多少、改规则。

> 支持 macOS 和 Windows。macOS 版在作者的电脑上日常使用；Windows 版已完成移植，每次提交都会在 Windows 上自动跑全部测试，但还没有在真实的 Windows 电脑上手工用过，欢迎反馈。Linux 版能构建，未验证。

<p align="center"><img src="docs/images/overview.png" width="760" alt="总览：按量花费、自动分档省下多少、调度次数，接入的应用和渠道，每天省下多少"></p>

<p align="center"><a href="docs/videos/switchboard-launch-0.6.mp4"><img src="docs/images/launch-poster-0.6.png" width="760" alt="50 秒介绍视频"></a><br><sub>50 秒看懂它在做什么 · <a href="docs/videos/switchboard-launch-0.6.mp4">播放视频</a></sub></p>

## 它解决什么

- **订阅额度用不完，按量付费却在涨。** Claude、ChatGPT 的套餐额度经本机官方命令行优先消耗，不够了才改走按量付费，月上限到了就停。
- **什么活都交给最贵的模型。** 规则加低档模型判档，或者打开智能分派请判官挑，把任务分到低档、中档、高档；只有显式要求、重做或质量模式才用顶配。
- **智能体不会生图、生视频。** 应用多了生图、生视频、做动画三个工具，产物直接落到项目的 `assets/` 里。

## 和其他工具有什么不同

它们解决的问题不一样，可以一起用。下表依据各自 2026 年 10 月的公开文档整理。

| | Switchboard | [magpie](https://github.com/yetone/magpie) | [cc-switch](https://github.com/farion1231/cc-switch) | [claude-code-router](https://github.com/musistudio/claude-code-router) | [OpenRouter](https://openrouter.ai) |
|---|---|---|---|---|---|
| 主要做什么 | 应用把子任务派出来，按难度选模型 | 每个智能体用哪个模型，一处管理 | 一键切换工具背后的供应商 | Claude Code 的本地网关 | 云端模型聚合 |
| 和应用主力模型的关系 | 两种都行：派子任务（不动主力），或直接当主力模型 | 替换主力 | 替换主力 | 替换主力 | 替换主力 |
| 按任务选模型 | 规则 + 低档模型判档，或智能分派（本机 Laya 等判官挑）；有公开的准确率测试 | 小模型判意图 | 无 | 按场景的规则 | 任务分类 + 全平台消费排名 |
| 订阅额度怎么用 | 只启动未修改的官方命令行（`claude -p`、`codex exec`、`agy -p`），不读登录信息 | 读取 Claude Code、Codex 的登录信息和钥匙串来发现订阅；Claude 部分驱动官方命令行 | 测试版账号功能：登录 Copilot、ChatGPT、Grok 后经本地代理转发 | 不用订阅，用你的 API 密钥 | 不用订阅 |
| 生图、生视频、动画 | 三类都有，各分三档 | 生图工具 | 无 | 无 | 有模型，需自己调用 |
| 平台 | macOS、Windows（Linux 未验证） | macOS、Windows、Linux | macOS、Windows、Linux | macOS、Windows、Linux | 云端 |

各厂商对「第三方工具使用订阅额度」的规定差别很大：Anthropic 允许本人在本机调用未修改的 `claude` 命令行，但禁止第三方客户端拿订阅凭据直连（[原文](https://code.claude.com/docs/en/legal-and-compliance)）；Google 明确规定第三方软件直连 Gemini CLI 背后的服务可停号（[原文](https://geminicli.com/docs/resources/tos-privacy/)）；OpenAI 提供官方的「Sign in with ChatGPT」通道。用任何工具之前，都建议读一下你所用套餐的条款。

## 合规边界

这几条写在代码里，并有测试守着，改坏了测试就过不去：

1. 不读取、不存储、不转发任何订阅凭据（`~/.claude/.credentials.json`、钥匙串里的登录项、`~/.codex/auth.json`、`~/.gemini/oauth_creds.json`）。
2. 不伪装官方客户端的请求头。
3. 订阅额度只由未修改的官方命令行子进程消耗；启动前清掉 `ANTHROPIC_API_KEY` 等会改走按量付费的环境变量，永远不传 `--bare`。
4. 只监听 `127.0.0.1`。
5. 不做多账号轮换、不做额度预热、不把订阅经代理转给别的程序。

测试分三层：扫描源码；用假的命令行在运行时检查子进程真正收到的参数和环境变量；每天读一次官方命令行的帮助，参数变了就提醒。

## 功能

**调度**
- 低档、中档、高档三档，加只在显式要求时才用的顶配；省钱、均衡、质量三种调度模式。
- 规则拿不准时，先请低档模型判一次档，10 秒内判不出就照规则走。
- 智能分派（默认关）：请判官在能用的模型里挑，规则判的档位当先验，带工具、带图的请求先筛掉接不了的模型。判官可以是本机 Laya（分派页里下载、运行、停止，空闲 10 分钟释放内存，任务内容不出电脑）、Jev，或你已有的一个快模型；应用的模型菜单里选「Switchboard 智能分派」也行。分派页的「试一句」把规则和判官的结果并排给你看。
- 订阅额度用完、限流或报错时，自动换下一个候选模型，同一段对话照样带过去；会话粘住的模型出错也一样回退。自动分档时整档都用不了，提一档接着答。
- 档位候选：低、中、高三档各一张表，勾选哪些渠道的型号进这一档、谁先试，全局一张，每个应用还能另排一张只对它生效的。生图、生视频也各有一张先后表，没有模型服务的型号不列出来。
- 选模型时检查上下文窗口，装不下就换窗口更大的；谁都装不下时返回标准的 `context_length_exceeded`，让应用自己压缩。
- 「判低了」一键标记，攒够了会建议调整规则；评估功能抽样重做、盲评，看便宜模型的结果够不够好。

**渠道**
- 订阅：Claude（`claude -p`）、ChatGPT（`codex exec`）、Antigravity（`agy -p`）。常驻时会提前启动好下一个进程，实测 Claude 从 7.5 秒降到约 4 秒，Antigravity 从 32 秒降到约 5 秒。
- 按量付费：智谱、阿里云百炼、DeepSeek、Moonshot、MiniMax、火山方舟、OpenRouter、Anthropic、Agnes AI、302.ai。WinWork（讯飞）装了就自动出现在订阅那组，用它网关里的 Sonnet 5.5、DeepSeek、Qwen、Kimi 等型号，额度算在 WinWork 账上，不计费。密钥存在系统自带的凭据库里：macOS 钥匙串、Windows 凭据管理器。
- 媒体：生图、生视频分便宜档、默认档、最好档，也能按你排的先后用、在请求里点名型号；能照参考图画；动画由模型写 Remotion 代码，在本机渲染成视频。

**接入的应用**

| 应用 | 接入方式 | 实测 |
|---|---|---|
| Claude Code | MCP 工具 + `CLAUDE.md` 调用约定 + 斜杠命令 | 已在本机日常使用 |
| Codex | MCP 工具 + `AGENTS.md` 调用约定；终端装的或 ChatGPT 桌面版自带的 `codex` 都认 | 已注册；派活流程用模拟应用验证 |
| Gemini CLI | MCP 工具 + `GEMINI.md` 调用约定 | 已注册；个人账号已被 Google 迁到 Antigravity，需 API 密钥或企业账号 |
| pi | 兼容入口（OpenAI 格式） | 带工具的智能体回合实测通过 |
| OpenClaw | 兼容入口 | 带工具的智能体回合实测通过 |
| DeepSeek Harness | 兼容入口 | 带工具的智能体回合实测通过 |
| Hermes Agent | MCP 工具 + 生图配置 | 接入代码有测试，未在真实环境跑过 |
| WorkBuddy | 兼容入口 | 带工具的回合实测通过；说「生成图片」直接走 Switchboard 的渠道出图 |
| WinWork（讯飞） | MCP 工具 + 它内嵌 Claude Code 的 `CLAUDE.md` 调用约定 + 斜杠命令 | 已在本机接入 |
| 豆包工作 | 自定义连接器（MCP） | 手动配置 |

<table>
<tr>
<td width="34%"><img src="docs/images/popover.png" alt="菜单栏弹窗"></td>
<td><img src="docs/images/rules.png" alt="分派页：智能分派和试一句"><br><img src="docs/images/calls.png" alt="记录页"></td>
</tr>
<tr>
<td align="center">菜单栏：今天花了多少、额度、调度模式</td>
<td align="center">分派页：选规则分派还是智能分派，试一句看两边各派给谁；记录页：每次调用的用量、单价、花费和原价</td>
</tr>
<tr>
<td colspan="2"><img src="docs/images/integrations.png" alt="应用页"></td>
</tr>
<tr>
<td colspan="2" align="center">应用页：接入各个智能体、排它自己的档位候选、设原价按哪个型号算</td>
</tr>
</table>

**其他**
- 记录：每次调用的模型、用量、单价、花费和原价，分页列出，点开看理由和候选；回看跑出来的调用不算进省下。总览看每天省下或调度量、档位分布、花得最多的任务类型。费用可按人民币或美元显示。
- 风格主题：标准、点将台、后宫、海王，只换界面说法；投屏时一键切回标准。菜单栏图标有八种样式可选（实验功能）。
- 后台服务：设置里能看到运行多久、一键重启；连着的后台不是当前安装包里那份时会提示。
- 检查更新：每天先查 Gitee 镜像，镜像没有再查 GitHub Releases；有新版本通知一声，设置里一键下载本平台的安装包。

## 安装

**macOS 用安装脚本**（推荐）：安装包还没有苹果公证，浏览器下载的 dmg 打开时要去「隐私与安全性」点「仍要打开」。用脚本装就不用：

```sh
curl -fsSL https://raw.githubusercontent.com/imoling/iml-switchboard/main/scripts/install-mac.sh | bash
```

脚本下载本机芯片对应的最新 dmg（先 Gitee 镜像，没有再去 GitHub），装到「应用程序」，清掉下载来源标记；后台服务没在干活时顺手换成新版本。已经下载好的 dmg 可以传给它：`bash install-mac.sh ~/Downloads/Switchboard_0.6.0_aarch64.dmg`。

**用安装包**：在 [Releases](https://github.com/imoling/iml-switchboard/releases) 页下载对应系统的安装包（macOS 是 dmg，Windows 是 msi 或 exe）。安装包里已经带了后台程序 `imls`。

还需要至少一个已登录的官方命令行（`claude`、`codex` 或 `agy`），或一个按量付费账号。

**从源码构建**：需要 Go 1.27+、Node 20.19+、Rust。

```sh
git clone https://github.com/imoling/iml-switchboard.git && cd iml-switchboard

# 后台服务和命令行（Windows 上写成 go build -o bin\imls.exe ./cmd/imls，之后用 .\bin\imls.exe）
go build -o bin/imls ./cmd/imls
./bin/imls doctor          # 检查本机命令行、账号、密钥存储、代理设置
./bin/imls install         # 注册开机自启（也可以 ./bin/imls serve 前台运行）

# 桌面应用：构建时会按目标平台编译一份 imls 一起打包
cd ui && npm install && npm run tauri build
```

| | macOS | Windows |
|---|---|---|
| 数据目录 | `~/Library/Application Support/Switchboard` | `%AppData%\Switchboard` |
| 快速面板快捷键 | ⌥ Space | Ctrl + Alt + Space |
| 密钥存放 | 钥匙串 | 凭据管理器（「Windows 凭据」里的 iML Switchboard 条目） |
| 开机自启 | launchd | 当前用户的「登录时启动」项 |

打开应用，首次引导会带你检测本机命令行、添加一个按量付费账号、接入 Claude Code，然后试一句。

终端里设了全局代理时，记得加上 `NO_PROXY=127.0.0.1,localhost`，否则 pi、OpenClaw 这些应用会把发往本机的请求也交给代理。

## 用起来

接入 Claude Code 后，它多了五个工具：`ask_model`（问别的模型）、`generate_image`、`generate_video`、`make_animation`、`get_job`（查媒体任务进度）。平时直接说「配一张封面图」「照着这张头像画成像素风」「请另一个模型评审一下」就行，贴的图会当参考图传过去。WorkBuddy、Codex 这类把 Switchboard 当主力模型的应用，在对话里说「根据上面的文案画一张」也行，上文会并进出图提示词。

命令行：

```sh
imls route "这个登录接口一直 500，试了很多次还是不行"   # 只看会派给谁、为什么
imls route --run "把这段话翻译成英文：……"              # 真正执行
imls stats                                            # 近 7 天的花费和节省
imls golden                                           # 导出「判低了」的请求，补进路由测试
```

更多见[使用指南](docs/使用指南.md)，接口说明见 [docs/API.md](docs/API.md)。

## 路由准确率

路由规则有两套测试数据：105 条调规则用的金标集，60 条由另一个模型独立出题、标注的盲测集（调规则时不看）。

| | 盲测集准确率 | 判低了 |
|---|---|---|
| 只靠规则（旧版） | 46.7% | 51.7% |
| 只靠规则（现在） | 65.0% | 33.3% |
| 智能分派（本机 Laya 判） | 56.7% | 21.7% |

规则判不准的大多是「没有关键词的难题」，所以拿不准时会再请低档模型判一次。智能分派那一行是 2026 年 11 月 5 日在作者电脑上跑的：判官在这台电脑实际能用的 11 个模型里挑，平均每题 57 毫秒。判低了比规则少了三分之一，但有 7 道中档题被它判成了高档，总准确率反而低一些。这些数字样本还小，只当方向参考；规则那两行用 `go test ./internal/core -run Tiers -v` 可以自己跑。

## 已知限制

- Windows 版还没有在真实电脑上手工用过；安装包暂未签名，macOS 直接打开 dmg 会提示「无法验证开发者」（用上面的安装脚本可以避开），Windows 会出现 SmartScreen 提示。
- 带系统提示的 Claude 请求不预热（兼容入口转来的请求多半带系统提示）。
- Codex 订阅渠道的派活流程、Hermes 的带工具回合还没有在真实环境完整跑过。
- 不会替应用压缩对话：只返回标准错误，由应用自己压缩。
- 智能分派的判官：本机 Laya 第一次要装 Python 运行环境、下载约 680 MB 权重；Jev 在国内直连会被拒，要在设置里填代理。
- 菜单栏图标的告警、出错两张带色点，不是模板图，深色菜单栏下底板会和菜单栏融在一起。

## 路线图

- 签名安装包和应用内静默更新（现在是检查新版本后给下载）；Linux 版验证。
- 订阅渠道限速；兼容入口默认不把整轮对话交给 Claude 订阅。
- 扩充路由测试数据，盲测集扩到 200 条。
- Copilot CLI 订阅渠道（走官方命令行）。
- 用真实密钥逐个验证全部按量付费和媒体渠道。

## 开发

```sh
make test          # go vet + go test，含金标集、盲测集、合规测试（CI 在 macOS、Windows、Linux 上都跑）
make ui-check      # 界面类型检查、测试、构建
cd ui && npm run tauri dev
```

项目约定（术语表、界面文案、红线）见 [CLAUDE.md](CLAUDE.md)，界面开发见 [ui/README.md](ui/README.md)。

## 许可

[MIT](LICENSE)

Switchboard 与 Anthropic、OpenAI、Google 没有任何关联。Claude、ChatGPT、Codex、Gemini、Antigravity 是各自公司的商标。

iML Studio 出品 · © 2026 imoling
