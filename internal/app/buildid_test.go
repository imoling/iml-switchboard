package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"
)

// 构建号必须来自程序文件本身：打包时忘了注入也不会退化成所有构建都一样
func TestBuildIDIsExecutableHash(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	want := hex.EncodeToString(sum[:])[:12]
	if got := BuildID(); got != want || BuildID() != got {
		t.Fatalf("构建号应是程序文件指纹的前 12 位：got %q want %q", got, want)
	}
}
