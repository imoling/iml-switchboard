//go:build !windows

package app

import (
	"os"
	"strings"
	"testing"
)

func TestMergePathKeepsOrderAndDedupes(t *testing.T) {
	if got := mergePath([]string{"/usr/bin", "", "/opt/homebrew/bin", "/usr/bin", " /bin "}); got != "/usr/bin:/opt/homebrew/bin:/bin" {
		t.Fatal(got)
	}
}

// 从访达拉起时只有系统目录；补完之后至少要有 Homebrew 目录，原有的排在前面
func TestFixPathFromMinimalEnv(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin:/usr/sbin:/sbin")
	FixPath()
	p := os.Getenv("PATH")
	if !strings.HasPrefix(p, "/usr/bin:/bin:/usr/sbin:/sbin") || !strings.Contains(p, "/opt/homebrew/bin") {
		t.Fatal(p)
	}
}
