package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// 假后台：只回 /api/internal/tool，证明 HTTP 入口和工具转发是通的。
func TestHTTPHandlerRoundTrip(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/internal/tool" || r.Header.Get("X-IMLS-Client") != "mcp" {
			t.Errorf("意外请求 %s", r.URL.Path)
		}
		var in struct {
			Name string          `json:"name"`
			Host string          `json:"host"`
			Args json.RawMessage `json:"args"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "ok:" + in.Name + ":" + in.Host + ":" + string(in.Args)})
	}))
	defer backend.Close()

	front := httptest.NewServer(HTTPHandler(backend.URL, "test", func() string { return "k1" }))
	defer front.Close()

	// 没带密钥要被拒
	resp, _ := http.Post(front.URL, "application/json", strings.NewReader(`{}`))
	if resp.StatusCode != 401 {
		t.Fatalf("无密钥应 401，得到 %d", resp.StatusCode)
	}

	hc := &http.Client{Transport: headerRT{key: "k1", host: "doubao_work"}}
	client := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	sess, err := client.Connect(context.Background(), &sdk.StreamableClientTransport{Endpoint: front.URL, HTTPClient: hc}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	tools, err := sess.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 5 {
		t.Fatalf("应有 5 个工具，得到 %d", len(tools.Tools))
	}
	res, err := sess.CallTool(context.Background(), &sdk.CallToolParams{Name: "get_job", Arguments: map[string]any{"job_id": "j1"}})
	if err != nil {
		t.Fatal(err)
	}
	got := res.Content[0].(*sdk.TextContent).Text
	if !strings.HasPrefix(got, "ok:get_job:doubao_work:") || !strings.Contains(got, `"j1"`) {
		t.Fatalf("转发结果不对：%s", got)
	}
}

type headerRT struct{ key, host string }

func (h headerRT) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Authorization", "Bearer "+h.key)
	r.Header.Set("X-IMLS-Host", h.host)
	return http.DefaultTransport.RoundTrip(r)
}
