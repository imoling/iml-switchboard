package app

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

// BuildID 是当前程序文件内容的指纹。版本号相同的两次构建也能分开，
// 客户端拿它和安装包里附带的 imls 比对，就能发现「连着的不是这个版本的后台」。
// 在启动时算一次：程序文件之后被新包覆盖，报的仍是正在跑的这一份。
var BuildID = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	f, err := os.Open(exe)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
})
