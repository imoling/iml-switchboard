# CLI 输出夹具

- `claude_ok.jsonl`：真实录制（2026-10-18，`claude -p "回复 OK 两个字" --output-format stream-json --verbose --no-session-persistence --model haiku --tools ""`）。
- `codex_model_error.jsonl`：真实录制，`-m gpt-5.4-mini` 时 ChatGPT 账号不支持该模型的报错。
- `codex_rate_limited.jsonl`：真实录制，`-m gpt-5.6-luna` 时撞上用量上限（「You've hit your usage limit … try again at …」）。
- `codex_ok.jsonl`：**手写合成**。三次真实调用都没拿到成功输出，按 `codex exec --json` 公开的事件形状
  （thread.started / turn.started / item.completed{agent_message} / turn.completed{usage}）写成，字段名以真实录到的错误夹具为参照。
