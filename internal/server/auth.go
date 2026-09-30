package server

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// extractKey 依次从 Authorization 头（Bearer/token 前缀，兼容 PicGo/GitHub 风格）、
// X-API-Key 头、URL query 参数中提取 key。
func extractKey(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		for _, prefix := range []string{"Bearer ", "bearer ", "token ", "Token "} {
			if strings.HasPrefix(h, prefix) {
				return strings.TrimSpace(strings.TrimPrefix(h, prefix))
			}
		}
		return strings.TrimSpace(h)
	}
	if k := r.Header.Get("X-API-Key"); k != "" {
		return strings.TrimSpace(k)
	}
	return strings.TrimSpace(r.URL.Query().Get("key"))
}

func (s *Server) validKey(key string) bool {
	if key == "" {
		return false
	}
	for _, k := range s.cfg.Auth.Keys {
		// 常量时间比较，避免时序侧信道逐位猜测 key
		if subtle.ConstantTimeCompare([]byte(key), []byte(k)) == 1 {
			return true
		}
	}
	return false
}

const (
	authFailWindow  = 15 * time.Minute // 统计窗口
	authFailLimit   = 12               // 窗口内允许的失败次数
	authBlockPeriod = 15 * time.Minute // 触发后的封禁时长
)

type failRecord struct {
	count        int
	firstFail    time.Time
	blockedUntil time.Time
}

// authLimiter 基于 IP 的鉴权失败限速，用于缓解 key 暴力枚举。
type authLimiter struct {
	mu      sync.Mutex
	records map[string]*failRecord
}

func newAuthLimiter() *authLimiter {
	return &authLimiter{records: map[string]*failRecord{}}
}

func (l *authLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.records[ip]
	if !ok {
		return false
	}
	if time.Now().Before(rec.blockedUntil) {
		return true
	}
	l.prune(ip, rec)
	return false
}

func (l *authLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	rec, ok := l.records[ip]
	if !ok || now.Sub(rec.firstFail) > authFailWindow {
		rec = &failRecord{firstFail: now}
		l.records[ip] = rec
	}
	rec.count++
	if rec.count >= authFailLimit {
		rec.blockedUntil = now.Add(authBlockPeriod)
		rec.count = 0
		rec.firstFail = now
	}
}

func (l *authLimiter) ok(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.records, ip)
}

func (l *authLimiter) prune(ip string, rec *failRecord) {
	if !rec.blockedUntil.IsZero() && time.Now().After(rec.blockedUntil) &&
		time.Now().Sub(rec.firstFail) > authFailWindow {
		delete(l.records, ip)
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// auth 包装需要鉴权的 handler；API 请求携带 key 或网页输入 key 后由前端附带。
func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if s.limiter.blocked(ip) {
			writeJSON(w, http.StatusTooManyRequests, envelope{Success: false, Message: "鉴权失败次数过多，请稍后再试"})
			return
		}
		key := extractKey(r)
		if !s.validKey(key) {
			s.limiter.fail(ip)
			w.Header().Set("WWW-Authenticate", `Bearer realm="GithubImagePush"`)
			writeJSON(w, http.StatusUnauthorized, envelope{Success: false, Message: "无效的 Key：请通过 Authorization: Bearer <key>、X-API-Key 头或 ?key= 参数携带"})
			return
		}
		s.limiter.ok(ip)
		next(w, r)
	}
}
