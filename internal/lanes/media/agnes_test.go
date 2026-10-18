package media

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/imoling/iml-switchboard/internal/lanes/api"
)

func agnesAccount(base string) api.Account {
	return api.Account{ID: "agnes-1", Provider: "agnes", BaseURL: base + "/v1", APIKeyGetter: func() (string, error) { return "k", nil }, Currency: "CNY"}
}

func TestAgnesImage(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" || r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(404)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"aGVsbG8=","url":null}]}`))
	}))
	defer srv.Close()
	p := &AgnesImage{Account: agnesAccount(srv.URL), Client: srv.Client()}
	res, err := p.Generate(context.Background(), ImageRequest{Model: "agnes-image-2.5-flash", Prompt: "一只猫", Size: "1536x1024"})
	if err != nil || string(res.Data) != "hello" {
		t.Fatalf("%v %q", err, res.Data)
	}
	if got["size"] != "1K" || got["ratio"] != "3:2" || got["return_base64"] != true {
		t.Fatalf("请求字段不对：%v", got)
	}
	tier, ratio := agnesSize("2048x2048")
	if tier != "2K" || ratio != "1:1" {
		t.Fatalf("%s %s", tier, ratio)
	}
}

func TestAgnesVideo(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/videos":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["seconds"] != "5" || body["size"] != "720P" || body["mode"] != "text" {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":{"message":"bad"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"video_id":"v1"}`))
		case r.Method == "GET" && r.URL.Path == "/agnesapi":
			if r.URL.Query().Get("video_id") != "v1" || r.URL.Query().Get("model_name") != "agnes-video-2.5-flash" {
				w.WriteHeader(400)
				return
			}
			polls++
			if polls < 2 {
				_, _ = w.Write([]byte(`{"id":"v1","status":"processing","progress":40}`))
				return
			}
			_, _ = w.Write([]byte(`{"id":"v1","status":"completed","progress":100,"url":"https://cdn/x.mp4"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := &AgnesVideo{Account: agnesAccount(srv.URL), Client: srv.Client()}
	ref, err := p.Submit(context.Background(), VideoRequest{Model: "agnes-video-2.5-flash", Prompt: "海边日出", Seconds: 5})
	if err != nil || !strings.HasPrefix(ref, "v1|") {
		t.Fatalf("%v %q", err, ref)
	}
	st, err := p.Poll(context.Background(), ref)
	if err != nil || st.Done || st.Progress != 0.4 {
		t.Fatalf("%+v %v", st, err)
	}
	st, _ = p.Poll(context.Background(), ref)
	if !st.Done || st.URL != "https://cdn/x.mp4" || st.Error != "" {
		t.Fatalf("%+v", st)
	}
}
