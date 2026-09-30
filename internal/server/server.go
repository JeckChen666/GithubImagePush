// Package server 提供 HTTP 服务：内嵌 Web 页面 + JSON API。
package server

import (
	"embed"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"githubimagepush/internal/config"
	"githubimagepush/internal/githubx"
)

//go:embed web
var webFiles embed.FS

type Server struct {
	cfg     *config.Config
	gh      *githubx.Client
	log     *slog.Logger
	limiter *authLimiter
}

func New(cfg *config.Config, logger *slog.Logger) *Server {
	return &Server{
		cfg:     cfg,
		gh:      githubx.New(cfg.GitHub.APIURL, cfg.GitHub.Token, cfg.GitHub.Repo, cfg.GitHub.Branch),
		log:     logger,
		limiter: newAuthLimiter(),
	}
}

// Handler 返回挂载全部路由与中间件的 http.Handler。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// 无需鉴权
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]any{"status": "ok"}})
	})

	// 需要鉴权的 API
	mux.HandleFunc("GET /api/verify", s.auth(s.handleVerify))
	mux.HandleFunc("POST /api/upload", s.auth(s.handleUpload))
	mux.HandleFunc("GET /api/list", s.auth(s.handleList))
	mux.HandleFunc("DELETE /api/delete", s.auth(s.handleDelete))
	mux.HandleFunc("GET /api/raw", s.auth(s.handleRaw))

	// 兼容 PicGo 生态自定义 Web Uploader 插件的常见路径，
	// 输入与 /api/upload 相同，输出为 {success, result: [url...]} 形式。
	mux.HandleFunc("POST /upload", s.auth(s.handleUploadPicGo))

	// 内嵌前端页面与静态资源（/ 交给 FileServer，命中 index.html）
	webFS, _ := fs.Sub(webFiles, "web")
	mux.Handle("GET /", http.FileServerFS(webFS))

	return s.recoverPanic(s.requestLog(mux))
}

// ---- 中间件 ----

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(sw, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start).Round(time.Millisecond).String(),
			"ip", clientIP(r),
		)
	})
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "err", rec, "stack", string(debug.Stack()))
				writeJSON(w, http.StatusInternalServerError, envelope{Success: false, Message: "服务内部错误"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---- JSON 输出 ----

type envelope struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
