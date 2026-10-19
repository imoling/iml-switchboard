package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestMediaServeOnlyFilesInDir(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "a.png"), []byte("png"), 0o644)
	_ = os.WriteFile(filepath.Join(filepath.Dir(dir), "outside.png"), []byte("x"), 0o644)
	mux := http.NewServeMux()
	mux.Handle("GET /media/{name}", mediaHandler(dir))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	for path, want := range map[string]int{"/media/a.png": 200, "/media/missing.png": 404, "/media/..%2Foutside.png": 404} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Fatalf("%s 应为 %d，得到 %d", path, want, resp.StatusCode)
		}
	}
	u := mediaURL(dir)
	if got := u(filepath.Join(dir, "红 杏.png")); got != BaseURL+"/media/%E7%BA%A2%20%E6%9D%8F.png" {
		t.Fatalf("地址不对：%s", got)
	}
	if u(filepath.Join(dir, "sub", "a.png")) != "" || u("/elsewhere/a.png") != "" {
		t.Fatal("目录外的文件不该给地址")
	}
}
