package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"githubimagepush/internal/config"
	"githubimagepush/internal/testutil"
)

const testKey = "test-secret-key"

func newTestServer(t *testing.T, mutate func(*config.Config)) (*httptest.Server, *testutil.MockGitHub) {
	t.Helper()
	mock := testutil.NewMockGitHub("goodtoken")
	t.Cleanup(mock.Close)

	cfg := config.Default()
	cfg.Auth.Keys = []string{testKey}
	cfg.GitHub.Token = "goodtoken"
	cfg.GitHub.Repo = "someone/images"
	cfg.GitHub.Branch = "main"
	cfg.GitHub.Path = "img"
	cfg.GitHub.PathTemplate = "{year}/{month}/{day}"
	cfg.GitHub.URLFormat = config.FormatRaw
	cfg.GitHub.APIURL = mock.URL()
	if mutate != nil {
		mutate(cfg)
	}
	srv := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, mock
}

// doReq 返回响应、解析后的 JSON 与原始 body（resp.Body 可重复读取）。
func doReq(t *testing.T, method, url string, body io.Reader, hdr map[string]string) (*http.Response, map[string]any, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(raw))
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp, out, raw
}

func withKey(h map[string]string) map[string]string {
	if h == nil {
		h = map[string]string{}
	}
	h["X-API-Key"] = testKey
	return h
}

