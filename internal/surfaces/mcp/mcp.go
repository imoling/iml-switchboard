// Package mcp 是被宿主拉起的 stdio 小程序：把五个工具转发给后台的 /api/internal/tool。
// 后台没起时自动拉起 imls serve。
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/imoling/iml-switchboard/internal/config"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Serve 在 stdio 上跑 MCP server。
func Serve(ctx context.Context, baseURL, version string) error {
	// Switchboard 拉起的 claude、codex 子进程里不再提供工具，否则子任务可能又派回来，一层套一层
	if os.Getenv("IMLS_INNER") != "" {
		return errors.New("在 Switchboard 派出的子任务里，不再提供调度工具")
	}
	if err := ensureBackend(baseURL); err != nil {
		return err
	}
	cwd, _ := os.Getwd()
	host := os.Getenv("IMLS_HOST")
	if host == "" {
		// Claude Code 会给子进程设 CLAUDECODE=1；Codex 设 CODEX_SANDBOX 之类；都没有就按通用 MCP 记
		switch {
		case os.Getenv("CLAUDECODE") != "" || os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "":
			host = "claude_code"
		case os.Getenv("CODEX_SANDBOX") != "" || os.Getenv("CODEX_HOME") != "":
			host = "codex"
		default:
			host = "mcp"
		}
	}
	return NewServer(baseURL, version, host, cwd).Run(ctx, &sdk.StdioTransport{})
}

// HTTPHandler 把同一套工具挂在后台的 /mcp 上，给只会连 HTTP 的宿主（豆包工作等）用。
// 要求 Authorization: Bearer <门面密钥>；宿主名取 X-IMLS-Host 头，默认 http。
func HTTPHandler(baseURL, version string, key func() string) http.Handler {
	h := sdk.NewStreamableHTTPHandler(func(r *http.Request) *sdk.Server {
		host := r.Header.Get("X-IMLS-Host")
		if host == "" {
			host = "http"
		}
		return NewServer(baseURL, version, host, "")
	}, &sdk.StreamableHTTPOptions{Stateless: true, JSONResponse: true})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, ok := config.CheckHostKey(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), key())
		if !ok {
			http.Error(w, "密钥不对：到设置里复制门面密钥", http.StatusUnauthorized)
			return
		}
		if host != "" && r.Header.Get("X-IMLS-Host") == "" {
			r.Header.Set("X-IMLS-Host", host)
		}
		h.ServeHTTP(w, r)
	})
}

// Instructions 随工具一起交给宿主，宿主会放进系统提示：比宿主 CLAUDE.md 里那段更新得及时，每次升级都跟着换。
const Instructions = `需要一张成品图时（插画、头像、封面、海报、像素画、照片风格的图），先调 generate_image，不要用 SVG、HTML、Canvas 或 Python 脚本自己画。
用户给了参考图（照着这张头像画、按这张的风格、把这张改成…），把图片的本地路径放进 image_paths 一起传。
只有用户明确要 SVG 或矢量源文件、要数据图表、流程图或界面里的小图标时，才自己画。
generate_image 报「没有可用的模型」说明还没配出图账号：这时再自己画，并告诉用户。
需要视频用 generate_video；标题动效、转场用 make_animation。不需要读写本地文件的杂活（分类、摘要、改写、翻译、抽取）用 ask_model。工具返回的路径和费用原样告诉用户。`

// NewServer 组装五个工具；每次调用都转发给后台的 /api/internal/tool。
func NewServer(baseURL, version, host, cwd string) *sdk.Server {
	srv := sdk.NewServer(&sdk.Implementation{Name: "iml-switchboard", Version: version}, &sdk.ServerOptions{Instructions: Instructions})
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
		Prompt     string   `json:"prompt" jsonschema:"要画什么，越具体越好；有参考图时写清要保留什么、改成什么"`
		ImagePaths []string `json:"image_paths,omitempty" jsonschema:"参考图，可以多张：用户贴的图或项目里的图片的本地路径（相对路径按项目目录算），也可以是 http 地址"`
		Model      string   `json:"model,omitempty" jsonschema:"点名用哪个出图型号（工具说明里列了能用的）；不填按档位选"`
		Tier       string   `json:"tier,omitempty" jsonschema:"档位：cheap 便宜 / default 默认 / best 最好 / free 用 ChatGPT 订阅出图"`
		Size       string   `json:"size,omitempty" jsonschema:"尺寸，如 1024x1024、1536x1024"`
		OutDir     string   `json:"out_dir,omitempty" jsonschema:"保存目录，默认项目下的 assets"`
	}
	type videoArgs struct {
		Prompt    string `json:"prompt" jsonschema:"视频内容描述"`
		Seconds   int    `json:"seconds,omitempty" jsonschema:"时长秒数，默认 5"`
		Tier      string `json:"tier,omitempty" jsonschema:"档位：cheap / default / best"`
		ImagePath string `json:"image_path,omitempty" jsonschema:"图生视频时的首帧图片路径"`
		Model     string `json:"model,omitempty" jsonschema:"点名用哪个出视频型号（工具说明里列了能用的）；不填按档位选"`
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
	sdk.AddTool(srv, &sdk.Tool{Name: "generate_image", Description: "生成图片并保存到项目里。需要插画、头像、封面、海报、像素画这类成品图时先用它，不要自己写 SVG、HTML 或脚本画图冒充；用户给了参考图（照着这张画、按这个风格）就把路径放进 image_paths。" + modelHint(baseURL, "image")},
		func(ctx context.Context, r *sdk.CallToolRequest, a imageArgs) (*sdk.CallToolResult, any, error) {
			return call("generate_image", a)
		})
	sdk.AddTool(srv, &sdk.Tool{Name: "generate_video", Description: "生成短视频。90 秒内没完成会返回任务号，之后用 get_job 查询。" + modelHint(baseURL, "video")},
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
	return srv
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
	cmd := exec.Command(self, "serve", "--background")
	cmd.Stdout, cmd.Stderr = nil, nil // 后台自己写日志文件；挂着宿主的输出会让宿主关不掉
	cmd.Env = os.Environ()
	detach(cmd)
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

// modelHint 是工具说明里「能点名的型号」那一句：向后台要一次，要不到就不写（按档位选照样能用）。
func modelHint(baseURL, kind string) string {
	req, _ := http.NewRequest("GET", baseURL+"/api/media/models?kind="+kind, nil)
	req.Header.Set("X-IMLS-Client", "mcp")
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var list []struct {
		Model   string `json:"model"`
		Display string `json:"display"`
		Skipped string `json:"skipped"`
	}
	if json.NewDecoder(resp.Body).Decode(&list) != nil {
		return ""
	}
	var names []string
	for _, m := range list {
		if m.Skipped != "" {
			continue
		}
		n := m.Model
		if m.Display != "" && m.Display != m.Model {
			n += "（" + m.Display + "）"
		}
		names = append(names, n)
	}
	if len(names) < 2 {
		return ""
	}
	return "用户点名型号时填 model，能用的有：" + strings.Join(names, "、") + "。"
}
