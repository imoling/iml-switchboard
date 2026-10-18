package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// USDPerCNY 是人民币折算美元的汇率，可按需调整。
var USDPerCNY = 7.1

// ModelPrice 是每百万 token 的价格，货币由 Account.Currency 决定。
type ModelPrice struct {
	InputPerM  float64
	OutputPerM float64
	// CachedPerM 为 0 时命中缓存的输入按 InputPerM 计。
	CachedPerM float64
}

// Account 是一个按量付费账号。
type Account struct {
	ID       string
	Provider string
	BaseURL  string
	// APIKeyGetter 按需取密钥（通常从钥匙串），不会被缓存。
	APIKeyGetter func() (string, error)
	// Currency 是价格表货币：USD 或 CNY。
	Currency string
	Models   map[string]ModelPrice
	// NeedsProxy 标记该供应商在国内需要代理。
	NeedsProxy bool
	// Headers 是额外请求头（例如 OpenRouter 的 HTTP-Referer）。
	Headers map[string]string
}

// APIKey 取密钥；没有配置 getter 时返回空串。
func (a Account) APIKey() (string, error) {
	if a.APIKeyGetter == nil {
		return "", &core.ErrProvider{Summary: a.Provider + " 没有配置密钥：在设置里填入后重试"}
	}
	k, err := a.APIKeyGetter()
	if err != nil {
		return "", &core.ErrProvider{Summary: a.Provider + " 读取密钥失败：" + firstLine(err.Error()), Raw: err.Error()}
	}
	if strings.TrimSpace(k) == "" {
		return "", &core.ErrProvider{Summary: a.Provider + " 没有配置密钥：在设置里填入后重试"}
	}
	return strings.TrimSpace(k), nil
}

// Cost 按价格表算一次调用的费用，返回 Usage 里的费用字段。
func (a Account) Cost(model string, in, out, cached int) (native, usd float64, currency string) {
	p, ok := a.Models[model]
	if !ok {
		// 允许用前缀匹配带日期后缀的模型名
		for k, v := range a.Models {
			if strings.HasPrefix(model, k) {
				p, ok = v, true
				break
			}
		}
	}
	if !ok {
		return 0, 0, a.currency()
	}
	if cached > in {
		cached = in
	}
	cachedPrice := p.CachedPerM
	if cachedPrice == 0 {
		cachedPrice = p.InputPerM
	}
	native = float64(in-cached)*p.InputPerM/1e6 + float64(cached)*cachedPrice/1e6 + float64(out)*p.OutputPerM/1e6
	if a.currency() == "CNY" {
		return native, native / USDPerCNY, "CNY"
	}
	return native, native, "USD"
}

func (a Account) currency() string {
	if strings.EqualFold(a.Currency, "cny") || a.Currency == "¥" {
		return "CNY"
	}
	return "USD"
}

// APIDriver 是 OpenAI 兼容的按量付费驱动，一个驱动管多个账号。
type APIDriver struct {
	mu       sync.RWMutex
	accounts map[string]Account
	order    []string
	// Client 是 HTTP 客户端；零值用 30 秒连接超时、无总超时的默认客户端。
	Client *http.Client
	// Now 便于测试注入时间。
	Now func() time.Time
}

// NewAPIDriver 构造驱动。
func NewAPIDriver(accounts []Account, client *http.Client) *APIDriver {
	d := &APIDriver{accounts: map[string]Account{}, Client: client}
	for _, a := range accounts {
		d.AddAccount(a)
	}
	return d
}

// AddAccount 增加或替换账号。
func (d *APIDriver) AddAccount(a Account) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.accounts[a.ID]; !ok {
		d.order = append(d.order, a.ID)
	}
	d.accounts[a.ID] = a
}

// Account 按 ID 取账号。
func (d *APIDriver) Account(id string) (Account, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	a, ok := d.accounts[id]
	return a, ok
}

// AccountByProvider 取某个供应商的第一个账号。
func (d *APIDriver) AccountByProvider(provider string) (Account, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	for _, id := range d.order {
		if a := d.accounts[id]; a.Provider == provider {
			return a, true
		}
	}
	return Account{}, false
}

// Accounts 返回所有账号（按加入顺序）。
func (d *APIDriver) Accounts() []Account {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]Account, 0, len(d.order))
	for _, id := range d.order {
		out = append(out, d.accounts[id])
	}
	return out
}

// ID 返回渠道标识。
func (d *APIDriver) ID() core.LaneID { return core.LaneAPI }

func (d *APIDriver) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return http.DefaultClient
}

// resolve 根据 target 找账号：先 AccountID，再 Provider。
func (d *APIDriver) resolve(target core.Target) (Account, error) {
	if target.AccountID != "" {
		if a, ok := d.Account(target.AccountID); ok {
			return a, nil
		}
	}
	if target.Provider != "" {
		if a, ok := d.AccountByProvider(target.Provider); ok {
			return a, nil
		}
	}
	return Account{}, &core.ErrProvider{Summary: "按量付费账号不存在：" + target.Provider + "/" + target.AccountID}
}

