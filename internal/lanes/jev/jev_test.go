package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestChoose(t *testing.T) {
	var got map[string]any
	reply := `{"answers":{"model":{"choice":"2","probabilities":{"1":0.3,"2":0.7},"confidence":0.8}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k1" {
			w.WriteHeader(401)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(reply))
	}))
	defer srv.Close()
	c := &Client{Endpoint: srv.URL, Key: func() (string, error) { return "k1", nil }}
	a, err := c.Choose(context.Background(), map[string]any{"task": "x"}, map[string]string{"1": "小", "2": "中"}, "挑一个")
	if err != nil || a.Choice != "2" || a.Probabilities["2"] != 0.7 {
		t.Fatalf("%+v %v", a, err)
	}
	q := got["questions"].(map[string]any)["model"].(map[string]any)
	if got["model"] != Model || q["type"] != "choice" {
		t.Fatalf("请求体不对：%v", got)
	}
	// 回包不合法：概率加起来不是 1
	reply = `{"answers":{"model":{"choice":"2","probabilities":{"1":0.3,"2":0.3},"confidence":0.8}}}`
	if _, err := c.Choose(context.Background(), nil, map[string]string{"1": "小", "2": "中"}, ""); err == nil {
		t.Fatal("概率不合法应报错")
	}
	c.Key = func() (string, error) { return "bad", nil }
	if _, err := c.Choose(context.Background(), nil, map[string]string{"1": "小", "2": "中"}, ""); err == nil || !strings.Contains(err.Error(), "密钥不对") {
		t.Fatalf("密钥不对：%v", err)
	}
}

func TestChooseTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) }))
	defer srv.Close()
	c := &Client{Endpoint: srv.URL, Key: func() (string, error) { return "k", nil }, Timeout: 50 * time.Millisecond}
	if _, err := c.Choose(context.Background(), nil, map[string]string{"1": "a", "2": "b"}, ""); err == nil || !strings.Contains(err.Error(), "超时") {
		t.Fatalf("应超时：%v", err)
	}
}

func TestRedact(t *testing.T) {
	in := "联系 kang@iflytek.com 或 13912345678，密钥 sk-abcdefghijklmnopqrstu，路径 /Users/imoling/a.txt，api_key=xyz123456"
	out := Redact(in, 1500)
	for _, bad := range []string{"kang@iflytek.com", "13912345678", "sk-abcdefghij", "/Users/imoling", "xyz123456"} {
		if strings.Contains(out, bad) {
			t.Fatalf("没脱敏 %q：%s", bad, out)
		}
	}
	if got := Redact(strings.Repeat("字", 2000), 10); len([]rune(got)) != 11 {
		t.Fatalf("应截到 10 字：%d", len([]rune(got)))
	}
}
