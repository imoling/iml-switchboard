package sub

import (
	"context"
	"os"
	"testing"
	"time"
)

// 真实探测：IMLS_LIVE=1 时才跑，用于本机排查。
func TestCodexProbeLive(t *testing.T) {
	if os.Getenv("IMLS_LIVE") == "" {
		t.Skip("需要 IMLS_LIVE=1")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	ws, err := NewCodexDriver().ProbeQuota(ctx)
	t.Logf("windows=%+v err=%v", ws, err)
	if err != nil {
		t.Fatal(err)
	}
}
