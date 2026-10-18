<p align="center"><img src="assets/brand/iml-switchboard-icon.svg" width="112" alt="Switchboard"></p>

<h1 align="center">Switchboard</h1>

<p align="center"><b>Spend your subscription first. Send hard problems to strong models and chores to cheap ones.</b></p>

<p align="center"><a href="README.md">中文</a> | English</p>

<p align="center"><a href="https://github.com/imoling/iml-switchboard/actions/workflows/ci.yml"><img src="https://github.com/imoling/iml-switchboard/actions/workflows/ci.yml/badge.svg" alt="ci"></a> <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-C4613A.svg" alt="MIT"></a></p>

Switchboard brings your subscription models (Claude, ChatGPT, Antigravity), your pay-as-you-go models and your image and video models under one roof, and makes them usable from every agent you run: Claude Code, Codex, WorkBuddy, Hermes and more.

- **Picks a model by difficulty and saves where it can**: chores go to cheap models, hard problems to strong ones, and subscription quota is spent first.
- **Images without switching tools**: calls GPT Image, Nano Banana, Volcengine Ark and others directly, and saves the result into your project.
- **One model down, the next one carries on**: when a subscription runs out, hits a rate limit or errors, the next model picks up the same conversation.
- **Top models for every agent**: with the matching subscription or key, Opus, Fable and GPT are reachable from any connected agent (agent turns that carry tools use pay-as-you-go only, never a subscription).

Two ways to use it:

- **Delegate**: Claude Code, Codex and Gemini CLI keep their own main model and hand sub-tasks (translation, a second opinion, images) to Switchboard as MCP tools. The main conversation and its prompt cache are untouched.
- **Be the main model**: WorkBuddy, pi, OpenClaw and DeepSeek Harness use Switchboard as a model through an OpenAI-compatible endpoint. Every request carries the full conversation; Switchboard picks a model by difficulty and falls over to the next one if it fails.

It comes with a menu bar app (system tray on Windows) for quotas, spend, savings, and routing rules.

> Runs on macOS and Windows. The macOS build is used daily by the author. The Windows port is complete and every commit runs the full test suite on Windows in CI, but it has not yet been used by hand on a real Windows machine; feedback is welcome. Linux builds but is untested.
>
> The app's interface is in Chinese for now.

<p align="center"><img src="docs/images/overview.png" width="760" alt="Overview: calls, pay-as-you-go spend, savings versus Opus, subscription quota"></p>

## What it solves

- **Your subscription goes unused while your API bill grows.** Claude and ChatGPT plan quota is spent first, through the official CLIs on your machine. Only when it runs out does work move to pay-as-you-go, and that stops at a monthly cap.
- **Every task goes to the most expensive model.** Rules plus a low-tier model acting as a judge sort tasks into low, mid and high tiers. The top tier is used only when you ask for it, when you redo a task, or in quality mode.
- **Agents can't make images or videos.** Agents get tools for images, videos and motion graphics, and the output lands in the project's `assets/` folder.

## How it differs from similar tools

These tools solve different problems and can be used together. Compiled from each project's public documentation as of October 2026.

