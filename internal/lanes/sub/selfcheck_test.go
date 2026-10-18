package sub

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/core"
)

const claudeHelpOK = `Usage: claude [options] [command] [prompt]
  -p, --print                 Print response and exit
  --output-format <format>    Output format
  --verbose                   Verbose
  --no-session-persistence    Do not save
  --model <model>             Model
  --append-system-prompt <p>  Append
  --permission-mode <mode>    Mode
  --tools <tools...>          Tools
  --strict-mcp-config         Only use MCP servers from --mcp-config
  --input-format <format>     Input format
  --system-prompt <prompt>    System prompt
  --` + "bare" + `                      Minimal mode: skip hooks, keychain reads. Anthropic auth is strictly ANTHROPIC_API_KEY
`

func TestClaudeProblems(t *testing.T) {
	if p := claudeProblems(claudeHelpOK); len(p) != 0 {
		t.Fatalf("当前帮助不该报问题：%v", p)
	}
	if p := claudeProblems(strings.Replace(claudeHelpOK, "--no-session-persistence", "--no-persist", 1)); len(p) != 1 || !strings.Contains(p[0], "--no-session-persistence") {
		t.Fatalf("参数没了要报出来：%v", p)
	}
	bareDefault := strings.Replace(claudeHelpOK, "Minimal mode:", "Minimal mode (default in -p):", 1)
	if p := claudeProblems(bareDefault); len(p) != 1 || !strings.Contains(p[0], "只认密钥") {
		t.Fatalf("极简模式成了默认要报出来：%v", p)
	}
	if p := claudeProblems(claudeHelpOK + "  --no-" + "bare   Use subscription login\n"); len(p) != 1 {
		t.Fatalf("出现 --no-bare 要报出来：%v", p)
	}
	if p := claudeProblems(""); len(p) != 1 {
		t.Fatalf("没有输出要报出来：%v", p)
	}
}

func TestCodexProblems(t *testing.T) {
	if p := codexProblems("  -c, --config <key=value>\n  --json\n  --skip-git-repo-check\n  --ephemeral\n"); len(p) != 0 {
		t.Fatalf("got %v", p)
	}
	if p := codexProblems("  --json\n  --ephemeral\n"); len(p) != 1 {
		t.Fatalf("got %v", p)
	}
}

func TestWithSelfCheck(t *testing.T) {
	p := []string{"claude 不再认这些参数：--tools"}
	if h := withSelfCheck(p, core.Health{Status: "ok"}); h.Status != "warn" || !strings.Contains(h.Detail, "--tools") {
		t.Fatalf("got %+v", h)
	}
	if h := withSelfCheck(p, core.Health{Status: "cooldown"}); h.Status != "cooldown" {
		t.Fatalf("冷却状态不该被盖掉：%+v", h)
	}
}

// 本机装了 claude 时，用真的帮助文本跑一次：当前版本不该报任何问题。
func TestClaudeSelfCheckLive(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("本机没有 claude")
	}
	ver, p := (&ClaudeDriver{}).SelfCheck(t.Context())
	if len(p) != 0 {
		t.Fatalf("claude %s 自检报了问题：%v", ver, p)
	}
}
