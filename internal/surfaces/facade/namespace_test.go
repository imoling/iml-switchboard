package facade

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Codex 把每个 MCP 服务器的工具包成 type=namespace 的一组（Switchboard 的 generate_image 就在 mcp__iml_switchboard 里）。
// 以前门面只认 function，整组丢掉，接管后的 Codex 看不到画师，只会自己写 SVG 或 curl。
func TestResponsesNamespaceTools(t *testing.T) {
	var got map[string]any
	ts := setupWithUpstream(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		call := `{"id":"call_9","type":"function","function":{"name":"mcp__iml_switchboard__generate_image","arguments":"{\"prompt\":\"Q 版\"}"}}`
		if got["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", `{"model":"glm-5.3","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_9","type":"function","function":{"name":"mcp__iml_switchboard__generate_image","arguments":"{\"prompt\":\"Q 版\"}"}}]},"finish_reason":"tool_calls"}]}`)
			fmt.Fprint(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"model":"glm-5.3","choices":[{"message":{"role":"assistant","content":null,"tool_calls":[%s]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, call)
	})
	body := map[string]any{
		"model": "router-mid",
		"tools": []map[string]any{
			{"type": "function", "name": "exec_command", "parameters": map[string]any{"type": "object"}},
			{"type": "namespace", "name": "mcp__iml_switchboard", "description": "需要成品图先调 generate_image", "tools": []map[string]any{
				{"type": "function", "name": "generate_image", "description": "生成图片", "parameters": map[string]any{"type": "object"}},
			}},
		},
		"input": []map[string]any{
			{"type": "message", "role": "user", "content": "先画一张草图"},
			{"type": "function_call", "call_id": "call_1", "name": "generate_image", "namespace": "mcp__iml_switchboard", "arguments": `{"prompt":"草图"}`},
			{"type": "function_call_output", "call_id": "call_1", "output": "已生成 /tmp/a.png"},
			{"type": "message", "role": "user", "content": "再改成 3D 软胶风格"},
		},
	}
	for _, stream := range []bool{false, true} {
		body["stream"] = stream
		resp, out := postResp(ts, "k", body)
		if resp.StatusCode != 200 {
			t.Fatalf("%d %s", resp.StatusCode, out)
		}
		up, _ := json.Marshal(got)
		if !strings.Contains(string(up), `"name":"mcp__iml_switchboard__generate_image"`) || !strings.Contains(string(up), `"name":"exec_command"`) {
			t.Fatalf("上游应收到拍平后的工具和历史调用：%s", up)
		}
		if !strings.Contains(string(up), "需要成品图先调 generate_image") {
			t.Fatalf("工具组的说明要带给上游：%s", up)
		}
		if !strings.Contains(out, `"namespace":"mcp__iml_switchboard"`) || !strings.Contains(out, `"name":"generate_image"`) || strings.Contains(out, `"name":"mcp__iml_switchboard__generate_image"`) {
			t.Fatalf("回给 Codex 的工具调用要拆回 namespace 和 name（stream=%v）：%s", stream, out)
		}
	}
}

func TestFlatToolNameLimit(t *testing.T) {
	long := flatToolName(strings.Repeat("n", 50), strings.Repeat("t", 30))
	if len(long) > 64 || long != flatToolName(strings.Repeat("n", 50), strings.Repeat("t", 30)) {
		t.Fatalf("超长名字要截到 64 以内且稳定：%q", long)
	}
}
