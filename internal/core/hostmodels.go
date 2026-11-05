package core

import "strings"

// RouterModels 是公布给宿主的路由型号：自动分档、智能分派和三个档位。
var RouterModels = []struct{ ID, Name string }{
	{"router-auto", "Switchboard 自动分档"},
	{"router-smart", "Switchboard 智能分派"},
	{"router-high", "Switchboard 高档"},
	{"router-mid", "Switchboard 中档"},
	{"router-low", "Switchboard 低档"},
}

// DesktopModels 是 Claude Desktop 菜单里的型号。它只收 Claude 家的型号名，一个不合规整张表都作废，
// 所以借四个名字占位：ID 写进它的配置，Label 显示在菜单上，请求按 Router 走。第一行是默认。
var DesktopModels = []struct{ ID, Router, Label string }{
	{"claude-sonnet-5", "router-auto", "自动分档"},
	{"claude-opus-5", "router-high", "高档"},
	{"claude-sonnet-4-6", "router-mid", "中档"},
	{"claude-haiku-5-5", "router-low", "低档"},
}

// GuessMediaKind 按型号名认出图、出视频型号：自定义服务只报名字，没有类别。认不出返回空串，当文本型号。
func GuessMediaKind(id string) string {
	m := strings.ToLower(id)
	for _, w := range []string{"video", "sora", "veo", "kling", "seedance", "hailuo"} {
		if strings.Contains(m, w) {
			return "video"
		}
	}
	for _, w := range []string{"image", "dall-e", "imagen", "flux", "seedream", "midjourney"} {
		if strings.Contains(m, w) {
			return "image"
		}
	}
	return ""
}

// RefImageUnsupported 认出只能文生图、不接参考图的型号：DALL·E 3、名字带 t2i 的、Imagen。
func RefImageUnsupported(id string) bool {
	m := strings.ToLower(id)
	for _, w := range []string{"dall-e-3", "t2i", "imagen"} {
		if strings.Contains(m, w) {
			return true
		}
	}
	return false
}

// DesktopRoute 把 Claude Desktop 选的型号名换成路由型号；不是借的名字就原样返回。
func DesktopRoute(model string) string {
	id := strings.TrimSuffix(strings.ToLower(model), "[1m]")
	for _, m := range DesktopModels {
		if m.ID == id {
			return m.Router
		}
	}
	return model
}
