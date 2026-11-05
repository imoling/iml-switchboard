package core

import (
	"github.com/imoling/iml-switchboard/internal/config"
)

// MediaModels 是这类媒体（image、video）现在能点名的全部型号：媒体设置里各档配的、账号自己选的出图出视频型号、
// 账号报上来的同类型号（自定义服务的 gpt-image-1、gpt-image-2 这些）。不可用的（没账号、只用免费型号）也列出来，带原因。
func (r *Router) MediaModels(kind Kind) []Target {
	cfg := r.cfg.Get()
	t := &Task{Kind: kind}
	out := r.mediaBase(cfg, t, "default") // 全集，不受手排顺序影响：点名、排顺序都要看到所有型号
	return append(out, r.discoveredMedia(cfg, kind, out)...)
}

// discoveredMedia 是账号报上来、还不在 have 里的同类型号（自定义服务的 gpt-image-1、gpt-image-2 这些）。
func (r *Router) discoveredMedia(cfg *config.Config, kind Kind, have []Target) []Target {
	var out []Target
	for _, acc := range cfg.API {
		if !acc.Enabled {
			continue
		}
		for _, d := range acc.Discovered {
			if GuessMediaKind(d.ID) != string(kind) || hasTarget(have, Target{Lane: LaneMedia, Model: d.ID, AccountID: acc.ID}) || hasTarget(out, Target{Lane: LaneMedia, Model: d.ID, AccountID: acc.ID}) {
				continue
			}
			tgt := Target{Lane: LaneMedia, Provider: acc.Provider, Model: d.ID, AccountID: acc.ID}
			tgt.EstCostUSD, _, _ = r.reg.MediaCost(d.ID, 1)
			if !acc.AllowPaid && len(r.reg.FreeModels(acc.Provider)) > 0 && !r.reg.IsFree(d.ID) {
				tgt.Skipped = "只用免费型号"
			}
			out = append(out, tgt)
		}
	}
	return out
}

// MediaOrderNow 是这类媒体现在实际的先后（手排了就是手排的，没排就是内置排法）。
func (r *Router) MediaOrderNow(kind Kind) []Target {
	return r.mediaCandidates(r.cfg.Get(), &Task{Kind: kind}, "default")
}

// pinMedia 把调用方点名的出图、出视频型号放到最前；其余照档位排在后面当回退。
// 点名的型号没配置就只留它一个、标上原因：点名了却悄悄换成别的，比报错更糟。
func (r *Router) pinMedia(cfg *config.Config, t *Task, d *Decision) {
	if t.Model == "" || t.Kind == KindAnimation {
		return
	}
	match := func(c Target) bool {
		return c.Model == t.Model && (t.Lane == "" || c.Lane == t.Lane)
	}
	var pinned, rest []Target
	for _, c := range d.Candidates {
		if match(c) && len(pinned) == 0 {
			pinned = append(pinned, c)
		} else {
			rest = append(rest, c)
		}
	}
	if len(pinned) == 0 {
		for _, c := range r.MediaModels(t.Kind) {
			if match(c) {
				pinned = append(pinned, c)
				break
			}
		}
	}
	name := r.reg.Display(t.Model)
	if len(pinned) == 0 {
		d.Candidates = []Target{{Lane: LaneMedia, Model: t.Model, Skipped: "没配置这个" + kindLabel(t.Kind) + "型号"}}
		d.Reason = append(d.Reason, "指定的"+kindLabel(t.Kind)+"型号 "+name+" 没配置")
		return
	}
	d.Candidates = append(pinned, rest...)
	d.Reason = append(d.Reason, "调用方指定模型 "+name)
}
