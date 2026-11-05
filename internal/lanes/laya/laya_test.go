package laya

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusAndKey(t *testing.T) {
	m := &Manager{Dir: filepath.Join(t.TempDir(), "laya")}
	st := m.Status()
	if st.Installed || st.Downloaded || st.Running {
		t.Fatalf("空目录应什么都没有：%+v", st)
	}
	if err := m.Start(); err == nil || !strings.Contains(err.Error(), "还没下载") {
		t.Fatalf("没装时启动应报还没下载：%v", err)
	}
	// 装好运行环境、下好权重
	bin := filepath.Join(m.Dir, "venv", "bin")
	_ = os.MkdirAll(bin, 0o755)
	_ = os.WriteFile(filepath.Join(bin, "laya-serve"), []byte("#!/bin/sh\n"), 0o755)
	w := filepath.Join(m.Dir, "hf", "hub", "models--convaiinnovations--laya", "snapshots", "abc", Checkpoint)
	_ = os.MkdirAll(w, 0o755)
	f, _ := os.Create(filepath.Join(w, "model.safetensors"))
	_ = f.Truncate(ModelBytes)
	f.Close()
	st = m.Status()
	if !st.Installed || !st.Downloaded {
		t.Fatalf("应认出装好、下好：%+v", st)
	}
	k1, err := m.Key()
	k2, _ := m.Key()
	if err != nil || len(k1) < 32 || k1 != k2 {
		t.Fatalf("本机密钥应生成一次、之后不变：%q %q %v", k1, k2, err)
	}
	if err := m.Remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.Dir); !os.IsNotExist(err) {
		t.Fatal("删除应清掉整个目录")
	}
}
