package sub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// cc-switch 这类工具把 Claude Code 切到第三方接口时，会往 settings.json 的 env 里写接口地址和令牌。
// claude -p 会读这些设置：订阅渠道就悄悄改走了第三方，花的是别人的钱，账本却记成订阅。
// 发现这种情况就暂停 Claude 订阅渠道，让同档任务改走别的渠道。

// thirdPartyKeys 是会让 claude 不走订阅登录的设置项。
var thirdPartyKeys = []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX"}

// settingsKeys 只解出键名：值（可能是令牌）不读进任何变量，用完即丢。
type settingsKeys struct {
	Env          map[string]json.RawMessage `json:"env"`
	APIKeyHelper json.RawMessage            `json:"apiKeyHelper"`
}

// thirdPartyIn 返回这些设置文件里出现的第三方接口设置项名字；文件不存在或解析不了就当没有。
func thirdPartyIn(paths ...string) []string {
	seen := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var s settingsKeys
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		for _, k := range thirdPartyKeys {
			if _, ok := s.Env[k]; ok {
				seen[k] = true
			}
		}
		if len(s.APIKeyHelper) > 0 && string(s.APIKeyHelper) != "null" {
			seen["apiKeyHelper"] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// claudeSettingsFiles 是 claude -p 会读到的设置文件：用户级，加上工作目录里的项目级。
func (d *ClaudeDriver) claudeSettingsFiles(projectDir string) []string {
	files := []string{filepath.Join(d.home(), ".claude", "settings.json")}
	if projectDir != "" {
		files = append(files, filepath.Join(projectDir, ".claude", "settings.json"), filepath.Join(projectDir, ".claude", "settings.local.json"))
	}
	return files
}

const thirdPartyHint = "Claude Code 设置里配了第三方接口，订阅暂停使用；多半是 cc-switch 切过，切回官方登录后恢复"
