package api

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// 带参考图走 /images/edits 的 multipart：图在 image 字段里，提示词、型号照传
func TestGenerateImageWithRefsUsesEdits(t *testing.T) {
	png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	ref := filepath.Join(t.TempDir(), "ref.png")
	if err := os.WriteFile(ref, png, 0o644); err != nil {
		t.Fatal(err)
	}
	var path, prompt, model string
	var got []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("不是 multipart：%v", err)
			return
		}
		prompt, model = r.FormValue("prompt"), r.FormValue("model")
		if f, _, err := r.FormFile("image"); err == nil {
			got, _ = io.ReadAll(f)
		}
		w.Write([]byte(`{"data":[{"b64_json":"` + base64.StdEncoding.EncodeToString(png) + `"}]}`))
	}))
	defer srv.Close()
	acct := testAccount(srv.URL + "/v1")
	res, err := NewAPIDriver(nil, srv.Client()).GenerateImage(context.Background(), acct, ImageGenRequest{Model: "gpt-image-2.5", Prompt: "改成像素画", Refs: []string{ref}})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/images/edits" || prompt != "改成像素画" || model != "gpt-image-2.5" || string(got) != string(png) {
		t.Fatalf("edits 请求不对：path=%s prompt=%q model=%q image=%d 字节", path, prompt, model, len(got))
	}
	if len(res.Data) == 0 {
		t.Fatal("没拿到图")
	}
}
