// Package api 实现按量付费渠道：OpenAI 兼容的 chat completions 与 images 接口，自带供应商预设与价格表。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// shortBody 把回包压成一行摘要，供人话错误末尾使用。
func shortBody(body []byte) string {
	s := strings.TrimSpace(string(body))
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 160 {
		s = s[:160] + "…"
	}
	return s
}

// bodyMessage 从回包里取出给人看的那句：{"message":…} 或 {"error":{"message":…}}；顺手去掉 request id。
func bodyMessage(body []byte) (code, msg string) {
	var v struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
		Error   struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &v) != nil {
		return "", ""
	}
	msg = v.Message
	code = fmt.Sprint(v.Code)
	if msg == "" {
		msg, code = v.Error.Message, fmt.Sprint(v.Error.Code)
	}
	if i := strings.Index(msg, "(request id"); i > 0 {
		msg = msg[:i]
	}
	return strings.ToLower(code), strings.TrimSpace(msg)
}

// busyWords 是「服务端这会儿忙，稍后再试」的各种说法。
var busyWords = []string{"queue_full", "do_request_failed", "overloaded", "server_busy", "try again", "稍后重试", "队列已满", "upstream"}

// ClassifyHTTPError 把 HTTP 非 2xx 回包映射成一行人话的错误；供媒体渠道复用。
func ClassifyHTTPError(provider string, status int, body []byte) error {
	raw := fmt.Sprintf("HTTP %d: %s", status, strings.TrimSpace(string(body)))
	lower := strings.ToLower(string(body))
	code, msg := bodyMessage(body)
	for _, w := range busyWords {
		if strings.Contains(code, w) || strings.Contains(lower, w) {
			if msg == "" {
				msg = "服务端正忙，稍后重试"
			}
			return &core.ErrProvider{Summary: provider + " " + msg, Raw: raw, Retryable: true, Busy: true}
		}
	}
	switch {
	// 余额判断放在 401/403 前面：有的平台（Agnes）余额不足也回 403
	case status == 402 || strings.Contains(lower, "insufficient") || strings.Contains(lower, "balance") || strings.Contains(lower, "余额") || strings.Contains(lower, "预扣费"):
		return &core.ErrProvider{Summary: provider + " 余额不足：请充值后重试", Raw: raw, Retryable: true, Account: true, Cooldown: time.Hour}
	case status == 401 || status == 403:
		return &core.ErrProvider{Summary: provider + " 验证失败：密钥无效或已过期", Raw: raw, Retryable: true, Account: true, Cooldown: 24 * time.Hour}
	case status == 429:
		return &core.ErrRateLimited{Lane: core.LaneAPI, RetryAfter: 0, Detail: provider + " 请求过于频繁"}
	case isModerationText(lower):
		return &core.ErrProvider{Summary: provider + " 拒绝了这次请求：内容未通过审核", Raw: raw, Retryable: false}
	case status == 404:
		return &core.ErrProvider{Summary: provider + " 找不到接口或模型：" + shortBody(body), Raw: raw, Retryable: true}
	case status >= 500:
		if msg != "" {
			return &core.ErrProvider{Summary: provider + " 服务端出错：" + msg, Raw: raw, Retryable: true}
		}
		return &core.ErrProvider{Summary: provider + " 服务端出错：" + shortBody(body), Raw: raw, Retryable: true}
	}
	return &core.ErrProvider{Summary: fmt.Sprintf("%s 返回 %d：%s", provider, status, shortBody(body)), Raw: raw, Retryable: status >= 500}
}

// ClassifyNetError 把网络层错误映射成一行人话；供媒体渠道复用。
func ClassifyNetError(provider string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	lower := strings.ToLower(err.Error())
	var ne net.Error
	timeout := errors.As(err, &ne) && ne.Timeout()
	if errors.Is(err, context.DeadlineExceeded) || timeout || strings.Contains(lower, "proxy") || strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "eof") || strings.Contains(lower, "tls") {
		return &core.ErrProvider{Summary: provider + " 连接失败：检查网络代理", Raw: err.Error(), Retryable: true}
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return &core.ErrProvider{Summary: provider + " 连接失败：检查网络代理", Raw: err.Error(), Retryable: true}
	}
	return &core.ErrProvider{Summary: provider + " 请求失败：" + firstLine(err.Error()), Raw: err.Error(), Retryable: true}
}

// IsModerationError 判断错误是否属于内容审核拦截（不可重试、换供应商也没意义）。
func IsModerationError(err error) bool {
	if err == nil {
		return false
	}
	var pe *core.ErrProvider
	if errors.As(err, &pe) {
		return isModerationText(strings.ToLower(pe.Summary + " " + pe.Raw))
	}
	return isModerationText(strings.ToLower(err.Error()))
}

func isModerationText(lower string) bool {
	for _, m := range []string{"sensitive", "moderation", "content_policy", "content policy", "safety system", "审核", "违规", "敏感", "inappropriate", "rejected by the safety", "blocked"} {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l != "" {
			if len(l) > 160 {
				l = l[:160] + "…"
			}
			return l
		}
	}
	return ""
}
