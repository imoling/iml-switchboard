package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

// memStore 是测试用的内存任务存储。
type memStore struct {
	mu   sync.Mutex
	jobs map[string]*core.Job
}

func newStore() *memStore { return &memStore{jobs: map[string]*core.Job{}} }
func (s *memStore) Put(j *core.Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := *j
	s.jobs[j.ID] = &c
	return nil
}
func (s *memStore) Get(id string) (*core.Job, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, false
	}
	c := *j
	return &c, true
}
func (s *memStore) List(kind core.Kind) []*core.Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*core.Job
	for _, j := range s.jobs {
		if j.Kind == kind {
			c := *j
			out = append(out, &c)
		}
	}
	return out
}
func (s *memStore) Update(id string, fn func(*core.Job)) (*core.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, errors.New("not found")
	}
	fn(j)
	c := *j
	return &c, nil
}

// fakeAccounts 是测试用账号表。
type fakeAccounts struct{ list []api.Account }

func (f *fakeAccounts) Account(id string) (api.Account, bool) {
	for _, a := range f.list {
		if a.ID == id {
			return a, true
		}
	}
	return api.Account{}, false
}
func (f *fakeAccounts) AccountByProvider(p string) (api.Account, bool) {
	for _, a := range f.list {
		if a.Provider == p {
			return a, true
		}
	}
	return api.Account{}, false
}
func (f *fakeAccounts) Accounts() []api.Account { return f.list }

func acct(id, provider, base string) api.Account {
	return api.Account{ID: id, Provider: provider, BaseURL: base, APIKeyGetter: func() (string, error) { return "k", nil }}
}

func TestPrices(t *testing.T) {
	cases := []struct {
		model, quality string
		units          float64
		wantUSD        float64
		wantCur        string
	}{
		{"google/gemini-nano-banana-2.1", "", 1, 0.045, "USD"},
		{"gpt-image-2.5", "low", 1, 0.006, "USD"},
		{"gpt-image-2.5-high", "", 1, 0.17, "USD"},
		{"doubao-seedream-5-0-flash-260915", "", 1, 0.12 / api.USDPerCNY, "CNY"},
		{"doubao-seedream-5-0-pro-260628", "", 1, 0.30 / api.USDPerCNY, "CNY"},
		{"doubao-seedance-2-0-fast-260128", "", 5, 4.0 / api.USDPerCNY, "CNY"},
		{"doubao-seedance-2-0-260128", "", 5, 4.95 / api.USDPerCNY, "CNY"},
		{"doubao-seedance-2-5-pro", "", 5, 7.55 / api.USDPerCNY, "CNY"},
		{"google/veo-3.1-lite", "", 8, 0.40, "USD"},
		{"google/veo-3.1", "", 8, 3.2, "USD"},
		{"wan3.0", "", 5, 3.0 / api.USDPerCNY, "CNY"},
		{"codex-imagegen", "", 1, 0, "USD"},
	}
	for _, c := range cases {
		u := UsageFor(c.model, c.quality, c.units)
		if fmt.Sprintf("%.6f", u.CostUSD) != fmt.Sprintf("%.6f", c.wantUSD) || u.Currency != c.wantCur {
			t.Errorf("%s/%s ×%v → %+v，期望 %.6f %s", c.model, c.quality, c.units, u, c.wantUSD, c.wantCur)
		}
	}
	if _, ok := PriceFor("unknown-model", ""); ok {
		t.Errorf("未知模型不应有价")
	}
}

func TestDefaultOutDirAndSlug(t *testing.T) {
	// 用本系统真正的绝对路径：Windows 上 /x/out 没有盘符，不算绝对路径
	out, proj := filepath.Join(t.TempDir(), "out"), t.TempDir()
	if p, _ := DefaultOutDir(&core.Task{OutDir: out, ProjectDir: proj}); p != out {
		t.Errorf("OutDir 应优先：%s", p)
	}
	if p, _ := DefaultOutDir(&core.Task{OutDir: "assets", ProjectDir: proj}); p != filepath.Join(proj, "assets") {
		t.Errorf("相对 OutDir 应接在 ProjectDir 后：%s", p)
	}
	if p, _ := DefaultOutDir(&core.Task{ProjectDir: proj}); p != filepath.Join(proj, "assets") {
		t.Errorf("ProjectDir/assets：%s", p)
	}
	if p, _ := DefaultOutDir(&core.Task{}); !strings.HasSuffix(p, filepath.Join("Switchboard", "media")) {
		t.Errorf("默认目录不对：%s", p)
	}
	if s := Slug("一只 在月球上 跳舞的猫, with hat!"); s != "一只-在月球上-跳舞的猫-with-hat" {
		t.Errorf("slug 不对：%s", s)
	}
}