func TestHealthzAndWebPage(t *testing.T) {
	ts, _ := newTestServer(t, nil)

	resp, body, _ := doReq(t, "GET", ts.URL+"/healthz", nil, nil)
	if resp.StatusCode != 200 || body["success"] != true {
		t.Errorf("healthz: %d %v", resp.StatusCode, body)
	}

	resp, _, page := doReq(t, "GET", ts.URL+"/", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(string(page), "GithubImagePush") {
		t.Errorf("首页异常: %d", resp.StatusCode)
	}
	for _, asset := range []string{"/style.css", "/app.js", "/favicon.svg"} {
		resp, _, _ = doReq(t, "GET", ts.URL+asset, nil, nil)
		if resp.StatusCode != 200 {
			t.Errorf("静态资源 %s: %d", asset, resp.StatusCode)
		}
	}
}

func TestAuthVariants(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	url := ts.URL + "/api/verify"

	// 无 key / 错误 key
	for _, hdr := range []map[string]string{nil, {"X-API-Key": "wrong"}, {"Authorization": "Bearer wrong"}} {
		resp, body, _ := doReq(t, "GET", url, nil, hdr)
		if resp.StatusCode != 401 || body["success"] != false {
			t.Errorf("无/错 key 应 401: %d %v", resp.StatusCode, body)
		}
	}

	// 三种合法携带方式
	checks := []struct {
		name string
		hdr  map[string]string
		url  string
	}{
		{"X-API-Key", withKey(nil), url},
		{"Bearer", map[string]string{"Authorization": "Bearer " + testKey}, url},
		{"token 前缀", map[string]string{"Authorization": "token " + testKey}, url},
		{"query", nil, url + "?key=" + testKey},
	}
	for _, c := range checks {
		resp, body, _ := doReq(t, "GET", c.url, nil, c.hdr)
		if resp.StatusCode != 200 || body["success"] != true {
			t.Errorf("%s 方式鉴权失败: %d %v", c.name, resp.StatusCode, body)
		}
	}
}

func TestVerifyContent(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	resp, body, _ := doReq(t, "GET", ts.URL+"/api/verify", nil, withKey(nil))
	if resp.StatusCode != 200 {
		t.Fatalf("verify: %d", resp.StatusCode)
	}
	data := body["data"].(map[string]any)
	if data["repo"] != "someone/images" || data["branch"] != "main" {
		t.Errorf("verify 返回: %v", data)
	}
	if data["configured"] != true {
		t.Errorf("configured 应为 true")
	}
}

func multipartBody(t *testing.T, files map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for field, name := range files {
		fw, _ := mw.CreateFormFile(field, name)
		_, _ = fw.Write([]byte("PNGDATA-" + name))
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}

func uploadOK(t *testing.T, ts *httptest.Server, files map[string]string) []map[string]any {
	t.Helper()
	buf, ct := multipartBody(t, files)
	resp, body, _ := doReq(t, "POST", ts.URL+"/api/upload", buf, withKey(map[string]string{"Content-Type": ct}))
	if resp.StatusCode != 200 || body["success"] != true {
		t.Fatalf("上传失败: %d %v", resp.StatusCode, body)
	}
	items := body["data"].([]any)
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		out = append(out, it.(map[string]any))
	}
	return out
}

func TestUploadMultipart(t *testing.T) {
	ts, mock := newTestServer(t, nil)
	items := uploadOK(t, ts, map[string]string{"file": "photo.png", "extra": "second.jpg"})
	if len(items) != 2 {
		t.Fatalf("应上传 2 个文件，实际 %d", len(items))
	}
	// multipart 字段来自 map 遍历，顺序不确定：按后缀定位 png 项
	var png map[string]any
	for _, it := range items {
		if strings.HasSuffix(it["url"].(string), ".png") {
			png = it
		}
	}
	if png == nil {
		t.Fatalf("未找到 png 上传项: %v", items)
	}
	if png["success"] != true {
		t.Fatalf("png 文件失败: %v", png)
	}
	url := png["url"].(string)
	if !strings.HasPrefix(url, "https://raw.githubusercontent.com/someone/images/main/img/") {
		t.Errorf("url = %q", url)
	}
	if !strings.Contains(png["markdown"].(string), url) {
		t.Errorf("markdown 应包含 url")
	}
	urls := png["urls"].(map[string]any)
	if urls["jsdelivr"] == "" || urls["github"] == "" {
		t.Errorf("urls 应包含多格式: %v", urls)
	}
	if mock.PutCalls != 2 {
		t.Errorf("GitHub PUT 次数 = %d, want 2", mock.PutCalls)
	}
}

func TestUploadJSON(t *testing.T) {
	ts, _ := newTestServer(t, nil)

	b64 := base64.StdEncoding.EncodeToString([]byte("json-image"))
	payload := fmt.Sprintf(`{"filename":"a.png","data":%q}`, b64)
	resp, body, _ := doReq(t, "POST", ts.URL+"/api/upload", strings.NewReader(payload),
		withKey(map[string]string{"Content-Type": "application/json"}))
	if resp.StatusCode != 200 || body["success"] != true {
		t.Fatalf("JSON 上传失败: %d %v", resp.StatusCode, body)
	}

	payload = fmt.Sprintf(`{"filename":"b.png","data":"data:image/png;base64,%s"}`, b64)
	resp, body, _ = doReq(t, "POST", ts.URL+"/api/upload", strings.NewReader(payload),
		withKey(map[string]string{"Content-Type": "application/json"}))
	if resp.StatusCode != 200 || body["success"] != true {
		t.Fatalf("data URI 上传失败: %d %v", resp.StatusCode, body)
	}
}

func TestUploadValidation(t *testing.T) {
	ts, _ := newTestServer(t, nil)

	// 扩展名不允许
	buf, ct := multipartBody(t, map[string]string{"file": "evil.exe"})
	resp, body, _ := doReq(t, "POST", ts.URL+"/api/upload", buf, withKey(map[string]string{"Content-Type": ct}))
	if resp.StatusCode != 200 {
		t.Fatalf("HTTP 状态: %d", resp.StatusCode)
	}
	item := body["data"].([]any)[0].(map[string]any)
	if item["success"] == true || !strings.Contains(item["message"].(string), "不允许") {
		t.Errorf("exe 应被拒绝: %v", item)
	}

	// 空 body
	resp, body, _ = doReq(t, "POST", ts.URL+"/api/upload", strings.NewReader(""),
		withKey(map[string]string{"Content-Type": "application/json"}))
	if resp.StatusCode != 400 {
		t.Errorf("空 JSON body 应 400: %d %v", resp.StatusCode, body)
	}

	// 无鉴权上传
	buf, ct = multipartBody(t, map[string]string{"file": "a.png"})
	resp, _, _ = doReq(t, "POST", ts.URL+"/api/upload", buf, map[string]string{"Content-Type": ct})
	if resp.StatusCode != 401 {
		t.Errorf("未鉴权上传应 401: %d", resp.StatusCode)
	}
}

func TestUploadOriginalNameConflict(t *testing.T) {
	ts, _ := newTestServer(t, func(c *config.Config) {
		c.GitHub.FilenameStrategy = config.NameOriginal
		c.GitHub.PathTemplate = ""
	})
	buf, ct := multipartBody(t, map[string]string{"file": "same.png"})
	resp, body, _ := doReq(t, "POST", ts.URL+"/api/upload", buf, withKey(map[string]string{"Content-Type": ct}))
	if resp.StatusCode != 200 || body["success"] != true {
		t.Fatalf("首次上传应成功: %d %v", resp.StatusCode, body)
	}

	buf, ct = multipartBody(t, map[string]string{"file": "same.png"})
	resp, body, _ = doReq(t, "POST", ts.URL+"/api/upload", buf, withKey(map[string]string{"Content-Type": ct}))
	if resp.StatusCode != 200 {
		t.Fatalf("第二次上传 HTTP 状态: %d", resp.StatusCode)
	}
	item := body["data"].([]any)[0].(map[string]any)
	if item["success"] == true || !strings.Contains(item["message"].(string), "已存在") {
		t.Fatalf("同名二次上传应报已存在: %v", item)
	}
}

func TestUploadOverwriteStrategy(t *testing.T) {
	ts, _ := newTestServer(t, func(c *config.Config) {
		c.GitHub.FilenameStrategy = config.NameOriginal
		c.GitHub.OnConflict = config.ConflictOverwrite
		c.GitHub.PathTemplate = ""
	})
	for i := 0; i < 2; i++ {
		items := uploadOK(t, ts, map[string]string{"file": "same.png"})
		if items[0]["success"] != true {
			t.Fatalf("第 %d 次上传(overwrite)应成功: %v", i+1, items[0])
		}
	}
}

func TestPicGoCompatAlias(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	buf, ct := multipartBody(t, map[string]string{"image": "picgo.png"})
	resp, body, _ := doReq(t, "POST", ts.URL+"/upload", buf, withKey(map[string]string{"Content-Type": ct}))
	if resp.StatusCode != 200 || body["success"] != true {
		t.Fatalf("/upload 兼容接口失败: %d %v", resp.StatusCode, body)
	}
	result := body["result"].([]any)
	if len(result) != 1 || !strings.HasSuffix(result[0].(string), ".png") {
		t.Errorf("result = %v", result)
	}
}

func TestListAndDeleteAndRaw(t *testing.T) {
	ts, _ := newTestServer(t, func(c *config.Config) {
		c.GitHub.FilenameStrategy = config.NameOriginal
		c.GitHub.PathTemplate = "sub"
	})
	uploadOK(t, ts, map[string]string{"file": "listme.png"})

	// 列表
	resp, body, _ := doReq(t, "GET", ts.URL+"/api/list?dir=img/sub", nil, withKey(nil))
	if resp.StatusCode != 200 {
		t.Fatalf("list: %d", resp.StatusCode)
	}
	data := body["data"].(map[string]any)
	entries := data["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries = %v", entries)
	}
	entry := entries[0].(map[string]any)
	if entry["name"] != "listme.png" || entry["url"] == "" {
		t.Errorf("entry = %v", entry)
	}

	// raw 代理
	resp, _, raw := doReq(t, "GET", ts.URL+"/api/raw?path=img/sub/listme.png&key="+testKey, nil, nil)
	if resp.StatusCode != 200 || string(raw) != "PNGDATA-listme.png" {
		t.Errorf("raw: %d %q", resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/") {
		t.Errorf("raw Content-Type = %q", ct)
	}

	// 删除
	resp, body, _ = doReq(t, "DELETE", ts.URL+"/api/delete?path=img/sub/listme.png", nil, withKey(nil))
	if resp.StatusCode != 200 || body["success"] != true {
		t.Fatalf("delete: %d %v", resp.StatusCode, body)
	}
	// git 不存在"空目录"：清空后该目录在 GitHub 上即不存在，list 返回 404
	resp, body, _ = doReq(t, "GET", ts.URL+"/api/list?dir=img/sub", nil, withKey(nil))
	if resp.StatusCode != 404 || body["success"] != false {
		t.Errorf("清空后目录应不存在(404): %d %v", resp.StatusCode, body)
	}

	// 删除不存在的文件
	resp, _, _ = doReq(t, "DELETE", ts.URL+"/api/delete?path=img/sub/listme.png", nil, withKey(nil))
	if resp.StatusCode != 404 {
		t.Errorf("删除不存在文件应 404: %d", resp.StatusCode)
	}
}

func TestListPathTraversalSanitized(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	resp, body, _ := doReq(t, "GET", ts.URL+"/api/list?dir=../../etc", nil, withKey(nil))
	// 经过清洗后 dir=etc，仓库中不存在该目录 → 404，而不是越权访问其他路径
	if resp.StatusCode != 404 || !strings.Contains(body["message"].(string), "不存在") {
		t.Errorf("路径穿越请求处理异常: %d %v", resp.StatusCode, body)
	}
}

func TestUnconfiguredGitHub(t *testing.T) {
	ts, _ := newTestServer(t, func(c *config.Config) {
		c.GitHub.Token = ""
		c.GitHub.Repo = ""
	})
	resp, body, _ := doReq(t, "POST", ts.URL+"/api/upload", strings.NewReader(""),
		withKey(map[string]string{"Content-Type": "application/json"}))
	if resp.StatusCode != 503 || !strings.Contains(body["message"].(string), "未配置") {
		t.Errorf("未配置 GitHub 应 503: %d %v", resp.StatusCode, body)
	}
}

func TestAuthRateLimit(t *testing.T) {
	ts, _ := newTestServer(t, nil)
	var last int
	for i := 0; i < authFailLimit+2; i++ {
		resp, _, _ := doReq(t, "GET", ts.URL+"/api/verify", nil, map[string]string{"X-API-Key": "bad"})
		last = resp.StatusCode
		if i < authFailLimit && last != 401 {
			t.Fatalf("第 %d 次应为 401，实际 %d", i+1, last)
		}
	}
	if last != 429 {
		t.Errorf("超过失败阈值后应为 429，实际 %d", last)
	}
	// 封禁期内，来自同一 IP 的正确 key 也被拒绝
	resp, _, _ := doReq(t, "GET", ts.URL+"/api/verify", nil, withKey(nil))
	if resp.StatusCode != 429 {
		t.Errorf("封禁期内正确 key 也应 429，实际 %d", resp.StatusCode)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KB", 26214400: "25.0 MB"}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}
