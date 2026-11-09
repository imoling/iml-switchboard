package facade

// Anthropic Messages 接口：Claude Code、Claude Desktop、Cline 这类按 Anthropic 协议说话的宿主，把 Switchboard 当成它们的模型。
// 内部的消息、工具仍是 OpenAI 格式（按量付费渠道只认这一种），这里只做两头的转换；流式按 Anthropic 的事件序列回。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

type anthReq struct {
	Model      string          `json:"model"`
	MaxTokens  int             `json:"max_tokens"`
	System     json.RawMessage `json:"system"`
	Messages   []anthMessage   `json:"messages"`
	Tools      []anthTool      `json:"tools"`
	ToolChoice json.RawMessage `json:"tool_choice"`
	Stream     bool            `json:"stream"`
	Metadata   struct {
		UserID string `json:"user_id"`
	} `json:"metadata"`
}

type anthMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
	Type        string          `json:"type"`
}

type anthBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	Source    struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	} `json:"source"`
}

func anthErr(w http.ResponseWriter, code int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "error", "error": map[string]any{"type": typ, "message": msg}})
}

// blocksText 把 content（字符串或分段数组）里的文字拼起来；顺手收集图片（转成 data URL）。
func blocksText(raw json.RawMessage) (string, []string) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var blocks []anthBlock
	if json.Unmarshal(raw, &blocks) != nil {
		return strings.TrimSpace(string(raw)), nil
	}
	var sb strings.Builder
	var imgs []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if sb.Len() > 0 && b.Text != "" {
				sb.WriteString("\n")
			}
			sb.WriteString(b.Text)
		case "image":
			switch b.Source.Type {
			case "base64":
				imgs = append(imgs, "data:"+b.Source.MediaType+";base64,"+b.Source.Data)
			case "url":
				imgs = append(imgs, b.Source.URL)
			}
		}
	}
	return sb.String(), imgs
}

// toTask 把 Anthropic 请求转成内部任务。
func (s *Server) toTask(req anthReq, r *http.Request) (*core.Task, error) {
	t := &core.Task{Kind: core.KindText, Source: "facade", Host: hostOf(r), MaxTokens: req.MaxTokens, Stream: req.Stream, SessionID: req.Metadata.UserID}
	t.System, _ = blocksText(req.System)
	// 工具：Anthropic 的 input_schema 就是 OpenAI 的 parameters
	var tools []map[string]any
	for _, tl := range req.Tools {
		if tl.Name == "" {
			continue // 服务端内置工具（联网搜索之类）这里没有
		}
		fn := map[string]any{"name": tl.Name, "description": tl.Description}
		if len(tl.InputSchema) > 0 {
			fn["parameters"] = tl.InputSchema
		}
		tools = append(tools, map[string]any{"type": "function", "function": fn})
	}
	if len(tools) > 0 {
		t.Tools, _ = json.Marshal(tools)
	}
	if len(req.ToolChoice) > 0 {
		var tc struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.ToolChoice, &tc)
		switch tc.Type {
		case "any":
			t.ToolChoice = json.RawMessage(`"required"`)
		case "tool":
			t.ToolChoice, _ = json.Marshal(map[string]any{"type": "function", "function": map[string]string{"name": tc.Name}})
		case "none":
			t.ToolChoice = json.RawMessage(`"none"`)
		}
	}
	for _, m := range req.Messages {
		var blocks []anthBlock
		isBlocks := json.Unmarshal(m.Content, &blocks) == nil && len(m.Content) > 0 && m.Content[0] == '['
		if !isBlocks {
			text, imgs := blocksText(m.Content)
			t.Messages = append(t.Messages, core.Message{Role: m.Role, Content: text, Images: imgs})
			continue
		}
		switch m.Role {
		case "assistant":
			var text strings.Builder
			var calls []map[string]any
			for _, b := range blocks {
				switch b.Type {
				case "text":
					text.WriteString(b.Text)
				case "tool_use":
					args := "{}"
					if len(b.Input) > 0 {
						args = string(b.Input)
					}
					calls = append(calls, map[string]any{"id": b.ID, "type": "function", "function": map[string]string{"name": b.Name, "arguments": args}})
				}
			}
			msg := core.Message{Role: "assistant", Content: text.String()}
			if len(calls) > 0 {
				msg.ToolCalls, _ = json.Marshal(calls)
			}
			t.Messages = append(t.Messages, msg)
		default: // user：文字和图片合成一条，每个工具结果单独一条 tool 消息
			var text strings.Builder
			var imgs []string
			userImage := false
			for _, b := range blocks {
				switch b.Type {
				case "tool_result":
					content, more := blocksText(b.Content)
					imgs = append(imgs, more...)
					if b.IsError && content == "" {
						content = "工具执行失败"
					}
					t.Messages = append(t.Messages, core.Message{Role: "tool", Content: content, ToolCallID: b.ToolUseID})
				case "text":
					if text.Len() > 0 {
						text.WriteString("\n")
					}
					text.WriteString(b.Text)
				case "image":
					_, more := blocksText(json.RawMessage(mustJSON([]anthBlock{b})))
					imgs = append(imgs, more...)
					userImage = true
				}
			}
			if text.Len() > 0 || len(imgs) > 0 {
				t.Messages = append(t.Messages, core.Message{Role: "user", Content: text.String(), Images: imgs, ToolImage: text.Len() == 0 && !userImage})
			}
		}
	}
	for i := len(t.Messages) - 1; i >= 0; i-- {
		if t.Messages[i].Role == "user" {
			t.Prompt = t.Messages[i].Content
			if len(t.Messages[i].Images) > 0 {
				t.ImagePath = t.Messages[i].Images[0]
				t.RefImages = t.Messages[i].Images[1:] // 出图时第二张起也当参考图
			}
			break
		}
	}
	if t.Prompt == "" {
		if len(t.Messages) == 0 {
			return nil, errors.New("没有用户消息")
		}
		t.Prompt = "（继续处理工具结果）"
	}
	if t.SessionID == "" && len(t.Messages) > 1 {
		t.SessionID = "facade:" + hash(t.Messages[0].Content)
	}
	t.HostModel = req.Model
	applyModelName(t, req.Model, s.accountModel)
	return t, nil
}

