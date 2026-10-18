package sub

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// readClaudePlan 从 ~/.claude.json 里只取账号的套餐类型两个字段。
// 这个文件是 Claude Code 的本机状态，不是凭据；邮箱等其他字段一律不解析。
func readClaudePlan(home string) string {
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return ""
	}
	var s struct {
		Account struct {
			OrgType  string `json:"organizationType"`
			OrgTier  string `json:"organizationRateLimitTier"`
			SeatTier string `json:"seatTier"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(b, &s) != nil {
		return ""
	}
	return ClaudePlanLabel(s.Account.OrgType, s.Account.OrgTier, s.Account.SeatTier)
}

// ClaudePlanLabel 把 organizationType / rateLimitTier 变成界面上的说法。
func ClaudePlanLabel(orgType, tier, seat string) string {
	t := strings.ToLower(tier)
	switch {
	case strings.Contains(t, "max_20x"):
		return "Max 20x"
	case strings.Contains(t, "max_5x"), strings.Contains(t, "max") && orgType == "claude_max":
		return "Max 5x"
	}
	switch strings.ToLower(orgType) {
	case "claude_max":
		return "Max"
	case "claude_pro":
		return "Pro"
	case "claude_team", "team":
		if strings.Contains(strings.ToLower(seat), "premium") {
			return "Team 高级席位"
		}
		return "Team"
	case "claude_enterprise", "enterprise":
		return "企业版"
	case "claude_free", "free":
		return "免费版"
	case "api", "console":
		return "按量付费"
	}
	return ""
}
