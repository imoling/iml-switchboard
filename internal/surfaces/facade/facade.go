// Package facade 是 OpenAI 兼容门面：/v1/models、/v1/chat/completions、/v1/images/generations。
// 给 WorkBuddy 的自定义模型和 Hermes 的 image_gen.openai.base_url 用。
package facade

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// Server 是门面处理器。
type Server struct {
	router *core.Router
	key    func() string
}

// New 创建门面。key 返回当前 facade_key。
func New(r *core.Router, key func() string) *Server { return &Server{router: r, key: key} }

// Routes 注册。
func (s *Server) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/models", s.auth(s.models))
	mux.HandleFunc("POST /v1/chat/completions", s.auth(s.chat))
	mux.HandleFunc("POST /v1/images/generations", s.auth(s.images))
}

func (s *Server) auth(fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" || got != s.key() {
			oaiErr(w, 401, "invalid_api_key", "密钥不对：到设置里复制门面密钥")
			return
		}
		fn(w, r)
	}
}

func oaiErr(w http.ResponseWriter, code int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"message": msg, "type": typ, "code": typ}})
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	now := time.Now().Unix()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{
		{"id": "router-auto", "object": "model", "created": now, "owned_by": "iml-switchboard"},
		{"id": "router-sub", "object": "model", "created": now, "owned_by": "iml-switchboard"},
	}})
}

type oaiMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type chatReq struct {
	Model     string          `json:"model"`
	Messages  []oaiMessage    `json:"messages"`
	Stream    bool            `json:"stream"`
	Tools     json.RawMessage `json:"tools"`
	MaxTokens int             `json:"max_tokens"`
	User      string          `json:"user"`
}

// 把 OpenAI 的 content（字符串或分段数组）拍平成文本 + 图片列表。
func flatten(raw json.RawMessage) (string, []string) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return string(raw), nil
	}
	var sb strings.Builder
	var imgs []string
	for _, p := range parts {
		switch p.Type {
		case "text":
			sb.WriteString(p.Text)
		case "image_url":
			imgs = append(imgs, p.ImageURL.URL)
		}
	}
	return sb.String(), imgs
}

func (s *Server) chat(w http.ResponseWriter, r *http.Request) {
	var req chatReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<20)).Decode(&req); err != nil {
		oaiErr(w, 400, "invalid_request_error", "请求体不是合法 JSON")
		return
	}
	t := &core.Task{Kind: core.KindText, Source: "facade", Host: hostOf(r), Tools: req.Tools, MaxTokens: req.MaxTokens, Stream: req.Stream, SessionID: req.User}
	hasTools := len(req.Tools) > 0 && string(req.Tools) != "null" && string(req.Tools) != "[]"
	if req.Model == "router-sub" && hasTools {
		oaiErr(w, 400, "invalid_request_error", "router-sub 只接不带工具的请求，带工具请改用 router-auto")
		return
	}
	if req.Model == "router-sub" {
		t.Lane = core.LaneClaudeSub
	}
	for _, m := range req.Messages {
		text, imgs := flatten(m.Content)
		if m.Role == "system" {
			t.System += text + "\n"
			continue
		}
		t.Messages = append(t.Messages, core.Message{Role: m.Role, Content: text, Images: imgs})
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
		oaiErr(w, 400, "invalid_request_error", "没有用户消息")
		return
	}
	if t.SessionID == "" && len(t.Messages) > 1 {
		// 多轮对话没有 user 字段时，用首条消息做会话键，保证粘性
		t.SessionID = "facade:" + hash(t.Messages[0].Content)
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Minute)
	defer cancel()
	id := "chatcmpl-" + core.NewID("")[1:]
	created := time.Now().Unix()
	if !req.Stream {
		call, res, err := s.router.Run(ctx, t, nil)
		if err != nil {
			oaiErr(w, 502, "upstream_error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-IMLS-Call-Id", call.ID)
		w.Header().Set("X-IMLS-Model", call.Model)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": id, "object": "chat.completion", "created": created, "model": call.Model,
			"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": res.Text}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": call.InputTokens, "completion_tokens": call.OutputTokens, "total_tokens": call.InputTokens + call.OutputTokens},
		})
		return
	}
	fl, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(200)
	chunk := func(delta map[string]any, finish any) {
		b, _ := json.Marshal(map[string]any{"id": id, "object": "chat.completion.chunk", "created": created, "model": "router", "choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": finish}}})
		fmt.Fprintf(w, "data: %s\n\n", b)
		if fl != nil {
			fl.Flush()
		}
	}
	chunk(map[string]any{"role": "assistant", "content": ""}, nil)
	streamed := false
	call, res, err := s.router.Run(ctx, t, func(e core.Event) {
		if e.Type == "text_delta" && e.Text != "" {
			streamed = true
			chunk(map[string]any{"content": e.Text}, nil)
		}
	})
	if err != nil {
		b, _ := json.Marshal(map[string]any{"error": map[string]any{"message": err.Error(), "type": "upstream_error"}})
		fmt.Fprintf(w, "data: %s\n\n", b)
	} else {
		if !streamed && res != nil {
			chunk(map[string]any{"content": res.Text}, nil)
		}
		chunk(map[string]any{}, "stop")
		_ = call
	}
	fmt.Fprint(w, "data: [DONE]\n\n")
	if fl != nil {
		fl.Flush()
	}
}

func hash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}

func hostOf(r *http.Request) string {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	switch {
	case strings.Contains(ua, "workbuddy"):
		return "workbuddy"
	case strings.Contains(ua, "hermes"):
		return "hermes"
	}
	if h := r.Header.Get("X-IMLS-Host"); h != "" {
		return h
	}
	return "openai_compatible"
}

func (s *Server) images(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Prompt         string `json:"prompt"`
		Size           string `json:"size"`
		Quality        string `json:"quality"`
		N              int    `json:"n"`
		ResponseFormat string `json:"response_format"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		oaiErr(w, 400, "invalid_request_error", "请求体不是合法 JSON")
		return
	}
	tier := "default"
	switch strings.ToLower(req.Quality) {
	case "hd", "high":
		tier = "best"
	case "low", "standard":
		tier = "cheap"
	}
	t := &core.Task{Kind: core.KindImage, Prompt: req.Prompt, Size: req.Size, MediaTier: tier, Source: "facade", Host: hostOf(r)}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	call, res, err := s.router.Run(ctx, t, nil)
	if err != nil {
		oaiErr(w, 502, "upstream_error", err.Error())
		return
	}
	data, err := os.ReadFile(res.Path)
	if err != nil {
		oaiErr(w, 500, "server_error", "产物读取失败")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-IMLS-Call-Id", call.ID)
	w.Header().Set("X-IMLS-Model", call.Model)
	_ = json.NewEncoder(w).Encode(map[string]any{"created": time.Now().Unix(), "data": []map[string]any{{"b64_json": base64.StdEncoding.EncodeToString(data), "revised_prompt": req.Prompt, "path": res.Path}}})
}