func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// applyModelName 把宿主选的模型名落到任务上：router-auto 自动分档，router-high/mid/low 指定档位，
// Claude 自家的 opus/sonnet/haiku 名字按高/中/低档理解（接管后没有订阅可走），其余当成点名某个型号。
func applyModelName(t *core.Task, model string, known func(string) bool) {
	if t.Host == "claude_desktop" {
		model = core.DesktopRoute(model) // 它的菜单里全是借的名字，只有它按这张表理解
	}
	// 账号里配了的型号就是点名，哪怕名字像 gpt-* / claude-*：自定义服务的 gpt-6.1-sol 要真的发到那个服务
	if known != nil && known(model) {
		t.Model = model
		return
	}
	m := strings.ToLower(model)
	switch {
	case m == "" || m == "router-auto":
	case m == "router-smart":
		t.Smart = true
	case m == "router-high":
		t.Tier = core.TierHigh
	case m == "router-mid":
		t.Tier = core.TierMid
	case m == "router-low":
		t.Tier = core.TierLow
	case m == "router-sub":
		t.Lane = core.LaneClaudeSub
	case strings.HasPrefix(m, "claude") || m == "opus" || m == "sonnet" || m == "haiku" || m == "fable":
		switch {
		case strings.Contains(m, "haiku"):
			t.Tier = core.TierLow
		case strings.Contains(m, "sonnet"):
			t.Tier = core.TierMid
		default:
			t.Tier = core.TierHigh
		}
	case m == "codex-auto-review":
		t.Tier = core.TierMid // Codex 的自动审批点名这个，它不是真实型号
	case strings.HasPrefix(m, "gpt-") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") || strings.HasPrefix(m, "o4"):
		// Codex 接管后，它内部那些小请求点名的 OpenAI 型号也按档位走，别去起订阅命令行
		switch {
		case strings.Contains(m, "luna") || strings.Contains(m, "mini") || strings.Contains(m, "nano"):
			t.Tier = core.TierLow
		case strings.Contains(m, "sol") || strings.Contains(m, "pro"):
			t.Tier = core.TierHigh
		default:
			t.Tier = core.TierMid
		}
	default:
		t.Model = model
	}
}

// toolUses 把 OpenAI 格式的 tool_calls 转成 Anthropic 的 tool_use 块。
func toolUses(raw json.RawMessage) []map[string]any {
	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	_ = json.Unmarshal(raw, &calls)
	var out []map[string]any
	for i, c := range calls {
		var input any = map[string]any{}
		if json.Unmarshal([]byte(c.Function.Arguments), &input) != nil || input == nil {
			input = map[string]any{"_raw": c.Function.Arguments}
		}
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("toolu_%s_%d", core.NewID("")[1:9], i)
		}
		out = append(out, map[string]any{"type": "tool_use", "id": id, "name": c.Function.Name, "input": input})
	}
	return out
}