func TestImageOpenAICompat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"data":[{"b64_json":"iVBORw0KGgo="}]}`)
	}))
	defer srv.Close()
	out := t.TempDir()
	now := time.Date(2026, 10, 18, 12, 30, 45, 0, time.Local)
	d := New(Options{Jobs: newStore(), Accounts: &fakeAccounts{[]api.Account{acct("a1", "openai", srv.URL+"/v1")}}, Client: srv.Client(), Now: func() time.Time { return now }})
	var events []string
	res, err := d.Run(context.Background(), &core.Task{Kind: core.KindImage, Prompt: "一只猫", OutDir: out, MediaTier: "cheap"}, core.Target{Lane: core.LaneMedia, Provider: "openai", Model: "gpt-image-2.5"}, func(e core.Event) { events = append(events, e.Type) })
	if err != nil {
		t.Fatalf("出图失败：%v", err)
	}
	if filepath.Base(res.Path) != "20261018-123045-一只猫.png" {
		t.Errorf("文件名不对：%s", res.Path)
	}
	if b, _ := os.ReadFile(res.Path); string(b) != "\x89PNG\r\n\x1a\n" {
		t.Errorf("内容不对：%q", b)
	}
	if res.Usage.CostUSD != 0.006 || res.Usage.Unit != "image" || res.Usage.Units != 1 {
		t.Errorf("cheap 档应按 low 计价：%+v", res.Usage)
	}
	if strings.Join(events, ",") != "media,done" {
		t.Errorf("事件不对：%v", events)
	}
	// 没有账号
	d2 := New(Options{Jobs: newStore()})
	_, err = d2.Run(context.Background(), &core.Task{Kind: core.KindImage, Prompt: "x", OutDir: out}, core.Target{Provider: "openai", Model: "gpt-image-2.5"}, nil)
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || !strings.Contains(pe.Summary, "没有可用的 openai 账号") {
		t.Errorf("缺账号错误不对：%v", err)
	}
}

func TestImageArkURLAndOpenRouter(t *testing.T) {
	var arkBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/images/generations":
			json.NewDecoder(r.Body).Decode(&arkBody)
			fmt.Fprintf(w, `{"model":"doubao-seedream-5-0-flash-260915","data":[{"url":"http://%s/file.jpg"}]}`, r.Host)
		case "/file.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("\xff\xd8\xff"))
		case "/api/v1/images":
			if r.Header.Get("Authorization") != "Bearer k" {
				w.WriteHeader(401)
				return
			}
			fmt.Fprint(w, `{"data":[{"b64_json":"iVBORw0KGgo="}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	out := t.TempDir()
	accts := &fakeAccounts{[]api.Account{acct("ark1", "ark", srv.URL+"/api/v3"), acct("or1", "openrouter", srv.URL+"/api/v1")}}
	d := New(Options{Jobs: newStore(), Accounts: accts, Client: srv.Client()})
	res, err := d.Run(context.Background(), &core.Task{Kind: core.KindImage, Prompt: "海边", OutDir: out, Size: "2K"}, core.Target{Provider: "ark", Model: "doubao-seedream-5-0-flash-260915"}, nil)
	if err != nil {
		t.Fatalf("ark 出图失败：%v", err)
	}
	if !strings.HasSuffix(res.Path, ".jpg") || res.Usage.Currency != "CNY" || res.Usage.CostNative != 0.12 {
		t.Errorf("ark 结果不对：%+v", res)
	}
	if arkBody["response_format"] != "b64_json" || arkBody["size"] != "2K" || arkBody["watermark"] != false {
		t.Errorf("ark 请求体不对：%v", arkBody)
	}
	res, err = d.Run(context.Background(), &core.Task{Kind: core.KindImage, Prompt: "cat", OutDir: out}, core.Target{Provider: "openrouter", Model: "google/gemini-nano-banana-2.1"}, nil)
	if err != nil {
		t.Fatalf("openrouter 出图失败：%v", err)
	}
	if res.Usage.CostUSD != 0.045 || !strings.HasSuffix(res.Path, ".png") {
		t.Errorf("openrouter 结果不对：%+v", res)
	}
	// 401 → 验证失败：这是账号自己的问题，标记账号、换下一个候选
	accts.list[1].APIKeyGetter = func() (string, error) { return "bad", nil }
	_, err = d.Run(context.Background(), &core.Task{Kind: core.KindImage, Prompt: "cat", OutDir: out}, core.Target{Provider: "openrouter", Model: "x"}, nil)
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || pe.Summary != "openrouter 验证失败：密钥无效或已过期" || !pe.Account {
		t.Errorf("401 映射不对：%v", err)
	}
}

// fakeCodex 模拟 Codex 驱动出图。
type fakeCodex struct{ prompt string }

func (f *fakeCodex) ID() core.LaneID { return core.LaneCodexSub }
func (f *fakeCodex) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	f.prompt = task.Prompt
	p := filepath.Join(task.OutDir, "x.png")
	os.WriteFile(p, []byte("PNGDATA"), 0o644)
	return &core.Result{Path: p, Model: target.Model}, nil
}
func (f *fakeCodex) Health(ctx context.Context) core.Health { return core.Health{Status: "ok"} }

