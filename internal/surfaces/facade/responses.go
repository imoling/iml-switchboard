package facade

// OpenAI Responses 接口：Codex 只认这个协议（Chat Completions 已被它弃用）。
// 内部仍是 Chat 格式的消息与工具，这里做两头转换；流式按 Responses 的事件序列回。

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

type respReq struct {
	Model           string          `json:"model"`
	Instructions    string          `json:"instructions"`
	Input           json.RawMessage `json:"input"`
	Tools           []respTool      `json:"tools"`
	ToolChoice      json.RawMessage `json:"tool_choice"`
	Stream          bool            `json:"stream"`
	MaxOutputTokens int             `json:"max_output_tokens"`
	PromptCacheKey  string          `json:"prompt_cache_key"`
	User            string          `json:"user"`
}

type respTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

type respItem struct {
	Type      string          `json:"type"`
	Role      string          `json:"role"`
	Content   json.RawMessage `json:"content"`
	CallID    string          `json:"call_id"`
	Name      string          `json:"name"`
	Arguments string          `json:"arguments"`
	Output    json.RawMessage `json:"output"`
}

type respPart struct {
	Type     string `json:"type"`
	Text     string `json:"text"`
	ImageURL string `json:"image_url"`
}

// partsText 把 Responses 的 content（字符串或分段）拍平成文字和图片。
func partsText(raw json.RawMessage) (string, []string) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var parts []respPart
	if json.Unmarshal(raw, &parts) != nil {
		return strings.TrimSpace(string(raw)), nil
	}
	var sb strings.Builder
	var imgs []string
	for _, p := range parts {
		switch p.Type {
		case "input_text", "output_text", "text":
			if sb.Len() > 0 && p.Text != "" {
				sb.WriteString("\n")
			}
			sb.WriteString(p.Text)
		case "input_image":
			if p.ImageURL != "" {
				imgs = append(imgs, p.ImageURL)
			}
		}
	}
	return sb.String(), imgs
}

