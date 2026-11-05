// Package jev 是 TypeSafe Jev 的最小客户端：只用它的选择题，一次请求拿到每个选项的概率。
// 文档：docs.typesafe.ai；请求和回包的形状与 iml-jev-gate、jev-ultrafast 用的一致。
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// DefaultEndpoint 是 Jev 的接口；测试和隔离环境用 IMLS_JEV_ENDPOINT 改到假服务上。
const DefaultEndpoint = "https://api.typesafe.ai/v1/systemone"

// Model 是用的 Jev 型号。
const Model = "jev-latest"

// Client 调 Jev。Key 现取，不缓存在内存里以外的地方。
type Client struct {
	Name     string // 报错里怎么称呼：Jev（默认）或 Laya
	Key      func() (string, error)
	HTTP     *http.Client
	Endpoint string
	Timeout  time.Duration
}

// Endpoint 返回实际用的接口地址。
func Endpoint() string {
	if e := os.Getenv("IMLS_JEV_ENDPOINT"); e != "" {
		return e
	}
	return DefaultEndpoint
}

// Answer 是一道选择题的回答。
type Answer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    float64            `json:"confidence"`
}

// Choose 问一道选择题：state 是给它看的材料，criteria 是选项 id → 描述，instructions 是怎么挑。
func (c *Client) Choose(ctx context.Context, state any, criteria map[string]string, instructions any) (*Answer, error) {
	name := c.Name
	if name == "" {
		name = "Jev"
	}
	key, err := c.Key()
	if err != nil || key == "" {
		return nil, errors.New("没有 " + name + " 密钥")
	}
	body, _ := json.Marshal(map[string]any{
		"model": Model,
		"state": state,
		"questions": map[string]any{
			"model": map[string]any{"type": "choice", "criteria": criteria, "instructions": instructions},
		},
	})
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 3 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ep := c.Endpoint
	if ep == "" {
		ep = Endpoint()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New(name + " 超时没回")
		}
		return nil, fmt.Errorf("连不上 %s：%w", name, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, errors.New(name + " 密钥不对")
	case resp.StatusCode == http.StatusUnavailableForLegalReasons:
		return nil, errors.New("Jev 不接受国内直连：到设置里填代理")
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, errors.New(name + " 请求太频繁")
	case resp.StatusCode/100 != 2:
		return nil, fmt.Errorf("%s 出错：HTTP %d", name, resp.StatusCode)
	}
	var out struct {
		Answers map[string]Answer `json:"answers"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return nil, errors.New(name + " 回包不是合法 JSON")
	}
	a, ok := out.Answers["model"]
	if !ok || !valid(a, criteria) {
		return nil, errors.New(name + " 回包不合法")
	}
	return &a, nil
}

// valid 照 jev-ultrafast 的 validate_choice：选的在选项里、概率齐全且在 0 到 1 之间、加起来约等于 1、选的是概率最大的。
func valid(a Answer, criteria map[string]string) bool {
	if _, ok := criteria[a.Choice]; !ok || len(a.Probabilities) != len(criteria) {
		return false
	}
	sum, best := 0.0, 0.0
	for id, p := range a.Probabilities {
		if _, ok := criteria[id]; !ok || math.IsNaN(p) || p < 0 || p > 1 {
			return false
		}
		sum += p
		best = math.Max(best, p)
	}
	return math.Abs(sum-1) < 0.02 && a.Probabilities[a.Choice] >= best-1e-6
}

// 脱敏规则照 iml-jev-gate：家目录、令牌、密钥样式的长串、邮箱、手机号
var redactions = []struct {
	re  *regexp.Regexp
	rep string
}{
	{regexp.MustCompile(`/(?:Users|home)/[^/\s"']+`), "~"},
	{regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._\-]{8,}`), "${1}<已脱敏>"},
	{regexp.MustCompile(`\b(sk|pk|ak|ghp|gho|xox[abp])[-_][A-Za-z0-9\-_]{12,}`), "<已脱敏>"},
	{regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|passw(?:or)?d|authorization)\w*\s*[=:]\s*)["']?[^\s"'&]{4,}`), "${1}<已脱敏>"},
	{regexp.MustCompile(`\b[A-Za-z0-9+_\-]{32,}={0,2}`), "<长串>"},
	{regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`), "<邮箱>"},
	{regexp.MustCompile(`(^|\D)1[3-9]\d{9}(\D|$)`), "${1}<手机号>${2}"},
}

// Redact 去掉不该出境的东西，只留开头 limit 个字。
func Redact(text string, limit int) string {
	for _, r := range redactions {
		text = r.re.ReplaceAllString(text, r.rep)
	}
	if rs := []rune(strings.TrimSpace(text)); len(rs) > limit {
		return string(rs[:limit]) + "…"
	}
	return strings.TrimSpace(text)
}
