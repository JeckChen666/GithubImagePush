package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"path"
	"strings"
	"time"

	"githubimagepush/internal/config"
	"githubimagepush/internal/githubx"
	"githubimagepush/internal/naming"
)

// maxFilesPerRequest 单次上传请求允许的最大文件数。
const maxFilesPerRequest = 20

// handleVerify 校验 key 有效性并返回服务端配置摘要（供网页展示）。
func (s *Server) handleVerify(w http.ResponseWriter, r *http.Request) {
	g := s.cfg.GitHub
	formats := []string{config.FormatRaw, config.FormatJsdelivr, config.FormatGithub}
	if g.CustomURL != "" {
		formats = append(formats, config.FormatCustom)
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]any{
		"configured":       s.cfg.Configured(),
		"repo":             g.Repo,
		"branch":           g.Branch,
		"path":             g.Path,
		"urlFormat":        g.URLFormat,
		"formats":          formats,
		"pathTemplate":     g.PathTemplate,
		"filenameStrategy": g.FilenameStrategy,
		"maxUploadSize":    s.cfg.Server.MaxUploadSize,
		"allowExtensions":  g.AllowExtensions,
	}})
}

// ---- 上传 ----

type uploadItem struct {
	Filename string `json:"filename"`
	Data     string `json:"data"` // base64，兼容 data:image/png;base64,xxx 前缀
}

type uploadJSONBody struct {
	Filename string       `json:"filename"`
	Data     string       `json:"data"`
	Files    []uploadItem `json:"files"`
}

type uploadResult struct {
	Name     string            `json:"name"`
	Path     string            `json:"path"`
	Size     int64             `json:"size"`
	URL      string            `json:"url,omitempty"`
	URLs     map[string]string `json:"urls,omitempty"`
	Markdown string            `json:"markdown,omitempty"`
	HTML     string            `json:"html,omitempty"`
	BBCode   string            `json:"bbcode,omitempty"`
	Success  bool              `json:"success"`
	Message  string            `json:"message,omitempty"`
}

type incomingFile struct {
	name string
	data []byte
	err  string // 采集阶段的问题（如超限），逐文件报错而不中断其他文件
}

// collectFiles 从 multipart 表单（任意带文件名的字段，file/files 均可）
// 或 JSON body（{"filename","data"} / {"files":[...]}，data 为 base64）中提取文件。
func (s *Server) collectFiles(r *http.Request) ([]incomingFile, string, int, bool) {
	mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))

	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		mr := multipart.NewReader(r.Body, params["boundary"])
		var files []incomingFile
		for {
			part, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, "解析上传表单失败: " + err.Error(), http.StatusBadRequest, false
			}
			if part.FileName() == "" {
				continue // 普通字段，忽略
			}
			// 逐文件限制大小；超限时排空该 part 再继续处理其余文件
			data, _ := io.ReadAll(io.LimitReader(part, s.cfg.Server.MaxUploadSize+1))
			f := incomingFile{name: part.FileName(), data: data}
			if int64(len(data)) > s.cfg.Server.MaxUploadSize {
				_, _ = io.Copy(io.Discard, part)
				f.err = fmt.Sprintf("文件超过大小限制（%s）", humanSize(s.cfg.Server.MaxUploadSize))
			}
			files = append(files, f)
			if len(files) > maxFilesPerRequest {
				return nil, fmt.Sprintf("单次请求最多上传 %d 个文件", maxFilesPerRequest), http.StatusBadRequest, false
			}
		}
		if len(files) == 0 {
			return nil, "multipart 请求中未找到文件（请用 file 字段携带文件）", http.StatusBadRequest, false
		}
		return files, "", 0, true

	case mediaType == "application/json":
		limit := s.cfg.Server.MaxUploadSize*maxFilesPerRequest*4/3 + (4 << 20) // base64 膨胀 4/3
		var body uploadJSONBody
		if err := json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(&body); err != nil {
			return nil, "解析 JSON 失败: " + err.Error(), http.StatusBadRequest, false
		}
		items := body.Files
		if body.Filename != "" || body.Data != "" {
			items = append([]uploadItem{{Filename: body.Filename, Data: body.Data}}, items...)
		}
		if len(items) == 0 {
			return nil, "JSON body 需要提供 filename+data 或 files 字段", http.StatusBadRequest, false
		}
		if len(items) > maxFilesPerRequest {
			return nil, fmt.Sprintf("单次请求最多上传 %d 个文件", maxFilesPerRequest), http.StatusBadRequest, false
		}
		files := make([]incomingFile, 0, len(items))
		for _, it := range items {
			if it.Data == "" {
				return nil, "files[].data 不能为空", http.StatusBadRequest, false
			}
			raw := it.Data
			if strings.HasPrefix(raw, "data:") {
				if i := strings.Index(raw, ","); i >= 0 {
					raw = raw[i+1:]
				}
			}
			data, err := base64.StdEncoding.DecodeString(raw)
			if err != nil {
				return nil, fmt.Sprintf("%q 的 data 不是合法的 base64", it.Filename), http.StatusBadRequest, false
			}
			name := it.Filename
			if name == "" {
				name = "image.png"
			}
			files = append(files, incomingFile{name: name, data: data})
		}
		return files, "", 0, true

	default:
		return nil, "Content-Type 需为 multipart/form-data（表单上传）或 application/json（base64 上传）", http.StatusUnsupportedMediaType, false
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	s.limitBody(w, r)
	status, env := s.processUpload(r)
	writeJSON(w, status, env)
}

