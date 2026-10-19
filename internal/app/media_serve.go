package app

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// mediaURL 把数据目录 media/ 里的产物换成本机地址，宿主的 Markdown 才能把图显示出来；别处的文件不对外提供。
func mediaURL(dir string) func(string) string {
	return func(p string) string {
		if filepath.Dir(p) != dir {
			return ""
		}
		return BaseURL + "/media/" + url.PathEscape(filepath.Base(p))
	}
}

// mediaHandler 只提供 dir 里的普通文件，不认子目录和 ..；只监听本机，图片本身不是秘密，所以不要客户端标识。
func mediaHandler(dir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
			http.NotFound(w, r)
			return
		}
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, p)
	})
}
