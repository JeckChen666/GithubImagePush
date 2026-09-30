// Package naming 负责上传路径、文件名与最终图片链接的生成，
// 对应 PicGo 中 handleUrlPathSafeEncode 与链接拼接的逻辑。
package naming

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"githubimagepush/internal/config"
)

// SanitizeBaseName 清洗原始文件名中的危险字符，返回不含扩展名的安全基名。
func SanitizeBaseName(name string) string {
	name = strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
	if name == "." || name == "/" || name == "" {
		return "image"
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r < 0x20 || r == 0x7f: // 控制字符
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' ||
			r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), ".- ")
	if len(out) > 120 {
		out = out[:120]
	}
	if out == "" {
		return "image"
	}
	return out
}

// ExtOf 返回小写扩展名（含点），无扩展名返回 ""。
func ExtOf(filename string) string {
	ext := strings.ToLower(path.Ext(filename))
	if len(ext) > 1 {
		return ext
	}
	return ""
}

// TimestampName 生成 "20060102150445-ab12cd34.png" 形式的文件名，避免同名冲突。
func TimestampName(now time.Time, ext string) string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return now.Format("20060102150405") + "-" + hex.EncodeToString(buf[:]) + ext
}

// RenderDirTemplate 渲染目录模板，支持 {year} {month} {day} {timestamp} {rand}。
func RenderDirTemplate(tmpl string, now time.Time) string {
	if tmpl == "" {
		return ""
	}
	var rnd string
	if strings.Contains(tmpl, "{rand}") {
		var buf [3]byte
		_, _ = rand.Read(buf[:])
		rnd = hex.EncodeToString(buf[:])
	}
	r := strings.NewReplacer(
		"{year}", now.Format("2006"),
		"{month}", now.Format("01"),
		"{day}", now.Format("02"),
		"{timestamp}", now.Format("20060102150405"),
		"{rand}", rnd,
	)
	return r.Replace(tmpl)
}

// BuildObjectPath 计算文件在仓库内的完整路径（rootPath + 目录模板 + 文件名）。
func BuildObjectPath(rootPath, dirTemplate, filename string, now time.Time) string {
	parts := []string{}
	for _, p := range []string{rootPath, RenderDirTemplate(dirTemplate, now), filename} {
		p = strings.TrimSpace(p)
		if p != "" {
			parts = append(parts, p)
		}
	}
	return path.Clean(strings.Join(parts, "/"))
}

// EscapePathSegments 对路径的每一段做 URL 编码（对应 PicGo 的
// handleUrlPathSafeEncode：逐段 encodeURIComponent，避免整段编码吞掉 "/"）。
func EscapePathSegments(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		e := url.PathEscape(s)
		// PathEscape 对部分字符放行（如 "+"），在 URL 路径段中补齐编码更稳妥
		e = strings.ReplaceAll(e, "+", "%2B")
		segs[i] = e
	}
	return strings.Join(segs, "/")
}

// SanitizeRequestPath 清洗来自请求参数的仓库路径：不允许 ..、不允许前导 /，
// 返回仓库相对路径（"" 表示仓库根目录）。
func SanitizeRequestPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(strings.ReplaceAll(p, "\\", "/"), "/")
	if p == "" {
		return ""
	}
	segs := []string{}
	for _, s := range strings.Split(p, "/") {
		s = strings.TrimSpace(s)
		if s == "" || s == "." || s == ".." {
			continue
		}
		segs = append(segs, s)
	}
	return strings.Join(segs, "/")
}

// BuildURLs 生成各格式的图片链接。customURL 为空时不含 custom 项。
// fullPath 为仓库内路径（未编码）。
func BuildURLs(repo, branch, fullPath, customURL string) map[string]string {
	esc := EscapePathSegments(fullPath)
	urls := map[string]string{
		config.FormatRaw:      fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repo, branch, esc),
		config.FormatJsdelivr: fmt.Sprintf("https://cdn.jsdelivr.net/gh/%s@%s/%s", repo, branch, esc),
		config.FormatGithub:   fmt.Sprintf("https://github.com/%s/raw/%s/%s", repo, branch, esc),
	}
	if customURL != "" {
		urls[config.FormatCustom] = joinCustom(customURL, esc)
	}
	return urls
}

// BuildURL 返回指定格式的链接，未知格式回退 raw。
func BuildURL(format, repo, branch, fullPath, customURL string) string {
	urls := BuildURLs(repo, branch, fullPath, customURL)
	if u, ok := urls[format]; ok {
		return u
	}
	return urls[config.FormatRaw]
}

func joinCustom(customURL, escPath string) string {
	// 支持模板形式：customUrl 里带 {path} 占位符则替换，否则按前缀拼接（兼容 PicGo 习惯）
	if strings.Contains(customURL, "{path}") {
		return strings.ReplaceAll(customURL, "{path}", escPath)
	}
	return strings.TrimRight(customURL, "/") + "/" + escPath
}