// ---- 请求体 ----

type chatMessage struct {
	Role       string          `json:"role"`
	Content    interface{}     `json:"content"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
}

type contentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL *struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

type chatRequest struct {
	Model         string          `json:"model"`
	Messages      []chatMessage   `json:"messages"`
	Tools         json.RawMessage `json:"tools,omitempty"`
	ToolChoice    json.RawMessage `json:"tool_choice,omitempty"`
	Stream        bool            `json:"stream,omitempty"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	MaxTokens int `json:"max_tokens,omitempty"`
}

// usageBody 是 OpenAI 兼容的 usage 字段。
type usageBody struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
	// DeepSeek 风格
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"`
}

func (u usageBody) cached() int {
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens > 0 {
		return u.PromptTokensDetails.CachedTokens
	}
	return u.PromptCacheHitTokens
}

type toolCall struct {
	Index    int    `json:"index,omitempty"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type chatResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"message"`
		Delta struct {
			Content   string     `json:"content"`
			ToolCalls []toolCall `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *usageBody `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// imageToDataURL 把本地图片路径转成 data URL；已是 URL 的原样返回。
func imageToDataURL(p string) (string, error) {
	if strings.HasPrefix(p, "data:") || strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p, nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	mt := mime.TypeByExtension(filepath.Ext(p))
	if mt == "" {
		mt = http.DetectContentType(b)
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// BuildChatRequest 把 Task 转成 OpenAI 兼容请求体（导出以便测试）。
func BuildChatRequest(task *core.Task, model string) (*chatRequest, error) {
	req := &chatRequest{Model: model, MaxTokens: task.MaxTokens}
	if task.System != "" {
		req.Messages = append(req.Messages, chatMessage{Role: "system", Content: task.System})
	}
	msgs := task.Messages
	if len(msgs) == 0 {
		msgs = []core.Message{{Role: "user", Content: task.Prompt}}
		if task.ImagePath != "" {
			msgs[0].Images = []string{task.ImagePath}
		}
	}
	for _, m := range msgs {
		if m.Role == "system" && task.System != "" {
			continue // 已经放在最前面
		}
		if len(m.Images) == 0 {
			cm := chatMessage{Role: m.Role, Content: m.Content, ToolCalls: m.ToolCalls, ToolCallID: m.ToolCallID, Name: m.Name}
			if len(m.ToolCalls) > 0 && m.Content == "" {
				cm.Content = nil // 只调工具的助手消息，content 按惯例给 null
			}
			req.Messages = append(req.Messages, cm)
			continue
		}
		parts := []contentPart{}
		if m.Content != "" {
			parts = append(parts, contentPart{Type: "text", Text: m.Content})
		}
		for _, img := range m.Images {
			u, err := imageToDataURL(img)
			if err != nil {
				return nil, &core.ErrProvider{Summary: "读取图片失败：" + img, Raw: err.Error()}
			}
			cp := contentPart{Type: "image_url", ImageURL: &struct {
				URL string `json:"url"`
			}{URL: u}}
			parts = append(parts, cp)
		}
		req.Messages = append(req.Messages, chatMessage{Role: m.Role, Content: parts})
	}
	if task.HasTools() {
		req.Tools = task.Tools
		if len(task.ToolChoice) > 0 && string(task.ToolChoice) != "null" {
			req.ToolChoice = task.ToolChoice
		}
	}
	if task.Stream {
		req.Stream = true
		req.StreamOptions = &struct {
			IncludeUsage bool `json:"include_usage"`
		}{IncludeUsage: true}
	}
	return req, nil
}

// endpoint 拼接 BaseURL 与路径，兼容带或不带 /v1 结尾。
func endpoint(base, path string) string {
	return strings.TrimRight(base, "/") + path
}

// newRequest 构造带认证头的请求。
func (a Account) newRequest(ctx context.Context, method, url string, body []byte) (*http.Request, error) {
	key, err := a.APIKey()
	if err != nil {
		return nil, err
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range a.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// Run 执行文本任务。
func (d *APIDriver) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	acct, err := d.resolve(target)
	if err != nil {
		return nil, err
	}
	model := target.Model
	if model == "" {
		model = task.Model
	}
	if model == "" {
		return nil, &core.ErrProvider{Summary: acct.Provider + " 没有指定模型"}
	}
	body, err := BuildChatRequest(task, model)
	if err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(body)
	req, err := acct.newRequest(ctx, http.MethodPost, endpoint(acct.BaseURL, "/chat/completions"), raw)
	if err != nil {
		return nil, err
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return nil, ClassifyNetError(acct.Provider, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
		return nil, ClassifyHTTPError(acct.Provider, resp.StatusCode, b)
	}
	var res *core.Result
	if body.Stream {
		res, err = d.readStream(resp.Body, emit)
	} else {
		res, err = d.readOnce(resp.Body)
	}
	if err != nil {
		return nil, &core.ErrProvider{Summary: acct.Provider + " 回包无法解析：" + firstLine(err.Error()), Raw: err.Error(), Retryable: true}
	}
	if res.Model == "" {
		res.Model = model
	}
	res.Usage.CostNative, res.Usage.CostUSD, res.Usage.Currency = acct.Cost(model, res.Usage.InputTokens, res.Usage.OutputTokens, res.Usage.CacheReadTokens)
	if emit != nil {
		emit(core.Event{Type: "done"})
	}
	return res, nil
}

func (d *APIDriver) readOnce(r io.Reader) (*core.Result, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var cr chatResponse
	if err := json.Unmarshal(b, &cr); err != nil {
		return nil, err
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return nil, fmt.Errorf("%s", cr.Error.Message)
	}
	res := &core.Result{Model: cr.Model, Raw: json.RawMessage(b)}
	if len(cr.Choices) > 0 {
		res.Text = cr.Choices[0].Message.Content
		res.FinishReason = cr.Choices[0].FinishReason
		res.ToolCalls = marshalToolCalls(cr.Choices[0].Message.ToolCalls)
	}
	if cr.Usage != nil {
		res.Usage.InputTokens = cr.Usage.PromptTokens
		res.Usage.OutputTokens = cr.Usage.CompletionTokens
		res.Usage.CacheReadTokens = cr.Usage.cached()
	}
	return res, nil
}

// readStream 解析 SSE 流：把 delta.content 作为 text_delta 发出，拼装 tool_calls，并从末块取 usage。
func (d *APIDriver) readStream(r io.Reader, emit func(core.Event)) (*core.Result, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	res := &core.Result{}
	var text strings.Builder
	calls := map[int]*toolCall{}
	var finish string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var cr chatResponse
		if err := json.Unmarshal([]byte(data), &cr); err != nil {
			continue
		}
		if cr.Error != nil && cr.Error.Message != "" {
			return nil, fmt.Errorf("%s", cr.Error.Message)
		}
		if cr.Model != "" {
			res.Model = cr.Model
		}
		for _, ch := range cr.Choices {
			if ch.Delta.Content != "" {
				text.WriteString(ch.Delta.Content)
				if emit != nil {
					emit(core.Event{Type: "text_delta", Text: ch.Delta.Content})
				}
			}
			for _, tc := range ch.Delta.ToolCalls {
				cur, ok := calls[tc.Index]
				if !ok {
					c := tc
					calls[tc.Index] = &c
					continue
				}
				if tc.ID != "" {
					cur.ID = tc.ID
				}
				if tc.Function.Name != "" {
					cur.Function.Name = tc.Function.Name
				}
				cur.Function.Arguments += tc.Function.Arguments
			}
			if ch.FinishReason != "" {
				finish = ch.FinishReason
			}
		}
		if cr.Usage != nil {
			res.Usage.InputTokens = cr.Usage.PromptTokens
			res.Usage.OutputTokens = cr.Usage.CompletionTokens
			res.Usage.CacheReadTokens = cr.Usage.cached()
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	res.Text = text.String()
	// 把拼好的消息放进 Raw，门面层需要 tool_calls 时从这里取。
	var tcs []toolCall
	for i := 0; i < len(calls); i++ {
		if c, ok := calls[i]; ok {
			tcs = append(tcs, *c)
		}
	}
	rawMsg := map[string]interface{}{"role": "assistant", "content": res.Text, "finish_reason": finish}
	if len(tcs) > 0 {
		rawMsg["tool_calls"] = tcs
	}
	res.Raw, _ = json.Marshal(rawMsg)
	res.FinishReason = finish
	res.ToolCalls = marshalToolCalls(tcs)
	return res, nil
}

// marshalToolCalls 把工具调用整理成 OpenAI 非流式格式的数组：补上 type，去掉流式才有的 index。没有返回 nil。
func marshalToolCalls(tcs []toolCall) json.RawMessage {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tcs))
	for _, c := range tcs {
		typ := c.Type
		if typ == "" {
			typ = "function"
		}
		out = append(out, map[string]any{"id": c.ID, "type": typ, "function": map[string]string{"name": c.Function.Name, "arguments": c.Function.Arguments}})
	}
	b, _ := json.Marshal(out)
	return b
}

// Health 只检查是否有账号与密钥可取，不发任何请求。
func (d *APIDriver) Health(ctx context.Context) core.Health {
	accts := d.Accounts()
	if len(accts) == 0 {
		return core.Health{Status: "disabled", Detail: "还没有添加按量付费账号"}
	}
	ready := 0
	var missing []string
	for _, a := range accts {
		if _, err := a.APIKey(); err == nil {
			ready++
		} else {
			missing = append(missing, a.Provider)
		}
	}
	if ready == 0 {
		return core.Health{Status: "warn", Detail: "账号都没有密钥：" + strings.Join(missing, "、")}
	}
	if len(missing) > 0 {
		return core.Health{Status: "warn", Detail: fmt.Sprintf("%d 个账号可用；缺密钥：%s", ready, strings.Join(missing, "、"))}
	}
	return core.Health{Status: "ok", Detail: fmt.Sprintf("%d 个账号可用", ready)}
}
