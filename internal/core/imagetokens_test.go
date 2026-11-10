package core

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

// 官方 vision 文档里高分辨率档的六组例子
func TestImageTokensMatchesDocs(t *testing.T) {
	for _, c := range []struct{ w, h, want int }{
		{200, 200, 64}, {1000, 1000, 1296}, {1092, 1092, 1521}, {1920, 1080, 2691}, {2000, 1500, 3888}, {3840, 2160, 4784},
	} {
		if got := imageTokensFor(c.w, c.h); got != c.want {
			t.Errorf("%d×%d：%d，应为 %d", c.w, c.h, got, c.want)
		}
	}
	// 超长图：先按长边缩，再不超过上限
	if got := imageTokensFor(1200, 8000); got > imageMaxTokens || got < 1000 {
		t.Errorf("长图：%d", got)
	}
}

func TestImageTokensFromDataURL(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 200, 200)))
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
	if got := ImageTokens(url); got != 64 {
		t.Fatalf("200×200 的 PNG 应为 64：%d", got)
	}
	// 读不出尺寸（网址、坏数据）按上限算
	for _, src := range []string{"https://example.com/a.png", "data:image/png;base64,AAAA", "data:image/webp;base64," + strings.Repeat("A", 40)} {
		if got := ImageTokens(src); got != imageMaxTokens {
			t.Errorf("%s：%d", src[:24], got)
		}
	}
	// 文字照旧，图片按尺寸：一张小图不会被算成几万
	n := EstimateMessages([]Message{{Content: "看图", Images: []string{url}}})
	if n < 64 || n > 100 {
		t.Fatalf("消息估算：%d", n)
	}
}
