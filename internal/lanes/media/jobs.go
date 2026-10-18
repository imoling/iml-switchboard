package media

import (
	"context"
	"strings"
	"time"

	"github.com/imoling/iml-switchboard/internal/core"
)

// waiter 为任务建一个完成信号。
func (d *MediaDriver) waiter(jobID string) chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	ch := make(chan struct{})
	d.waiters[jobID] = ch
	return ch
}

func (d *MediaDriver) finish(jobID string, done chan struct{}) {
	d.mu.Lock()
	delete(d.waiters, jobID)
	delete(d.active, jobID)
	d.mu.Unlock()
	close(done)
}

// update 更新任务并顺带发一个 job 事件。
func (d *MediaDriver) update(jobID string, emit func(core.Event), fn func(*core.Job)) *core.Job {
	now := d.opt.Now()
	j, err := d.opt.Jobs.Update(jobID, func(j *core.Job) {
		// 用户取消后，后台轮询还可能带着取消前拿到的进度回来写「运行中」：取消必须作数，丢掉这次写入
		if j.Status == "canceled" {
			return
		}
		fn(j)
		j.UpdatedAt = now
	})
	if err != nil || j == nil {
		return nil
	}
	if emit != nil {
		emit(core.Event{Type: "job", Job: j})
	}
	return j
}

// waitJob 等任务完成或 ctx 到期：完成返回 Path，否则返回 JobID 让调用方用 get_job 查询。
func (d *MediaDriver) waitJob(ctx context.Context, jobID string, done chan struct{}, emit func(core.Event)) (*core.Result, error) {
	select {
	case <-done:
	case <-ctx.Done():
		j, _ := d.opt.Jobs.Get(jobID)
		res := &core.Result{JobID: jobID}
		if j != nil {
			res.Model = j.Model
		}
		return res, nil
	}
	j, ok := d.opt.Jobs.Get(jobID)
	if !ok {
		return nil, &core.ErrProvider{Summary: "任务记录丢失：" + jobID}
	}
	switch j.Status {
	case "done":
		res := &core.Result{Path: j.ResultPath, JobID: j.ID, Model: j.Model}
		if j.Task != nil {
			switch j.Kind {
			case core.KindVideo:
				res.Usage = UsageFor(j.Model, "", float64(secondsOf(j.Task)))
			default:
				res.Usage = core.Usage{Unit: "second", Units: float64(secondsOf(j.Task))}
			}
		}
		if emit != nil {
			emit(core.Event{Type: "media", Path: j.ResultPath})
			emit(core.Event{Type: "done"})
		}
		return res, nil
	case "timeout":
		return nil, &core.ErrProvider{Summary: "任务超时：10 分钟内没有完成，已尝试取消", Raw: j.Error, Retryable: true}
	}
	retryable := len(j.RetryOptions) > 0 || j.Status == "timeout"
	if j.Error == "" {
		j.Error = "任务失败"
	}
	return nil, &core.ErrProvider{Summary: j.Error, Retryable: retryable || !isModerationMessage(j.Error)}
}

