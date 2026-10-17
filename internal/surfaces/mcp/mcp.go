// Package mcp 是被宿主拉起的 stdio 小程序：把五个工具转发给后台的 /api/internal/tool。
// 后台没起时自动拉起 imls serve。
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Serve 在 stdio 上跑 MCP server。
func Serve(ctx context.Context, baseURL, version string) error {
	if err := ensureBackend(baseURL); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	host := os.Getenv("IMLS_HOST")
	if host == "" {
		host = "claude_code"
	}
	srv := sdk.NewServer(&sdk.Implementation{Name: "iml-switchboard", Version: version}, nil)
	call := func(name string, args any) (*sdk.CallToolResult, any, error) {
		raw, _ := json.Marshal(args)
		body, _ := json.Marshal(map[string]any{"name": name, "args": json.RawMessage(raw), "project_dir": cwd, "host": host})
		req, _ := http.NewRequest("POST", baseURL+"/api/internal/tool", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-IMLS-Client", "mcp")
		resp, err := (&http.Client{Timeout: 16 * time.Minute}).Do(req)
		if err != nil {
			return nil, nil, errors.New("后台未响应：" + err.Error())
		}
		defer resp.Body.Close()
		var out struct {
			Text    string `json:"text"`
			IsError bool   `json:"is_error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: out.Text}}, IsError: out.IsError}, nil, nil
	}

	type imageArgs struct {
		Prompt string `json:"prompt" jsonschema:"要画什么，越具体越好"`
		Tier   string `json:"tier,omitempty" jsonschema:"档位：cheap 便宜 / default 默认 / best 最好 / free 用 ChatGPT 订阅出图"`
		Size   string `json:"size,omitempty" jsonschema:"尺寸，如 1024x1024、1536x1024"`
		OutDir string `json:"out_dir,omitempty" jsonschema:"保存目录，默认项目下的 assets"`
	}
	type videoArgs struct {
		Prompt    string `json:"prompt" jsonschema:"视频内容描述"`
		Seconds   int    `json:"seconds,omitempty" jsonschema:"时长秒数，默认 5"`
		Tier      string `json:"tier,omitempty" jsonschema:"档位：cheap / default / best"`
		ImagePath string `json:"image_path,omitempty" jsonschema:"图生视频时的首帧图片路径"`
	}
	type animArgs struct {
		Spec    string `json:"spec" jsonschema:"动画要表现什么：画面、文字、节奏"`
		Seconds int    `json:"seconds,omitempty" jsonschema:"时长秒数，默认 8"`
		Engine  string `json:"engine,omitempty" jsonschema:"渲染引擎，目前只有 remotion"`
	}
	type askArgs struct {
		Prompt string `json:"prompt" jsonschema:"要问的内容，把需要的上下文一起放进来"`
		Tier   string `json:"tier,omitempty" jsonschema:"档位：low 杂活 / mid 第二意见 / high 难题 / top 顶配"`
	}
	type jobArgs struct {
		JobID string `json:"job_id" jsonschema:"任务号"`
	}
	sdk.AddTool(srv, &sdk.Tool{Name: "generate_image", Description: "生成图片并保存到项目里。需要封面、配图、插图、海报时用它，不要自己写 SVG 冒充插图。"},
		func(ctx context.Context, r *sdk.CallToolRequest, a imageArgs) (*sdk.CallToolResult, any, error) {
			return call("generate_image", a)
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "generate_video", Description: "生成短视频。90 秒内没完成会返回任务号，之后用 get_job 查询。"},
		func(ctx context.Context, r *sdk.CallToolRequest, a videoArgs) (*sdk.CallToolResult, any, error) {
			return call("generate_video", a)
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "make_animation", Description: "按描述做动效视频：由强模型写 Remotion 代码并在本机渲染成 mp4。需要标题动效、转场、数据动画时用它。"},
		func(ctx context.Context, r *sdk.CallToolRequest, a animArgs) (*sdk.CallToolResult, any, error) {
			return call("make_animation", a)
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "ask_model", Description: "把不需要读写本地文件的杂活交给更便宜的模型：分类、摘要、改写、翻译、抽取；tier=mid 可以要第二意见。"},
		func(ctx context.Context, r *sdk.CallToolRequest, a askArgs) (*sdk.CallToolResult, any, error) {
			return call("ask_model", a)
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "get_job", Description: "查询视频或动画任务的进度和结果路径。"},
		func(ctx context.Context, r *sdk.CallToolRequest, a jobArgs) (*sdk.CallToolResult, any, error) {
			return call("get_job", a)
		})
	return srv.Run(ctx, &sdk.StdioTransport{})
}

// ensureBackend 探测后台；没起就用同一个可执行文件启动 serve 并等它就绪。
func ensureBackend(baseURL string) error {
	ping := func() bool {
		req, _ := http.NewRequest("GET", baseURL+"/api/status", nil)
		req.Header.Set("X-IMLS-Client", "mcp")
		resp, err := (&http.Client{Timeout: 1500 * time.Millisecond}).Do(req)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == 200
	}
	if ping() {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "serve")
	cmd.Stdout, cmd.Stderr = nil, os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("后台启动失败：%w", err)
	}
	_ = cmd.Process.Release()
	for i := 0; i < 40; i++ {
		time.Sleep(250 * time.Millisecond)
		if ping() {
			return nil
		}
	}
	return errors.New("后台 10 秒内没有就绪")
}