func TestImageCodexFree(t *testing.T) {
	fc := &fakeCodex{}
	out := t.TempDir()
	d := New(Options{Jobs: newStore(), Codex: fc})
	res, err := d.Run(context.Background(), &core.Task{Kind: core.KindImage, Prompt: "免费猫", OutDir: out, MediaTier: "free"}, core.Target{Provider: "codex", Model: "codex-imagegen"}, nil)
	if err != nil {
		t.Fatalf("codex 出图失败：%v", err)
	}
	if fc.prompt != "免费猫" || res.Usage.CostUSD != 0 || !strings.HasPrefix(res.Path, out) {
		t.Errorf("结果不对：%+v prompt=%q", res, fc.prompt)
	}
	if b, _ := os.ReadFile(res.Path); string(b) != "PNGDATA" {
		t.Errorf("产物内容不对")
	}
	if h := d.Health(context.Background()); h.Status != "ok" || !strings.Contains(h.Detail, "codex") {
		t.Errorf("Health 不对：%+v", h)
	}
}

// fakeVideo 是可编程的视频供应商。
type fakeVideo struct {
	name      string
	polls     int
	needPolls int
	failWith  string
	submitErr error
	canceled  bool
	url       string
}

func (f *fakeVideo) Name() string { return f.name }
func (f *fakeVideo) Submit(ctx context.Context, req VideoRequest) (string, error) {
	if f.submitErr != nil {
		return "", f.submitErr
	}
	return "ref-1", nil
}
func (f *fakeVideo) Poll(ctx context.Context, ref string) (VideoStatus, error) {
	f.polls++
	if f.polls < f.needPolls {
		return VideoStatus{Progress: float64(f.polls) / float64(f.needPolls)}, nil
	}
	if f.failWith != "" {
		return VideoStatus{Done: true, Error: f.failWith}, nil
	}
	return VideoStatus{Done: true, Progress: 1, URL: f.url}, nil
}
func (f *fakeVideo) Cancel(ctx context.Context, ref string) error { f.canceled = true; return nil }

