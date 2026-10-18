package sub

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// 官方 CLI 升级后改参数，订阅渠道会悄悄失效，要等用户调用失败才发现。
// 自检只读 --help，不调模型、不花额度：驱动要用的参数还在不在、-p 是否改成了只认密钥。

// claudeFlags 是 claude -p 每次都可能用到的参数。--max-turns 是隐藏参数，不在帮助里，不查。
var claudeFlags = []string{"--print", "--output-format", "--verbose", "--no-session-persistence", "--model", "--append-system-prompt", "--permission-mode", "--tools", "--strict-mcp-config", "--input-format", "--system-prompt"}

var codexExecFlags = []string{"--json", "--skip-git-repo-check", "--ephemeral", "--config"}

// reBareDefault 认「只认密钥的极简模式成了默认」的说法：出现 --no-bare，或 bare 的说明里写了 default。
var reBareDefault = regexp.MustCompile(`(?is)--no-` + forbiddenClaudeFlag[2:] + `\b|` + forbiddenClaudeFlag + `\b[^-]{0,600}\(default`)

// missingFlags 返回帮助文本里找不到的参数。
func missingFlags(help string, flags []string) []string {
	var out []string
	for _, f := range flags {
		if !regexp.MustCompile(regexp.QuoteMeta(f) + `\b`).MatchString(help) {
			out = append(out, f)
		}
	}
	return out
}

// claudeProblems 根据 claude --help 判断订阅渠道还能不能用，返回一行一条的人话。
func claudeProblems(help string) []string {
	if strings.TrimSpace(help) == "" {
		return []string{"claude --help 没有输出"}
	}
	var out []string
	if m := missingFlags(help, claudeFlags); len(m) > 0 {
		out = append(out, "claude 不再认这些参数："+strings.Join(m, " "))
	}
	if reBareDefault.MatchString(help) {
		out = append(out, "claude -p 可能默认只认密钥了，订阅额度会用不上")
	}
	return out
}

func codexProblems(help string) []string {
	if strings.TrimSpace(help) == "" {
		return []string{"codex exec --help 没有输出"}
	}
	if m := missingFlags(help, codexExecFlags); len(m) > 0 {
		return []string{"codex exec 不再认这些参数：" + strings.Join(m, " ")}
	}
	return nil
}

func helpOf(ctx context.Context, bin string, args ...string) string {
	var b strings.Builder
	runProc(ctx, procSpec{Bin: bin, Args: args, Timeout: 20 * time.Second, OnLine: func(l string) { b.WriteString(l); b.WriteByte('\n') }})
	return b.String()
}

// SelfCheck 检查 claude 的参数，返回版本和问题；没装 claude 时返回空。
func (d *ClaudeDriver) SelfCheck(ctx context.Context) (string, []string) {
	bin := lookBin(d.bin())
	if bin == "" {
		return "", nil
	}
	p := claudeProblems(helpOf(ctx, bin, "--help"))
	d.mu.Lock()
	d.problems = p
	d.mu.Unlock()
	return versionOf(ctx, bin), p
}

// SelfCheck 检查 codex exec 的参数。
func (d *CodexDriver) SelfCheck(ctx context.Context) (string, []string) {
	bin := lookBin(d.bin())
	if bin == "" {
		return "", nil
	}
	p := codexProblems(helpOf(ctx, bin, "exec", "--help"))
	d.mu.Lock()
	d.problems = p
	d.mu.Unlock()
	return versionOf(ctx, bin), p
}

// withSelfCheck 把最近一次自检发现的问题并进健康状态：本来正常的降为警告。
func withSelfCheck(problems []string, h core.Health) core.Health {
	if p := problems; len(p) > 0 && h.Status == "ok" {
		h.Status = "warn"
		h.Detail = p[0]
	}
	return h
}
