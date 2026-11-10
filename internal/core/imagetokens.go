package core

import (
	"encoding/base64"
	"image"
	_ "image/gif"  // 注册解码器：只读尺寸
	_ "image/jpeg" // 同上
	_ "image/png"  // 同上
	"math"
	"strings"
)

// 图片占多少 token，按 Claude 的规则（2026-11 官方 vision 文档）：每 28×28 像素一个 token；
// 长边超过 2576 或总数超过 4784 时先等比缩小。这是高分辨率档（4.7 及之后），老型号上限更低；
// 这里不知道最后派给谁，按高的估，宁可早压缩也别装不下。
// 以前按 base64 字符数除 4 估，一张 200 KB 的截图算成 6.8 万，几张图就被当成「装不下」退回。
const (
	imagePatch     = 28
	imageMaxEdge   = 2576
	imageMaxTokens = 4784
)

// ImageTokens 估一张图（data URL 或网址）占的 token；读不出尺寸的按上限算。
func ImageTokens(src string) int {
	w, h, ok := imageSize(src)
	if !ok {
		return imageMaxTokens
	}
	return imageTokensFor(w, h)
}

func imageTokensFor(w, h int) int {
	if w <= 0 || h <= 0 {
		return imageMaxTokens
	}
	count := func(s float64) int {
		return int(math.Ceil(float64(w)*s/imagePatch)) * int(math.Ceil(float64(h)*s/imagePatch))
	}
	scale := 1.0
	if long := max(w, h); long > imageMaxEdge {
		scale = float64(imageMaxEdge) / float64(long)
	}
	n := count(scale)
	if n > imageMaxTokens {
		scale *= math.Sqrt(float64(imageMaxTokens) / float64(n))
		for n = count(scale); n > imageMaxTokens; n = count(scale) {
			scale *= 0.99
		}
	}
	return n
}

// imageSize 从 data URL 里读出宽高，只解码到文件头，不解整张图。
func imageSize(src string) (int, int, bool) {
	if !strings.HasPrefix(src, "data:") {
		return 0, 0, false
	}
	i := strings.Index(src, ";base64,")
	if i < 0 {
		return 0, 0, false
	}
	cfg, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(src[i+len(";base64,"):])))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// EstimateMessages 估一组消息的输入 token：文字加图片。
func EstimateMessages(msgs []Message) int {
	n := 0
	for _, m := range msgs {
		n += EstimateTokens(m.Content)
		for _, img := range m.Images {
			n += ImageTokens(img)
		}
	}
	return n
}
