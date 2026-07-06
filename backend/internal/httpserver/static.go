package httpserver

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// staticHandler 提供前端静态文件服务和 Vue history 路由回退。
// API 请求（/api/）不经过此 handler，由调用方在路由层分流。
func staticHandler(publicDir string) http.Handler {
	fs := http.Dir(publicDir)
	fileServer := http.FileServer(fs)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 安全：禁止路径穿越
		cleanPath := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
		if strings.Contains(cleanPath, "..") {
			http.NotFound(w, r)
			return
		}

		// 检查文件是否存在
		fullPath := filepath.Join(publicDir, filepath.FromSlash(cleanPath))
		info, err := os.Stat(fullPath)
		if err == nil && !info.IsDir() {
			if strings.HasPrefix(cleanPath, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if cleanPath == "/" || strings.HasSuffix(cleanPath, ".html") {
				w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		if strings.HasPrefix(cleanPath, "/assets/") {
			http.NotFound(w, r)
			return
		}

		// 文件不存在或是目录，回退到 index.html（Vue history 路由）
		indexPath := filepath.Join(publicDir, "index.html")
		if _, err := os.Stat(indexPath); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		http.ServeFile(w, r, indexPath)
	})
}