func TestVideoJobFlow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("MP4DATA")) }))
	defer srv.Close()
	store := newStore()
	fv := &fakeVideo{name: "ark", needPolls: 2, url: srv.URL + "/v.mp4"}
	other := &fakeVideo{name: "openrouter"}
	out := t.TempDir()
	d := New(Options{Jobs: store, Client: srv.Client(), VideoProviders: map[string]VideoProvider{"ark": fv, "openrouter": other}, PollBase: 10 * time.Millisecond, PollMax: 20 * time.Millisecond})
	var jobEvents int
	res, err := d.Run(context.Background(), &core.Task{ID: "t1", Kind: core.KindVideo, Prompt: "海浪", Seconds: 5, OutDir: out}, core.Target{Provider: "ark", Model: "doubao-seedance-2-0-fast-260128"}, func(e core.Event) {
		if e.Type == "job" {
			jobEvents++
		}
	})
	if err != nil {
		t.Fatalf("视频失败：%v", err)
	}
	if res.Path == "" || !strings.HasSuffix(res.Path, ".mp4") || res.JobID != "job_t1" {
		t.Errorf("结果不对：%+v", res)
	}
	if b, _ := os.ReadFile(res.Path); string(b) != "MP4DATA" {
		t.Errorf("产物内容不对")
	}
	if res.Usage.Unit != "second" || res.Usage.Units != 5 || fmt.Sprintf("%.2f", res.Usage.CostNative) != "4.00" {
		t.Errorf("用量不对：%+v", res.Usage)
	}
	j, _ := store.Get("job_t1")
	if j.Status != "done" || j.Progress != 1 || j.ResultPath != res.Path || j.ProviderRef != "ref-1" || j.CostUSD == 0 {
		t.Errorf("任务记录不对：%+v", j)
	}
	if jobEvents < 2 {
		t.Errorf("应至少发出 submitted 与 done 两次 job 事件，得到 %d", jobEvents)
	}

	// ctx 先到期 → 返回 JobID，任务继续在后台完成
	slow := &fakeVideo{name: "ark", needPolls: 30, url: srv.URL + "/v.mp4"}
	d2 := New(Options{Jobs: store, Client: srv.Client(), VideoProviders: map[string]VideoProvider{"ark": slow}, PollBase: 5 * time.Millisecond, PollMax: 5 * time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	res, err = d2.Run(ctx, &core.Task{ID: "t2", Kind: core.KindVideo, Prompt: "x", OutDir: out}, core.Target{Provider: "ark", Model: "m"}, nil)
	if err != nil || res.JobID != "job_t2" || res.Path != "" {
		t.Fatalf("ctx 到期应返回 JobID：%+v %v", res, err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		j, _ := store.Get("job_t2")
		if j.Status == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("后台任务没有完成：%+v", j)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 审核拦截 → 不可重试、无重试选项
	mod := &fakeVideo{name: "ark", needPolls: 1, failWith: "Request was rejected: sensitive content"}
	d3 := New(Options{Jobs: store, VideoProviders: map[string]VideoProvider{"ark": mod, "openrouter": other}, PollBase: time.Millisecond})
	_, err = d3.Run(context.Background(), &core.Task{ID: "t3", Kind: core.KindVideo, Prompt: "x", OutDir: out}, core.Target{Provider: "ark", Model: "m"}, nil)
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || pe.Retryable || !strings.Contains(pe.Summary, "审核") {
		t.Errorf("审核拦截应不可重试：%v", err)
	}
	j, _ = store.Get("job_t3")
	if j.Status != "failed" || len(j.RetryOptions) != 0 {
		t.Errorf("审核失败不应给重试选项：%+v", j)
	}

	// 普通失败 → 可重试，RetryOptions 列出其它供应商
	bad := &fakeVideo{name: "ark", submitErr: &core.ErrProvider{Summary: "ark 余额不足：请充值后重试", Retryable: true}}
	d4 := New(Options{Jobs: store, VideoProviders: map[string]VideoProvider{"ark": bad, "openrouter": other}, PollBase: time.Millisecond})
	_, err = d4.Run(context.Background(), &core.Task{ID: "t4", Kind: core.KindVideo, Prompt: "x", OutDir: out}, core.Target{Provider: "ark", Model: "m"}, nil)
	if !errors.As(err, &pe) || !pe.Retryable {
		t.Errorf("余额不足应可重试：%v", err)
	}
	j, _ = store.Get("job_t4")
	if len(j.RetryOptions) != 1 || j.RetryOptions[0].Provider != "openrouter" {
		t.Errorf("应给出 openrouter 作为重试选项：%+v", j.RetryOptions)
	}

	// 超时 → status timeout 并取消
	stuck := &fakeVideo{name: "ark", needPolls: 1000}
	d5 := New(Options{Jobs: store, VideoProviders: map[string]VideoProvider{"ark": stuck}, PollBase: time.Millisecond, PollMax: time.Millisecond, JobTimeout: 50 * time.Millisecond})
	_, err = d5.Run(context.Background(), &core.Task{ID: "t5", Kind: core.KindVideo, Prompt: "x", OutDir: out}, core.Target{Provider: "ark", Model: "m"}, nil)
	if !errors.As(err, &pe) || !strings.Contains(pe.Summary, "超时") {
		t.Errorf("应报超时：%v", err)
	}
	j, _ = store.Get("job_t5")
	if j.Status != "timeout" || !stuck.canceled {
		t.Errorf("超时应标记并取消：%+v canceled=%v", j, stuck.canceled)
	}
}

func TestVideoProvidersHTTP(t *testing.T) {
	var arkSubmit map[string]interface{}
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v3/contents/generations/tasks" && r.Method == "POST":
			json.NewDecoder(r.Body).Decode(&arkSubmit)
			fmt.Fprint(w, `{"id":"cgt-1"}`)
		case r.URL.Path == "/api/v3/contents/generations/tasks/cgt-1" && r.Method == "GET":
			polls++
			if polls == 1 {
				fmt.Fprint(w, `{"id":"cgt-1","status":"running"}`)
			} else {
				fmt.Fprint(w, `{"id":"cgt-1","status":"succeeded","content":{"video_url":"http://x/v.mp4"}}`)
			}
		case r.URL.Path == "/api/v3/contents/generations/tasks/cgt-1" && r.Method == "DELETE":
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/api/v1/videos" && r.Method == "POST":
			fmt.Fprint(w, `{"id":"or-1","status":"queued"}`)
		case r.URL.Path == "/api/v1/videos/or-1":
			fmt.Fprint(w, `{"id":"or-1","status":"failed","error":{"message":"content moderation blocked"}}`)
		case r.URL.Path == "/v1/videos" && r.Method == "POST":
			fmt.Fprint(w, `{"id":"di-1","status":"queued"}`)
		case r.URL.Path == "/v1/videos/di-1":
			fmt.Fprint(w, `{"id":"di-1","status":"completed","progress":100}`)
		case r.URL.Path == "/v1/videos/di-1/content":
			if r.Header.Get("Authorization") != "Bearer k" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte("VIDEO"))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	ark := &ArkVideo{Account: acct("a", "ark", srv.URL+"/api/v3"), Client: srv.Client()}
	ref, err := ark.Submit(ctx, VideoRequest{Prompt: "p", Model: "m", Seconds: 5, Size: "16:9"})
	if err != nil || ref != "cgt-1" {
		t.Fatalf("ark submit：%v %s", err, ref)
	}
	if arkSubmit["duration"] != float64(5) || arkSubmit["ratio"] != "16:9" {
		t.Errorf("ark 请求体不对：%v", arkSubmit)
	}
	st, _ := ark.Poll(ctx, ref)
	if st.Done {
		t.Errorf("running 不应 done")
	}
	st, _ = ark.Poll(ctx, ref)
	if !st.Done || st.URL != "http://x/v.mp4" {
		t.Errorf("ark 完成状态不对：%+v", st)
	}
	if err := ark.Cancel(ctx, ref); err != nil {
		t.Errorf("ark cancel：%v", err)
	}

	or := &OpenRouterVideo{Account: acct("o", "openrouter", srv.URL+"/api/v1"), Client: srv.Client()}
	ref, err = or.Submit(ctx, VideoRequest{Prompt: "p", Model: "m"})
	if err != nil || ref != "or-1" {
		t.Fatalf("openrouter submit：%v", err)
	}
	st, _ = or.Poll(ctx, ref)
	if !st.Done || !strings.Contains(st.Error, "moderation") {
		t.Errorf("openrouter 失败状态不对：%+v", st)
	}

	di := &OpenAICompatVideo{Account: acct("d", "deepinfra", srv.URL+"/v1"), Client: srv.Client()}
	ref, err = di.Submit(ctx, VideoRequest{Prompt: "p", Model: "m", Seconds: 4})
	if err != nil || ref != "di-1" {
		t.Fatalf("deepinfra submit：%v", err)
	}
	st, _ = di.Poll(ctx, ref)
	if !st.Done || st.URL != srv.URL+"/v1/videos/di-1/content" {
		t.Errorf("deepinfra 完成状态应指向 /content：%+v", st)
	}
	dst := filepath.Join(t.TempDir(), "v.mp4")
	if err := di.Download(ctx, st.URL, dst); err != nil {
		t.Fatalf("带认证下载失败：%v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "VIDEO" {
		t.Errorf("下载内容不对")
	}
}

// fakeClaude 模拟 Claude 驱动改代码。
type fakeClaude struct {
	prompts []string
	dirs    []string
}

func (f *fakeClaude) ID() core.LaneID { return core.LaneClaudeSub }
func (f *fakeClaude) Run(ctx context.Context, task *core.Task, target core.Target, emit func(core.Event)) (*core.Result, error) {
	f.prompts = append(f.prompts, task.Prompt)
	f.dirs = append(f.dirs, task.ProjectDir)
	if task.Kind != core.KindAnimation || task.ProjectDir == "" {
		return nil, errors.New("应以 animation + ProjectDir 调用")
	}
	os.WriteFile(filepath.Join(task.ProjectDir, "src", "Main.tsx"), []byte("// edited "+fmt.Sprint(len(f.prompts))), 0o644)
	return &core.Result{Text: "已修改"}, nil
}
func (f *fakeClaude) Health(ctx context.Context) core.Health { return core.Health{Status: "ok"} }

func TestAnimationRemotion(t *testing.T) {
	root := t.TempDir()
	out := t.TempDir()
	fc := &fakeClaude{}
	var cmds []string
	renders := 0
	failFirst := true
	runner := func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error) {
		cmds = append(cmds, name+" "+strings.Join(args, " "))
		switch name {
		case "npm":
			for _, kv := range env {
				if strings.HasPrefix(kv, "HTTPS_PROXY=") {
					t.Errorf("npm install 前应去掉代理：%s", kv)
				}
			}
			os.MkdirAll(filepath.Join(dir, "node_modules", "remotion"), 0o755)
			return "", "", nil
		case "npx":
			renders++
			if failFirst && renders == 1 {
				return "", "Error: Main.tsx: unexpected token", errors.New("exit 1")
			}
			os.WriteFile(filepath.Join(dir, "out.mp4"), []byte("MP4"), 0o644)
			return "", "", nil
		}
		return "", "", errors.New("unknown cmd")
	}
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	store := newStore()
	d := New(Options{Jobs: store, Claude: fc, AnimRoot: root, Exec: runner})
	d.remotion.LookPath = func(string) (string, error) { return "/usr/bin/x", nil }
	res, err := d.Run(context.Background(), &core.Task{ID: "an1", Kind: core.KindAnimation, Prompt: "一个弹跳的球", Seconds: 6, OutDir: out}, core.Target{Provider: "remotion", Model: "sonnet"}, nil)
	if err != nil {
		t.Fatalf("动画失败：%v", err)
	}
	if !strings.HasSuffix(res.Path, ".mp4") || !strings.HasPrefix(res.Path, out) {
		t.Errorf("产物路径不对：%s", res.Path)
	}
	j, _ := store.Get("job_an1")
	if j.Status != "done" || j.CodeDir != filepath.Join(root, "job_an1") {
		t.Errorf("任务记录不对：%+v", j)
	}
	// 模板已复制
	for _, f := range []string{"package.json", "remotion.config.ts", "src/index.ts", "src/Root.tsx", "src/Main.tsx"} {
		if _, err := os.Stat(filepath.Join(j.CodeDir, f)); err != nil {
			t.Errorf("模板文件缺失：%s", f)
		}
	}
	// 第一次渲染失败 → 回喂修复 → 第二次成功
	if len(fc.prompts) != 2 || !strings.Contains(fc.prompts[0], "6 秒") || !strings.Contains(fc.prompts[0], "一个弹跳的球") || !strings.Contains(fc.prompts[1], "unexpected token") {
		t.Errorf("Claude 调用不对：%q", fc.prompts)
	}
	if renders != 2 || len(cmds) != 3 || !strings.HasPrefix(cmds[0], "npm install") || cmds[1] != "npx remotion render Main out.mp4" {
		t.Errorf("命令顺序不对：%v", cmds)
	}

	// revise：只跑 Claude + 渲染，不再 npm install
	failFirst = false
	cmds = nil
	res, err = d.ReviseAnimation(context.Background(), "job_an1", "把球改成红色", nil)
	if err != nil {
		t.Fatalf("revise 失败：%v", err)
	}
	if len(fc.prompts) != 3 || !strings.Contains(fc.prompts[2], "把球改成红色") || fc.dirs[2] != j.CodeDir {
		t.Errorf("revise 调用不对：%q %q", fc.prompts, fc.dirs)
	}
	if len(cmds) != 1 || !strings.HasPrefix(cmds[0], "npx remotion render") {
		t.Errorf("revise 不应再 npm install：%v", cmds)
	}

	// 两轮修复都失败
	failAll := func(ctx context.Context, dir string, env []string, name string, args ...string) (string, string, error) {
		if name == "npm" {
			os.MkdirAll(filepath.Join(dir, "node_modules", "remotion"), 0o755)
			return "", "", nil
		}
		return "", "boom", errors.New("exit 1")
	}
	d2 := New(Options{Jobs: store, Claude: &fakeClaude{}, AnimRoot: root, Exec: failAll})
	d2.remotion.LookPath = func(string) (string, error) { return "/x", nil }
	_, err = d2.Run(context.Background(), &core.Task{ID: "an2", Kind: core.KindAnimation, Prompt: "x", OutDir: out}, core.Target{Provider: "remotion"}, nil)
	var pe *core.ErrProvider
	if !errors.As(err, &pe) || !strings.Contains(pe.Summary, "自动修复两轮仍未通过") {
		t.Errorf("应报渲染失败：%v", err)
	}

	// 没有 Node
	d3 := New(Options{Jobs: store, Claude: fc, AnimRoot: root})
	d3.remotion.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	_, err = d3.Run(context.Background(), &core.Task{ID: "an3", Kind: core.KindAnimation, Prompt: "x", OutDir: out}, core.Target{Provider: "remotion"}, nil)
	if !errors.As(err, &pe) || pe.Retryable || !strings.HasPrefix(pe.Summary, "动画需要本机安装 Node") {
		t.Errorf("缺 Node 应不可重试：%v", err)
	}
}

func TestCancelRetryAndLostTask(t *testing.T) {
	store := newStore()
	out := t.TempDir()
	stuck := &fakeVideo{name: "ark", needPolls: 1000}
	other := &fakeVideo{name: "openrouter", needPolls: 1, url: "http://127.0.0.1:1/none"}
	d := New(Options{Jobs: store, VideoProviders: map[string]VideoProvider{"ark": stuck, "openrouter": other}, PollBase: time.Millisecond, PollMax: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	res, err := d.Run(ctx, &core.Task{ID: "c1", Kind: core.KindVideo, Prompt: "x", OutDir: out}, core.Target{Provider: "ark", Model: "m"}, nil)
	if err != nil || res.JobID == "" {
		t.Fatalf("应返回 JobID：%+v %v", res, err)
	}
	j, err := d.Cancel(context.Background(), res.JobID)
	if err != nil || j.Status != "canceled" || !stuck.canceled {
		t.Fatalf("取消失败：%+v %v canceled=%v", j, err, stuck.canceled)
	}
	time.Sleep(20 * time.Millisecond)
	if j, _ = store.Get(res.JobID); j.Status != "canceled" {
		t.Errorf("取消后状态被后台覆盖：%+v", j)
	}
	if _, err := d.Cancel(context.Background(), res.JobID); err == nil {
		t.Errorf("重复取消应报错")
	}
	// Retry：没指定模型时取 RetryOptions[0]
	store.Update(res.JobID, func(j *core.Job) {
		j.RetryOptions = []core.Target{{Lane: core.LaneMedia, Provider: "openrouter", Model: "google/veo-3.1-lite"}}
	})
	nj, err := d.Retry(context.Background(), res.JobID, "")
	if err != nil {
		t.Fatalf("Retry 失败：%v", err)
	}
	if nj.ID == res.JobID || nj.Provider != "openrouter" || nj.Model != "google/veo-3.1-lite" || nj.Task == nil {
		t.Errorf("新任务不对：%+v", nj)
	}
	// Retry 指定模型
	nj2, err := d.Retry(context.Background(), res.JobID, "doubao-seedance-2-5")
	if err != nil || nj2.Model != "doubao-seedance-2-5" || nj2.Provider != "ark" {
		t.Errorf("指定模型重试不对：%+v %v", nj2, err)
	}
	// Task 丢失（模拟重启后从账本恢复）
	store.Update(res.JobID, func(j *core.Job) { j.Task = nil })
	if _, err := d.Retry(context.Background(), res.JobID, ""); err == nil || !strings.Contains(err.Error(), "已丢失") {
		t.Errorf("Task 丢失应报一行人话：%v", err)
	}
	store.Put(&core.Job{ID: "lost", Kind: core.KindAnimation, Status: "done", CodeDir: "/nonexistent"})
	if _, err := d.ReviseAnimation(context.Background(), "lost", "改", nil); err == nil || !strings.Contains(err.Error(), "已丢失") {
		t.Errorf("动画 Task 丢失应报一行人话：%v", err)
	}
	if _, err := d.Retry(context.Background(), "nope", ""); err == nil {
		t.Errorf("不存在的任务应报错")
	}
}
