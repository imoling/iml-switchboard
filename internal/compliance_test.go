// 红线守卫：扫描源码，保证不碰订阅凭据、不传 --bare、不伪装客户端。
package internal

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var banned = []*regexp.Regexp{
	regexp.MustCompile(`\.credentials\.json`),
	regexp.MustCompile(`oauth_creds\.json`),
	regexp.MustCompile(`\.codex/auth\.json"`), // 只允许 stat 目录；读文件内容的写法会带引号路径
	regexp.MustCompile(`find-generic-password[^\n]*(Claude|Codex|claude|codex|anthropic)`),
	regexp.MustCompile(`"--bare"`),
	regexp.MustCompile(`claude-code-20250219`),
	regexp.MustCompile(`anthropic-beta`),
	regexp.MustCompile(`CLAUDE_CODE_OAUTH_TOKEN`),
}

func TestRedLines(t *testing.T) {
	root := ".."
	var hits []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			if info != nil && info.IsDir() && (info.Name() == "node_modules" || info.Name() == "target" || info.Name() == ".git" || info.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "compliance_test.go") {
			return nil
		}
		b, _ := os.ReadFile(path)
		for _, re := range banned {
			if re.Match(b) {
				hits = append(hits, path+" 含 "+re.String())
			}
		}
		return nil
	})
	if len(hits) > 0 {
		t.Fatalf("触碰红线：\n%s", strings.Join(hits, "\n"))
	}
}

// Windows 上直接 exec.Command 会给每个命令行子进程弹一个黑框（后台已脱离控制台）。
// 业务代码一律走 procutil.Command，它在 Windows 上设了不开窗口；测试文件和 procutil 自己除外。
func TestSubprocessesHideWindow(t *testing.T) {
	raw := regexp.MustCompile(`\bexec\.Command(Context)?\(`)
	var hits []string
	_ = filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == "node_modules" || info.Name() == "target" || info.Name() == ".git" || info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "internal/procutil/") {
			return nil
		}
		if b, _ := os.ReadFile(path); raw.Match(b) {
			hits = append(hits, path)
		}
		return nil
	})
	if len(hits) > 0 {
		t.Fatalf("要用 procutil.Command，Windows 上才不弹命令行窗口：\n%s", strings.Join(hits, "\n"))
	}
}