func anthStopReason(res *core.Result) string {
	switch finishReason(res) {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	}
	return "end_turn"
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	var req anthReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&req); err != nil {
		anthErr(w, 400, "invalid_request_error", "请求体不是合法 JSON")
		return
	}
	t, err := s.toTask(req, r)
	if err != nil {
		anthErr(w, 400, "invalid_request_error", err.Error())
		return
	}
	est := core.EstimateTokens(t.System)
	for _, m := range t.Messages {
		est += core.EstimateTokens(m.Content)
		for _, img := range m.Images {
			est += len(img) / 4 // data URL 的 base64 部分按字符除 4 粗估 token
		}
	}
	if err := s.router.FitsAny(est); err != nil {
		t.EstTokens = est
		s.router.Bounce(t, err)
		anthErr(w, 400, "invalid_request_error", "prompt is too long: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	id := "msg_" + core.NewID("")[1:]
	if !req.Stream {
		call, res, err := s.router.Run(ctx, t, nil)
		if err != nil {
			var tl *core.ErrContextTooLong
			if errors.As(err, &tl) {
				anthErr(w, 400, "invalid_request_error", "prompt is too long: "+err.Error())
				return
			}
			anthErr(w, 502, "api_error", err.Error())
			return
		}
		res.Text = s.replyText(call, res)
		content := []map[string]any{}
		if res.Text != "" {
			content = append(content, map[string]any{"type": "text", "text": res.Text})
		}
		content = append(content, toolUses(res.ToolCalls)...)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-IMLS-Call-Id", call.ID)
		w.Header().Set("X-IMLS-Model", call.Model)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": id, "type": "message", "role": "assistant", "model": call.Model, "content": content,
			"stop_reason": anthStopReason(res), "stop_sequence": nil,
			"usage": map[string]int{"input_tokens": call.InputTokens, "output_tokens": call.OutputTokens},
		})
		return
	}

	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	send := func(event string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if fl != nil {
			fl.Flush()
		}
	}
	send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": id, "type": "message", "role": "assistant", "model": req.Model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]int{"input_tokens": 0, "output_tokens": 0}}})
	index := -1
	textOpen := false
	call, res, err := s.router.Run(ctx, t, func(e core.Event) {
		if e.Type != "text_delta" || e.Text == "" {
			return
		}
		if !textOpen {
			index++
			textOpen = true
			send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "text", "text": ""}})
		}
		send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": e.Text}})
	})
	if err != nil {
		send("error", map[string]any{"type": "error", "error": map[string]any{"type": "api_error", "message": err.Error()}})
		return
	}
	res.Text = s.replyText(call, res)
	if !textOpen && res.Text != "" {
		index++
		textOpen = true
		send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "text", "text": ""}})
		send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "text_delta", "text": res.Text}})
	}
	if textOpen {
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}
	for _, tu := range toolUses(res.ToolCalls) {
		index++
		args, _ := json.Marshal(tu["input"])
		send("content_block_start", map[string]any{"type": "content_block_start", "index": index, "content_block": map[string]any{"type": "tool_use", "id": tu["id"], "name": tu["name"], "input": map[string]any{}}})
		send("content_block_delta", map[string]any{"type": "content_block_delta", "index": index, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(args)}})
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": index})
	}
	send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": anthStopReason(res), "stop_sequence": nil}, "usage": map[string]int{"input_tokens": call.InputTokens, "output_tokens": call.OutputTokens}})
	send("message_stop", map[string]any{"type": "message_stop"})
}

// countTokens 给宿主一个估算；Claude Code 用它决定什么时候压缩对话。
func (s *Server) countTokens(w http.ResponseWriter, r *http.Request) {
	var req anthReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&req); err != nil {
		anthErr(w, 400, "invalid_request_error", "请求体不是合法 JSON")
		return
	}
	n := 0
	if sys, _ := blocksText(req.System); sys != "" {
		n += core.EstimateTokens(sys)
	}
	for _, m := range req.Messages {
		text, imgs := blocksText(m.Content)
		n += core.EstimateTokens(text)
		for _, img := range imgs {
			n += len(img) / 4 // data URL 的 base64 部分按字符除 4 粗估 token
		}
	}
	for _, tl := range req.Tools {
		n += core.EstimateTokens(tl.Description) + len(tl.InputSchema)/4
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"input_tokens": n})
}