func isModerationMessage(s string) bool {
	for _, m := range []string{"审核", "moderation", "sensitive", "违规"} {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

func secondsOf(task *core.Task) int {
	if task.Seconds > 0 {
		return task.Seconds
	}
	if task.Kind == core.KindAnimation {
		return 8
	}
	return 5
}

// runVideoJob 在后台跑完整个视频任务：提交 → 轮询 → 下载。
func (d *MediaDriver) runVideoJob(job *core.Job, vp VideoProvider, task *core.Task, target core.Target, outPath string, emit func(core.Event), done chan struct{}) {
	defer d.finish(job.ID, done)
	ctx, cancel := context.WithTimeout(context.Background(), d.opt.JobTimeout)
	defer cancel()
	d.mu.Lock()
	d.active[job.ID] = &activeJob{cancel: cancel, video: vp}
	d.mu.Unlock()

	fail := func(err error) {
		if ctx.Err() == context.Canceled {
			return // 已被 Cancel 处理
		}
		msg, retryable := classifyJobError(err)
		var opts []core.Target
		if retryable {
			opts = d.videoRetryOptions(target)
		}
		d.update(job.ID, emit, func(j *core.Job) {
			j.Status = "failed"
			j.Error = msg
			j.RetryOptions = opts
		})
	}

	ref, err := vp.Submit(ctx, VideoRequest{Prompt: task.Prompt, Model: target.Model, Seconds: secondsOf(task), ImagePath: task.ImagePath, Size: task.Size})
	if err != nil {
		fail(err)
		return
	}
	d.mu.Lock()
	if a := d.active[job.ID]; a != nil {
		a.ref = ref
	}
	d.mu.Unlock()
	d.update(job.ID, emit, func(j *core.Job) {
		j.Status = "submitted"
		j.ProviderRef = ref
		j.Progress = 0.05
	})

	interval := d.opt.PollBase
	for {
		select {
		case <-ctx.Done():
			if ctx.Err() == context.Canceled {
				return // 用户取消，状态已由 Cancel 写好
			}
			// 10 分钟没完成：标记超时并尽力取消
			cctx, ccancel := context.WithTimeout(context.Background(), 15*time.Second)
			_ = vp.Cancel(cctx, ref)
			ccancel()
			d.update(job.ID, emit, func(j *core.Job) {
				j.Status = "timeout"
				j.Error = "供应商 10 分钟内没有完成，已尝试取消"
				j.RetryOptions = d.videoRetryOptions(target)
			})
			return
		case <-time.After(interval):
		}
		st, err := vp.Poll(ctx, ref)
		if err != nil {
			if ctx.Err() != nil {
				continue // 交给上面的 ctx.Done 分支处理
			}
			fail(err)
			return
		}
		if !st.Done {
			d.update(job.ID, emit, func(j *core.Job) {
				j.Status = "running"
				if st.Progress > j.Progress {
					j.Progress = st.Progress
				}
				j.ETASeconds = int(interval.Seconds()) * 2
			})
			interval *= 2
			if interval > d.opt.PollMax {
				interval = d.opt.PollMax
			}
			continue
		}
		if st.Error != "" {
			fail(&core.ErrProvider{Summary: vp.Name() + " 生成失败：" + firstLine(st.Error), Raw: st.Error, Retryable: true})
			return
		}
		if st.URL == "" {
			fail(&core.ErrProvider{Summary: vp.Name() + " 生成完成但没有给出视频地址", Retryable: true})
			return
		}
		var dlErr error
		if dl, ok := vp.(Downloader); ok {
			dlErr = dl.Download(ctx, st.URL, outPath)
		} else {
			dlErr = download(ctx, d.opt.Client, vp.Name(), st.URL, outPath, nil)
		}
		if dlErr != nil {
			fail(dlErr)
			return
		}
		usage := UsageFor(target.Model, "", float64(secondsOf(task)))
		d.update(job.ID, emit, func(j *core.Job) {
			j.Status = "done"
			j.Progress = 1
			j.ResultPath = outPath
			j.CostUSD = usage.CostUSD
			j.RetryOptions = nil
		})
		return
	}
}

// videoRetryOptions 列出除当前供应商外其它可用的视频供应商，作为失败时的重试建议。
func (d *MediaDriver) videoRetryOptions(cur core.Target) []core.Target {
	var out []core.Target
	seen := map[string]bool{cur.Provider: true}
	for name := range d.opt.VideoProviders {
		if !seen[name] {
			seen[name] = true
			out = append(out, core.Target{Lane: core.LaneMedia, Provider: name})
		}
	}
	if d.opt.Accounts != nil {
		for _, a := range d.opt.Accounts.Accounts() {
			if seen[a.Provider] {
				continue
			}
			if _, err := a.APIKey(); err != nil {
				continue
			}
			if m := defaultVideoModel(a.Provider); m != "" {
				seen[a.Provider] = true
				out = append(out, core.Target{Lane: core.LaneMedia, Provider: a.Provider, AccountID: a.ID, Model: m})
			}
		}
	}
	return out
}

// defaultVideoModel 给出各供应商默认的视频模型；不做视频的供应商返回空串。
func defaultVideoModel(provider string) string {
	switch provider {
	case "ark", "volcengine", "doubao":
		return "doubao-seedance-2-0-fast-260128"
	case "openrouter":
		return "google/veo-3.1-lite"
	case "deepinfra":
		return "wan3.0"
	}
	return ""
}