| | Switchboard | [magpie](https://github.com/yetone/magpie) | [cc-switch](https://github.com/farion1231/cc-switch) | [claude-code-router](https://github.com/musistudio/claude-code-router) | [OpenRouter](https://openrouter.ai) |
|---|---|---|---|---|---|
| Main job | Agents hand off sub-tasks; a model is picked by difficulty | One place to choose the model behind every agent | One-click switching of the provider behind a tool | Local gateway for Claude Code | Hosted model aggregator |
| Relationship to the agent's main model | Either: take delegated sub-tasks without touching it, or act as the main model | Replaces it | Replaces it | Replaces it | Replaces it |
| Picks a model per task | Rules plus a low-tier judge, with published accuracy tests | A small model classifies intent | No | Rules by scenario | Task classifier plus platform-wide spend ranking |
| How subscriptions are used | Only by launching the unmodified official CLIs (`claude -p`, `codex exec`, `agy -p`); never reads sign-in data | Reads Claude Code and Codex sign-in files and the Keychain to discover subscriptions; drives the official `claude` CLI for Claude | Beta accounts feature: signs in to Copilot, ChatGPT and Grok, then forwards through a local proxy | Doesn't use subscriptions; uses your API keys | Doesn't use subscriptions |
| Images, video, motion graphics | All three, each in three tiers | Image tool | No | No | Models available; you call them yourself |
| Platforms | macOS, Windows (Linux untested) | macOS, Windows, Linux | macOS, Windows, Linux | macOS, Windows, Linux | Hosted |

Vendors treat third-party use of subscription quota very differently. Anthropic allows you to run the unmodified `claude` CLI on your own machine but prohibits third-party clients from using subscription credentials directly ([terms](https://code.claude.com/docs/en/legal-and-compliance)). Google states that third-party software accessing the services behind Gemini CLI may lead to suspension ([terms](https://geminicli.com/docs/resources/tos-privacy/)). OpenAI offers an official "Sign in with ChatGPT" channel. Whatever tool you use, read the terms of your plan first.

## Compliance boundaries

These rules are enforced in code and guarded by tests; breaking one fails the build:

1. Never read, store or forward subscription credentials (`~/.claude/.credentials.json`, sign-in items in the Keychain, `~/.codex/auth.json`, `~/.gemini/oauth_creds.json`).
2. Never imitate an official client's request headers.
3. Subscription quota is spent only by unmodified official CLI subprocesses. Environment variables that would switch them to pay-as-you-go, such as `ANTHROPIC_API_KEY`, are removed before launch, and `--bare` is never passed.
4. Listen on `127.0.0.1` only.
5. No rotating multiple accounts, no pre-warming quota windows, no proxying a subscription to other programs.

The tests come in three layers: a source scan; a fake CLI that records the arguments and environment each subprocess actually receives; and a daily check of the official CLIs' help output that warns when a flag Switchboard relies on changes.

## Features

**Routing**
- Low, mid and high tiers, plus a top tier used only on explicit request; three modes: save, balanced and quality.
- When the rules are unsure, a low-tier model judges the task first; if it can't answer within 10 seconds, the rules decide.
- When a subscription runs out, hits a rate limit or errors, the next candidate takes over with the same conversation.
- Each candidate is checked against its context window and larger windows are tried when a request doesn't fit. When nothing fits, Switchboard returns the standard `context_length_exceeded` error so the agent can compact its own conversation.
- Mark a call as under-routed with one click; enough marks produce a rule suggestion. The evaluation feature re-runs samples on a high-tier model and has it judge blind whether the cheaper answer was good enough.

**Lanes**
- Subscriptions: Claude (`claude -p`), ChatGPT (`codex exec`), Antigravity (`agy -p`). While running, Switchboard starts the next CLI process ahead of time; measured end-to-end latency dropped from 7.5 s to about 4 s for Claude and from 32 s to about 5 s for Antigravity.
- Pay-as-you-go: Zhipu (GLM), Alibaba Cloud Model Studio (Qwen), DeepSeek, Moonshot (Kimi), MiniMax, Volcengine Ark (Doubao), OpenRouter, Anthropic, Agnes AI and 302.ai. Keys live in the system credential store: the macOS Keychain or Windows Credential Manager.
- Media: image and video generation in cheap, default and best tiers; motion graphics are written as Remotion code by a model and rendered to video on your machine.

**Supported agents**

| Agent | How it connects | Tested |
|---|---|---|
| Claude Code | MCP tools, usage notes in `CLAUDE.md`, slash commands | Used daily |
| Codex CLI | MCP tools, usage notes in `AGENTS.md` | Registered; hand-off flow verified with a simulated host |
| Gemini CLI | MCP tools, usage notes in `GEMINI.md` | Registered; personal Google accounts have moved to Antigravity, so an API key or enterprise account is needed |
| pi | OpenAI-compatible endpoint | Agent turns with tool calls verified |
| OpenClaw | OpenAI-compatible endpoint | Agent turns with tool calls verified |
| DeepSeek Harness | OpenAI-compatible endpoint | Agent turns with tool calls verified |
| Hermes Agent | MCP tools and image settings | Integration code is tested; not yet run for real |
| WorkBuddy (Tencent) | OpenAI-compatible endpoint | Plain chat verified; tool calls not yet verified |
| Doubao Work (ByteDance) | Custom connector (MCP) | Manual setup |

<table>
<tr>
<td width="34%"><img src="docs/images/popover.png" alt="Menu bar popover"></td>
<td><img src="docs/images/integrations.png" alt="Integrations"><br><img src="docs/images/rules.png" alt="Routing rules"></td>
</tr>
<tr>
<td align="center">Menu bar: today's spend, quotas, routing mode</td>
<td align="center">Connect agents and see how much each one delegates; edit routing rules in place</td>
</tr>
</table>

**Also**
- A ledger of every call: model, cost, and what the same call would have cost on Opus. The menu bar shows the last 7 days of savings. Costs can be shown in CNY or USD.
- Themes: Standard plus three playful ones that only change the interface wording; one click returns to Standard for screen sharing.

## Install

**Installers**: download the installer for your system from the [Releases](https://github.com/imoling/iml-switchboard/releases) page (a dmg on macOS; an msi or exe on Windows). The installer already includes the `imls` backend.

You also need at least one signed-in official CLI (`claude`, `codex` or `agy`) or one pay-as-you-go account.

**From source**: requires Go 1.27+, Node 20.19+ and Rust.

```sh
git clone https://github.com/imoling/iml-switchboard.git && cd iml-switchboard

# Backend and CLI (on Windows: go build -o bin\imls.exe ./cmd/imls, then run .\bin\imls.exe)
go build -o bin/imls ./cmd/imls
./bin/imls doctor          # checks CLIs, accounts, key storage and proxy settings
./bin/imls install         # start at login (or run ./bin/imls serve in the foreground)

# Desktop app: the build compiles imls for the target platform and bundles it
cd ui && npm install && npm run tauri build
```

| | macOS | Windows |
|---|---|---|
| Data folder | `~/Library/Application Support/Switchboard` | `%AppData%\Switchboard` |
| Quick panel shortcut | ⌥ Space | Ctrl + Alt + Space |
| Where keys are stored | Keychain | Credential Manager (an "iML Switchboard" entry under Windows Credentials) |
| Start at login | launchd | A per-user startup entry |

On first launch, a short setup checks your CLIs, adds a pay-as-you-go account, connects Claude Code and runs a test prompt.

If your terminal sets a global proxy, add `NO_PROXY=127.0.0.1,localhost`; otherwise agents such as pi and OpenClaw send requests meant for your own machine to the proxy and fail to connect.

## Usage

Once connected, Claude Code gains five tools: `ask_model` (ask another model), `generate_image`, `generate_video`, `make_animation` and `get_job` (check a media job). Just ask in plain words, for example "make a cover image" or "get a second opinion on this".

From the command line:

```sh
imls route "the login endpoint keeps returning 500, tried everything"   # show where it would go and why
imls route --run "translate this into Chinese: ..."                      # actually run it
imls stats                                                             # spend and savings for the last 7 days
imls golden                                                            # export under-routed requests as routing test cases
```

More in the [user guide](docs/使用指南.md) (Chinese) and the [API reference](docs/API.md) (Chinese).

## Routing accuracy

Routing is tested against two data sets: 105 golden cases used to tune the rules, and 60 blind cases written and labeled independently by another model and never looked at while tuning.

| | Blind-set accuracy | Under-routed |
|---|---|---|
| Rules only (previous version) | 46.7% | 51.7% |
| Rules only (current) | 65.0% | 33.3% |

Most rule misses are hard problems that contain no telltale keywords, which is why a low-tier judge is consulted when the rules are unsure. The samples are small, so treat these numbers as a direction rather than a guarantee; run `go test ./internal/core -run Tiers -v` to see them yourself.

## Known limitations

- The Windows build hasn't been used by hand on a real machine yet. Installers aren't signed yet: macOS will say the developer can't be verified, and Windows will show a SmartScreen prompt.
- Claude requests that carry their own system prompt aren't pre-warmed (requests from the OpenAI-compatible endpoint usually do).
- The Codex subscription hand-off flow, and tool-calling turns from Hermes and WorkBuddy, haven't been run end to end for real.
- Switchboard does not compact conversations on an agent's behalf; it returns the standard error and lets the agent compact.

## Roadmap

- Signed installers and auto-update; verify Linux.
- When Claude Code's subscription runs out, let it switch to Switchboard and continue on pay-as-you-go models (API keys only, never the subscription).
- Rate limits on subscription lanes; stop sending whole conversations from the OpenAI-compatible endpoint to the Claude subscription by default.
- More routing test data, growing the blind set to 200 cases.
- A Copilot CLI subscription lane (through the official CLI).
- Verify every pay-as-you-go and media lane with real keys.

## Development

```sh
make test          # go vet + go test, including golden, blind and compliance tests (CI runs on macOS, Windows and Linux)
make ui-check      # interface type check, tests and build
cd ui && npm run tauri dev
```

Project conventions (glossary, interface copy rules, hard limits) are in [CLAUDE.md](CLAUDE.md); interface development notes are in [ui/README.md](ui/README.md) (Chinese).

## License

[MIT](LICENSE)

Switchboard is not affiliated with Anthropic, OpenAI or Google. Claude, ChatGPT, Codex, Gemini and Antigravity are trademarks of their respective owners.

Made by iML Studio · © 2026 imoling