// handleUploadPicGo 以 PicGo 自定义 Web Uploader 插件的响应格式输出。
func (s *Server) handleUploadPicGo(w http.ResponseWriter, r *http.Request) {
	s.limitBody(w, r)
	status, env := s.processUpload(r)
	if !env.Success {
		writeJSON(w, status, *env)
		return
	}
	results, _ := env.Data.([]uploadResult)
	urls := make([]string, 0, len(results))
	for _, item := range results {
		if item.Success {
			urls = append(urls, item.URL)
		}
	}
	if len(urls) == 0 {
		writeJSON(w, status, envelope{Success: false, Message: firstError(results)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "result": urls})
}

// limitBody 为请求体设置硬上限，防止异常客户端拖垮服务。
func (s *Server) limitBody(w http.ResponseWriter, r *http.Request) {
	limit := s.cfg.Server.MaxUploadSize*(maxFilesPerRequest+1) + (32 << 20)
	r.Body = http.MaxBytesReader(w, r.Body, limit)
}

func firstError(results []uploadResult) string {
	for _, item := range results {
		if item.Message != "" {
			return item.Message
		}
	}
	return "上传失败"
}

func (s *Server) processUpload(r *http.Request) (int, *envelope) {
	if !s.cfg.Configured() {
		return http.StatusServiceUnavailable, &envelope{Success: false, Message: "服务端未配置 GitHub token/repo，请检查配置文件"}
	}
	files, msg, code, ok := s.collectFiles(r)
	if !ok {
		return code, &envelope{Success: false, Message: msg}
	}

	results := make([]uploadResult, 0, len(files))
	anySuccess := false
	for _, f := range files {
		item := s.uploadOne(r, f)
		if item.Success {
			anySuccess = true
		}
		results = append(results, item)
	}
	env := &envelope{Success: anySuccess, Data: results}
	if !anySuccess {
		env.Message = "所有文件上传失败"
	}
	return http.StatusOK, env
}

func (s *Server) uploadOne(r *http.Request, f incomingFile) uploadResult {
	g := s.cfg.GitHub
	item := uploadResult{Name: f.name, Size: int64(len(f.data))}

	if f.err != "" {
		item.Message = f.err
		return item
	}
	if int64(len(f.data)) > s.cfg.Server.MaxUploadSize {
		item.Message = fmt.Sprintf("文件超过大小限制（%s）", humanSize(s.cfg.Server.MaxUploadSize))
		return item
	}

	ext := naming.ExtOf(f.name)
	if ext == "" {
		// 无扩展名时嗅探内容类型兜底
		if e, ok := extFromSniff(f.data); ok {
			ext = e
		}
	}
	if ext == "" || !s.cfg.ExtAllowed(strings.TrimPrefix(ext, ".")) {
		item.Message = "不允许的文件类型（扩展名需为: " + strings.Join(g.AllowExtensions, ", ") + "）"
		return item
	}

	base := strings.TrimSuffix(path.Base(strings.ReplaceAll(f.name, "\\", "/")), path.Ext(f.name))
	var filename string
	switch g.FilenameStrategy {
	case config.NameOriginal:
		filename = naming.SanitizeBaseName(base) + ext
	default:
		filename = naming.TimestampName(time.Now(), ext)
	}
	objPath := naming.BuildObjectPath(g.Path, g.PathTemplate, filename, time.Now())

	info, err := s.gh.Upload(r.Context(), objPath, f.data, g.CommitMessage)
	if errors.Is(err, githubx.ErrExists) {
		if g.OnConflict == config.ConflictOverwrite {
			info, err = s.gh.UploadReplace(r.Context(), objPath, f.data, g.CommitMessage)
		} else {
			url := naming.BuildURL(config.FormatRaw, g.Repo, g.Branch, objPath, g.CustomURL)
			item.Message = "同名文件已存在（如需覆盖请将 onConflict 设为 overwrite）：" + url
			return item
		}
	}
	if err != nil {
		item.Message = friendlyGHErr(err)
		return item
	}

	item.Path = info.Path
	item.URLs = naming.BuildURLs(g.Repo, g.Branch, info.Path, g.CustomURL)
	item.URL = item.URLs[g.URLFormat]
	if item.URL == "" {
		item.URL = item.URLs[config.FormatRaw]
	}
	alt := strings.TrimSuffix(info.Name, path.Ext(info.Name))
	item.Markdown = fmt.Sprintf("![%s](%s)", alt, item.URL)
	item.HTML = fmt.Sprintf(`<img src="%s" alt="%s"/>`, item.URL, alt)
	item.BBCode = fmt.Sprintf("[img]%s[/img]", item.URL)
	item.Success = true
	return item
}

// ---- 图库 ----

type listEntry struct {
	Name  string            `json:"name"`
	Path  string            `json:"path"`
	Type  string            `json:"type"`
	Size  int64             `json:"size"`
	SHA   string            `json:"sha"`
	URL   string            `json:"url"`
	URLs  map[string]string `json:"urls"`
	IsDir bool              `json:"isDir"`
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Message: "服务端未配置 GitHub token/repo"})
		return
	}
	dir := naming.SanitizeRequestPath(r.URL.Query().Get("dir"))
	entries, err := s.gh.List(r.Context(), dir)
	if err != nil {
		writeErr(w, err)
		return
	}
	g := s.cfg.GitHub
	out := make([]listEntry, 0, len(entries))
	for _, e := range entries {
		if e.Type != "file" && e.Type != "dir" {
			continue
		}
		urls := naming.BuildURLs(g.Repo, g.Branch, e.Path, g.CustomURL)
		out = append(out, listEntry{
			Name: e.Name, Path: e.Path, Type: e.Type, Size: e.Size, SHA: e.SHA,
			URL: urls[g.URLFormat], URLs: urls, IsDir: e.Type == "dir",
		})
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]any{"dir": dir, "entries": out}})
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Message: "服务端未配置 GitHub token/repo"})
		return
	}
	objPath := naming.SanitizeRequestPath(r.URL.Query().Get("path"))
	if objPath == "" {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Message: "缺少 path 参数"})
		return
	}
	info, err := s.gh.Get(r.Context(), objPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	if info.Type == "dir" {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Message: "不支持删除目录，请先删除目录内的文件"})
		return
	}
	if err := s.gh.Delete(r.Context(), objPath, info.SHA, "Delete by GithubImagePush"); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]any{"path": objPath}})
}

