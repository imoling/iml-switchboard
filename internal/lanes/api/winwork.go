package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/imoling/iml-switchboard/internal/core"
)

// WinWork（讯飞）是自家产品，它的网关是普通的 OpenAI 兼容接口，令牌在它内嵌 Claude Code 的配置里。
// 这不属于第三方订阅红线：令牌每次请求现读，不落盘、不转发给别处，只用于它自己的网关。

// WinWorkConfigDir 是 WinWork 内嵌 Claude Code 的配置目录。
func WinWorkConfigDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".kooky-data", "claude-config")
}

// WinWorkInstalled 判断本机装没装 WinWork 且登录过（有它的 settings.json）。
func WinWorkInstalled() bool {
	_, err := os.Stat(filepath.Join(WinWorkConfigDir(), "settings.json"))
	return err == nil
}

// WinWorkGateway 读出网关地址（已拼到 /v1）和令牌。WinWork 每次启动会刷新令牌，所以不缓存。
func WinWorkGateway() (baseURL, token string, err error) {
	b, err := os.ReadFile(filepath.Join(WinWorkConfigDir(), "settings.json"))
	if err != nil {
		return "", "", errors.New("本机没有安装 WinWork，或还没登录过")
	}
	var s struct {
		Env struct {
			BaseURL string `json:"ANTHROPIC_BASE_URL"`
			Token   string `json:"ANTHROPIC_AUTH_TOKEN"`
		} `json:"env"`
	}
	if json.Unmarshal(b, &s) != nil || s.Env.BaseURL == "" || s.Env.Token == "" {
		return "", "", errors.New("WinWork 的配置里没有网关信息：打开 WinWork 登录一次")
	}
	return strings.TrimRight(s.Env.BaseURL, "/") + "/v1", s.Env.Token, nil
}

func winworkPreset() Preset {
	models := map[string]ModelPrice{}
	for _, w := range core.WinWorkModels {
		models[w.ID] = ModelPrice{}
	}
	// BaseURL 只是默认值，装配账号时以 WinWork 配置里的为准
	return Preset{ID: "winwork", Name: "WinWork", BaseURL: "https://one.iflytek.com/api/llm/console/chat/v1", Currency: "CNY", ProbeModel: "winwork-ds-v4.1-flash", Models: models}
}

func init() { Presets = append(Presets, winworkPreset()) }
