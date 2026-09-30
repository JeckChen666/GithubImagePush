// Package githubx 封装 GitHub Contents / Blobs API，
// 上传方式与 PicGo 一致：PUT /repos/{repo}/contents/{path}，body 携带 base64。
package githubx

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrNotFound 目标文件或目录在仓库中不存在。
	ErrNotFound = errors.New("github: file not found")
	// ErrExists 同名文件已存在（HTTP 422）。
	ErrExists = errors.New("github: file already exists")
)

// APIError 携带 GitHub 返回的非 2xx 状态与错误信息。
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: HTTP %d: %s", e.Status, e.Message)
}

// IsUnauthorized 判断是否 Token 无效或权限不足。
func IsUnauthorized(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden)
}

// FileInfo 对应 Contents API 返回的 content 对象。
type FileInfo struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	SHA          string `json:"sha"`
	Size         int64  `json:"size"`
	Type         string `json:"type"` // "file" | "dir"
	DownloadURL  string `json:"download_url"`
	HTMLURL      string `json:"html_url"`
	Content      string `json:"content"` // Contents API 对 <1MB 文件直接返回 base64（内部使用）
	contentBytes []byte
}

// DecodedContent 返回文件内容（仅对 <1MB 的文件可用，用于 /api/raw 代理）。
func (f *FileInfo) DecodedContent() []byte {
	if f.contentBytes == nil && f.Content != "" {
		if b, err := base64.StdEncoding.DecodeString(f.Content); err == nil {
			f.contentBytes = b
		}
	}
	return f.contentBytes
}

type Client struct {
	baseURL string
	token   string
	repo    string
	branch  string
	hc      *http.Client
}

func New(apiURL, token, repo, branch string) *Client {
	return &Client{
		baseURL: strings.TrimRight(apiURL, "/"),
		token:   token,
		repo:    repo,
		branch:  branch,
		hc:      &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *Client) contentsURL(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, c.repo, strings.Join(segs, "/"))
}

// Upload 创建新文件；同名已存在时返回 ErrExists。
func (c *Client) Upload(ctx context.Context, objPath string, content []byte, message string) (*FileInfo, error) {
	return c.putContents(ctx, objPath, content, message, "")
}

// UploadReplace 上传并覆盖同名文件（先 GET 取 sha 再 PUT）。
func (c *Client) UploadReplace(ctx context.Context, objPath string, content []byte, message string) (*FileInfo, error) {
	old, err := c.Get(ctx, objPath)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return c.putContents(ctx, objPath, content, message, "")
		}
		return nil, err
	}
	return c.putContents(ctx, objPath, content, message, old.SHA)
}

func (c *Client) putContents(ctx context.Context, objPath string, content []byte, message, sha string) (*FileInfo, error) {
	body := map[string]any{"message": message, "content": base64.StdEncoding.EncodeToString(content)}
	if c.branch != "" {
		body["branch"] = c.branch
	}
	if sha != "" {
		body["sha"] = sha
	}
	res, err := c.do(ctx, http.MethodPut, c.contentsURL(objPath), body)
	if err != nil {
		return nil, err
	}
	switch {
	case res.status == http.StatusOK || res.status == http.StatusCreated:
		var out struct {
			Content FileInfo `json:"content"`
		}
		if err := json.Unmarshal(res.body, &out); err != nil {
			return nil, fmt.Errorf("解析上传响应失败: %w", err)
		}
		return &out.Content, nil
	case res.status == http.StatusUnprocessableEntity && sha == "":
		// PicGo 同样以 422 表示文件已存在
		return nil, ErrExists
	default:
		return nil, &APIError{Status: res.status, Message: ghErrorMessage(res.body, res.status)}
	}
}

// Get 获取单个文件/目录条目信息。
func (c *Client) Get(ctx context.Context, objPath string) (*FileInfo, error) {
	res, err := c.do(ctx, http.MethodGet, c.contentsURL(objPath)+"?ref="+url.QueryEscape(c.branch), nil)
	if err != nil {
		return nil, err
	}
	switch {
	case res.status == http.StatusOK:
		var fi FileInfo
		if err := json.Unmarshal(res.body, &fi); err != nil {
			return nil, fmt.Errorf("解析响应失败: %w", err)
		}
		return &fi, nil
	case res.status == http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, &APIError{Status: res.status, Message: ghErrorMessage(res.body, res.status)}
	}
}

// List 列出目录下的条目（文件与子目录）。
func (c *Client) List(ctx context.Context, dir string) ([]FileInfo, error) {
	res, err := c.do(ctx, http.MethodGet, c.contentsURL(dir)+"?ref="+url.QueryEscape(c.branch), nil)
	if err != nil {
		return nil, err
	}
	switch {
	case res.status == http.StatusOK:
		var arr []FileInfo
		if err := json.Unmarshal(res.body, &arr); err == nil {
			return arr, nil
		}
		// 路径本身是文件时 Contents API 返回对象而非数组
		var one FileInfo
		if err := json.Unmarshal(res.body, &one); err != nil {
			return nil, fmt.Errorf("解析目录列表失败: %w", err)
		}
		return []FileInfo{one}, nil
	case res.status == http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, &APIError{Status: res.status, Message: ghErrorMessage(res.body, res.status)}
	}
}

// Delete 删除文件（Contents API 需要文件的 sha）。
func (c *Client) Delete(ctx context.Context, objPath, sha, message string) error {
	body := map[string]any{"message": message, "sha": sha}
	if c.branch != "" {
		body["branch"] = c.branch
	}
	res, err := c.do(ctx, http.MethodDelete, c.contentsURL(objPath), body)
	if err != nil {
		return err
	}
	switch {
	case res.status == http.StatusOK:
		return nil
	case res.status == http.StatusNotFound:
		return ErrNotFound
	default:
		return &APIError{Status: res.status, Message: ghErrorMessage(res.body, res.status)}
	}
}

// BlobContent 通过 Blobs API 取文件内容，用于 Contents API 不内联（>=1MB）的文件。
func (c *Client) BlobContent(ctx context.Context, sha string) ([]byte, error) {
	u := fmt.Sprintf("%s/repos/%s/git/blobs/%s", c.baseURL, c.repo, url.PathEscape(sha))
	res, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if res.status != http.StatusOK {
		return nil, &APIError{Status: res.status, Message: ghErrorMessage(res.body, res.status)}
	}
	var out struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(res.body, &out); err != nil {
		return nil, fmt.Errorf("解析 blob 响应失败: %w", err)
	}
	return base64.StdEncoding.DecodeString(out.Content)
}

type apiResult struct {
	status int
	body   []byte
}

// do 发送请求；对网络错误与 5xx 做少量重试（PUT Contents 对同一路径幂等）。
func (c *Client) do(ctx context.Context, method, u string, body any) (*apiResult, error) {
	if c.token == "" || c.repo == "" {
		return nil, errors.New("github: 未配置 token 或 repo")
	}
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 400 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "GithubImagePush")
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.hc.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("请求 GitHub 失败: %w", err)
			continue
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("读取 GitHub 响应失败: %w", err)
			continue
		}
		if resp.StatusCode >= 500 && attempt < 2 {
			lastErr = &APIError{Status: resp.StatusCode, Message: ghErrorMessage(data, resp.StatusCode)}
			continue
		}
		return &apiResult{status: resp.StatusCode, body: data}, nil
	}
	return nil, lastErr
}

func ghErrorMessage(body []byte, status int) string {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("unexpected response (HTTP %d)", status)
}