// handleRaw 通过服务端代理输出文件内容（带 token 请求 GitHub），
// 用于私有仓库的图库缩略图预览（<img> 标签无法携带鉴权头）。
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Configured() {
		writeJSON(w, http.StatusServiceUnavailable, envelope{Success: false, Message: "服务端未配置 GitHub token/repo"})
		return
	}
	objPath := naming.SanitizeRequestPath(r.URL.Query().Get("path"))
	if objPath == "" {
		writeJSON(w, http.StatusBadRequest, envelope{Success: false, Message: "缺少 path 参数"})
		return
	}
	info, err := s.gh.Get(r.Context(), objPath)
	if err != nil {
		writeErr(w, err)
		return
	}
	var content []byte
	if c := info.DecodedContent(); len(c) > 0 {
		content = c
	} else if info.SHA != "" {
		content, err = s.gh.BlobContent(r.Context(), info.SHA)
		if err != nil {
			writeErr(w, err)
			return
		}
	}
	if len(content) == 0 {
		writeJSON(w, http.StatusNotFound, envelope{Success: false, Message: "文件内容为空或过大"})
		return
	}
	w.Header().Set("Content-Type", contentTypeByExt(naming.ExtOf(objPath)))
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("Content-Length", fmt.Sprint(len(content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

// ---- 错误与工具 ----

func writeErr(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	switch {
	case errors.Is(err, githubx.ErrNotFound):
		status = http.StatusNotFound
	case githubx.IsUnauthorized(err):
		status = http.StatusBadGateway
	}
	writeJSON(w, status, envelope{Success: false, Message: friendlyGHErr(err)})
}

func friendlyGHErr(err error) string {
	var ae *githubx.APIError
	if errors.As(err, &ae) {
		if ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden {
			return "GitHub 拒绝了请求（" + ae.Message + "）：请检查 token 是否有效、是否拥有该仓库的 contents 读写权限"
		}
		return "GitHub API 错误（HTTP " + fmt.Sprint(ae.Status) + "）：" + ae.Message
	}
	if errors.Is(err, githubx.ErrNotFound) {
		return "文件或目录在仓库中不存在"
	}
	return err.Error()
}

func extFromSniff(data []byte) (string, bool) {
	n := len(data)
	if n > 512 {
		n = 512
	}
	switch http.DetectContentType(data[:n]) {
	case "image/png":
		return ".png", true
	case "image/jpeg":
		return ".jpg", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/bmp":
		return ".bmp", true
	case "image/svg+xml":
		return ".svg", true
	case "image/avif":
		return ".avif", true
	}
	return "", false
}

func contentTypeByExt(ext string) string {
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