func (s *Server) respToTask(req respReq, r *http.Request) (*core.Task, error) {
	t := &core.Task{Kind: core.KindText, Source: "facade", Host: hostOf(r), MaxTokens: req.MaxOutputTokens, Stream: req.Stream, SessionID: req.PromptCacheKey}
	if t.SessionID == "" {
		t.SessionID = req.User
	}
	t.System = req.Instructions
	var tools []map[string]any
	for _, tl := range req.Tools {
		if tl.Type != "function" || tl.Name == "" {
			continue // local_shell、web_search 这类内置工具这里没有
		}
		fn := map[string]any{"name": tl.Name, "description": tl.Description}
		if len(tl.Parameters) > 0 {
			fn["parameters"] = tl.Parameters
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
		if json.Unmarshal(req.ToolChoice, &tc) == nil && tc.Type == "function" {
			t.ToolChoice, _ = json.Marshal(map[string]any{"type": "function", "function": map[string]string{"name": tc.Name}})
		} else {
			t.ToolChoice = req.ToolChoice // "auto" / "required" / "none" 两边一样
		}
	}
	// input：一段字符串就是一条用户消息；数组按条转换
	var text string
	if json.Unmarshal(req.Input, &text) == nil {
		t.Messages = append(t.Messages, core.Message{Role: "user", Content: text})
	} else {
		var items []respItem
		if err := json.Unmarshal(req.Input, &items); err != nil {
			return nil, errors.New("input 不是预期的格式")
		}
		for _, it := range items {
			switch {
			case it.Type == "function_call":
				call, _ := json.Marshal([]map[string]any{{"id": it.CallID, "type": "function", "function": map[string]string{"name": it.Name, "arguments": it.Arguments}}})
				// 紧跟在助手消息后面的调用合并进去，否则单独一条助手消息
				if n := len(t.Messages); n > 0 && t.Messages[n-1].Role == "assistant" && len(t.Messages[n-1].ToolCalls) == 0 {
					t.Messages[n-1].ToolCalls = call
				} else {
					t.Messages = append(t.Messages, core.Message{Role: "assistant", ToolCalls: call})
				}
			case it.Type == "function_call_output":
				out, _ := partsText(it.Output)
				t.Messages = append(t.Messages, core.Message{Role: "tool", Content: out, ToolCallID: it.CallID})
			case it.Type == "message" || it.Type == "":
				content, imgs := partsText(it.Content)
				role := it.Role
				if role == "developer" || role == "system" {
					if t.System != "" {
						t.System += "\n\n"
					}
					t.System += content
					continue
				}
				if role == "" {
					role = "user"
				}
				t.Messages = append(t.Messages, core.Message{Role: role, Content: content, Images: imgs})
			}
		}
	}
	for i := len(t.Messages) - 1; i >= 0; i-- {
		if t.Messages[i].Role == "user" {
			t.Prompt = t.Messages[i].Content
			if len(t.Messages[i].Images) > 0 {
				t.ImagePath = t.Messages[i].Images[0]
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
	applyModelName(t, req.Model)
	return t, nil
}

// respOutput 把结果变成 Responses 的 output 数组。
func respOutput(res *core.Result) []map[string]any {
	var out []map[string]any
	if res.Text != "" {
		out = append(out, map[string]any{"type": "message", "id": "msg_" + core.NewID("")[1:], "status": "completed", "role": "assistant",
			"content": []map[string]any{{"type": "output_text", "text": res.Text, "annotations": []any{}}}})
	}
	var calls []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	_ = json.Unmarshal(res.ToolCalls, &calls)
	for i, c := range calls {
		id := c.ID
		if id == "" {
			id = fmt.Sprintf("call_%s_%d", core.NewID("")[1:9], i)
		}
		args := c.Function.Arguments
		if args == "" {
			args = "{}"
		}
		out = append(out, map[string]any{"type": "function_call", "id": "fc_" + core.NewID("")[1:], "call_id": id, "name": c.Function.Name, "arguments": args, "status": "completed"})
	}
	return out
}

func respObject(id, model, status string, output []map[string]any, call *core.Call) map[string]any {
	usage := map[string]any{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0, "input_tokens_details": map[string]int{"cached_tokens": 0}, "output_tokens_details": map[string]int{"reasoning_tokens": 0}}
	if call != nil {
		usage["input_tokens"], usage["output_tokens"], usage["total_tokens"] = call.InputTokens, call.OutputTokens, call.InputTokens+call.OutputTokens
		usage["input_tokens_details"] = map[string]int{"cached_tokens": call.CacheReadTokens}
	}
	if output == nil {
		output = []map[string]any{}
	}
	return map[string]any{"id": id, "object": "response", "created_at": time.Now().Unix(), "status": status, "model": model,
		"output": output, "usage": usage, "incomplete_details": nil, "error": nil, "parallel_tool_calls": true, "tool_choice": "auto", "tools": []any{}}
}

func (s *Server) responses(w http.ResponseWriter, r *http.Request) {
	var req respReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&req); err != nil {
		oaiErr(w, 400, "invalid_request_error", "请求体不是合法 JSON")
		return
	}
	t, err := s.respToTask(req, r)
	if err != nil {
		oaiErr(w, 400, "invalid_request_error", err.Error())
		return
	}
	est := core.EstimateTokens(t.System)
	for _, m := range t.Messages {
		est += core.EstimateTokens(m.Content)
	}
	if err := s.router.FitsAny(est); err != nil {
		contextTooLong(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	id := "resp_" + core.NewID("")[1:]
	if !req.Stream {
		call, res, err := s.router.Run(ctx, t, nil)
		if err != nil {
			var tl *core.ErrContextTooLong
			if errors.As(err, &tl) {
				contextTooLong(w, err)
				return
			}
			oaiErr(w, 502, "upstream_error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-IMLS-Call-Id", call.ID)
		w.Header().Set("X-IMLS-Model", call.Model)
		_ = json.NewEncoder(w).Encode(respObject(id, call.Model, "completed", respOutput(res), call))
		return
	}

	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	seq := 0
	send := func(event string, data map[string]any) {
		data["type"] = event
		data["sequence_number"] = seq
		seq++
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b)
		if fl != nil {
			fl.Flush()
		}
	}
	send("response.created", map[string]any{"response": respObject(id, req.Model, "in_progress", nil, nil)})
	send("response.in_progress", map[string]any{"response": respObject(id, req.Model, "in_progress", nil, nil)})
	outIndex := -1
	msgID := ""
	var textSB strings.Builder
	call, res, err := s.router.Run(ctx, t, func(e core.Event) {
		if e.Type != "text_delta" || e.Text == "" {
			return
		}
		if msgID == "" {
			outIndex++
			msgID = "msg_" + core.NewID("")[1:]
			send("response.output_item.added", map[string]any{"output_index": outIndex, "item": map[string]any{"type": "message", "id": msgID, "status": "in_progress", "role": "assistant", "content": []any{}}})
			send("response.content_part.added", map[string]any{"output_index": outIndex, "item_id": msgID, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
		}
		textSB.WriteString(e.Text)
		send("response.output_text.delta", map[string]any{"output_index": outIndex, "item_id": msgID, "content_index": 0, "delta": e.Text})
	})
	if err != nil {
		send("response.failed", map[string]any{"response": map[string]any{"id": id, "object": "response", "status": "failed", "error": map[string]any{"code": "upstream_error", "message": err.Error()}}})
		return
	}
	if msgID == "" && res.Text != "" {
		outIndex++
		msgID = "msg_" + core.NewID("")[1:]
		send("response.output_item.added", map[string]any{"output_index": outIndex, "item": map[string]any{"type": "message", "id": msgID, "status": "in_progress", "role": "assistant", "content": []any{}}})
		send("response.content_part.added", map[string]any{"output_index": outIndex, "item_id": msgID, "content_index": 0, "part": map[string]any{"type": "output_text", "text": "", "annotations": []any{}}})
		textSB.WriteString(res.Text)
		send("response.output_text.delta", map[string]any{"output_index": outIndex, "item_id": msgID, "content_index": 0, "delta": res.Text})
	}
	var output []map[string]any
	if msgID != "" {
		text := textSB.String()
		item := map[string]any{"type": "message", "id": msgID, "status": "completed", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": text, "annotations": []any{}}}}
		send("response.output_text.done", map[string]any{"output_index": outIndex, "item_id": msgID, "content_index": 0, "text": text})
		send("response.content_part.done", map[string]any{"output_index": outIndex, "item_id": msgID, "content_index": 0, "part": map[string]any{"type": "output_text", "text": text, "annotations": []any{}}})
		send("response.output_item.done", map[string]any{"output_index": outIndex, "item": item})
		output = append(output, item)
	}
	for _, fc := range respOutput(&core.Result{ToolCalls: res.ToolCalls}) {
		outIndex++
		pending := map[string]any{}
		for k, v := range fc {
			pending[k] = v
		}
		pending["arguments"], pending["status"] = "", "in_progress"
		send("response.output_item.added", map[string]any{"output_index": outIndex, "item": pending})
		send("response.function_call_arguments.delta", map[string]any{"output_index": outIndex, "item_id": fc["id"], "delta": fc["arguments"]})
		send("response.function_call_arguments.done", map[string]any{"output_index": outIndex, "item_id": fc["id"], "arguments": fc["arguments"]})
		send("response.output_item.done", map[string]any{"output_index": outIndex, "item": fc})
		output = append(output, fc)
	}
	send("response.completed", map[string]any{"response": respObject(id, call.Model, "completed", output, call)})
}
