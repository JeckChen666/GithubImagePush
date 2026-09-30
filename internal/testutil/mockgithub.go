// Package testutil 提供一个内存态的 GitHub Contents/Blobs API Mock，
// 用于单元测试与本地联调（无需真实 token）。
package testutil

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"sort"
	"strings"
	"sync"
)

type MockFile struct {
	Path    string
	Content []byte
	SHA     string
}

type MockGitHub struct {
	mu    sync.Mutex
	files map[string]*MockFile
	srv   *httptest.Server
	// Token 非空时校验 Authorization 头，不匹配返回 401
	Token    string
	PutCalls int
	DelCalls int
}

func NewMockGitHub(token string) *MockGitHub {
	m := &MockGitHub{files: map[string]*MockFile{}, Token: token}
	m.srv = httptest.NewServer(m.Handler())
	return m
}

// Handler 返回 Mock 的路由，可挂到自定义端口（见 cmd/mockgh）。
func (m *MockGitHub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /repos/{owner}/{repo}/contents/{path...}", m.handlePut)
	mux.HandleFunc("GET /repos/{owner}/{repo}/contents/{path...}", m.handleGet)
	mux.HandleFunc("DELETE /repos/{owner}/{repo}/contents/{path...}", m.handleDelete)
	mux.HandleFunc("GET /repos/{owner}/{repo}/git/blobs/{sha}", m.handleBlob)
	mux.HandleFunc("GET /raw/{path...}", m.handleRaw)
	return mux
}

func (m *MockGitHub) URL() string { return m.srv.URL }
func (m *MockGitHub) Close()      { m.srv.Close() }

// Seed 预置一个文件。
func (m *MockGitHub) Seed(objPath string, content []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[objPath] = &MockFile{Path: objPath, Content: content, SHA: sha(content)}
}

func sha(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

func (m *MockGitHub) checkAuth(w http.ResponseWriter, r *http.Request) bool {
	if m.Token == "" {
		return true
	}
	if r.Header.Get("Authorization") != "Bearer "+m.Token {
		writeGH(w, http.StatusUnauthorized, map[string]any{"message": "Bad credentials"})
		return false
	}
	return true
}

func (m *MockGitHub) fileObject(f *MockFile, inlineContent bool) map[string]any {
	obj := map[string]any{
		"name":         path.Base(f.Path),
		"path":         f.Path,
		"sha":          f.SHA,
		"size":         len(f.Content),
		"type":         "file",
		"download_url": m.srv.URL + "/raw/" + f.Path,
		"html_url":     "https://github.com/x/y/blob/main/" + f.Path,
	}
	if inlineContent && len(f.Content) < 1<<20 {
		obj["content"] = base64.StdEncoding.EncodeToString(f.Content)
		// Contents API 返回带换行的 base64，这里简化为无换行
		obj["encoding"] = "base64"
	}
	return obj
}

func (m *MockGitHub) children(dir string) []map[string]any {
	prefix := ""
	if dir != "" {
		prefix = strings.TrimSuffix(dir, "/") + "/"
	}
	seen := map[string]bool{}
	var out []map[string]any
	addFile := func(f *MockFile) {
		out = append(out, m.fileObject(f, false))
	}
	addDir := func(name, p string) {
		if seen[name] {
			return
		}
		seen[name] = true
		out = append(out, map[string]any{
			"name": name, "path": p, "sha": "dir-" + name, "size": 0,
			"type": "dir", "download_url": nil, "html_url": nil,
		})
	}
	for _, f := range m.files {
		if !strings.HasPrefix(f.Path, prefix) {
			continue
		}
		rest := f.Path[len(prefix):]
		if rest == "" {
			continue
		}
		if i := strings.Index(rest, "/"); i >= 0 {
			addDir(rest[:i], prefix+rest[:i])
		} else {
			addFile(f)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i]["path"].(string) < out[j]["path"].(string)
	})
	return out
}

func (m *MockGitHub) handlePut(w http.ResponseWriter, r *http.Request) {
	if !m.checkAuth(w, r) {
		return
	}
	objPath := r.PathValue("path")
	var body struct {
		Message string `json:"message"`
		Content string `json:"content"`
		Branch  string `json:"branch"`
		SHA     string `json:"sha"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeGH(w, http.StatusBadRequest, map[string]any{"message": "bad json"})
		return
	}
	content, err := base64.StdEncoding.DecodeString(body.Content)
	if err != nil {
		writeGH(w, http.StatusUnprocessableEntity, map[string]any{"message": "content is not valid base64"})
		return
	}

	m.mu.Lock()
	m.PutCalls++
	old, exists := m.files[objPath]
	m.mu.Unlock()

	if body.SHA == "" && exists {
		writeGH(w, http.StatusUnprocessableEntity, map[string]any{"message": "sha wasn't supplied"})
		return
	}
	if body.SHA != "" && (!exists || old.SHA != body.SHA) {
		writeGH(w, http.StatusUnprocessableEntity, map[string]any{"message": "provided sha does not match"})
		return
	}

	f := &MockFile{Path: objPath, Content: content, SHA: sha(content)}
	m.mu.Lock()
	m.files[objPath] = f
	m.mu.Unlock()

	status := http.StatusOK
	if !exists {
		status = http.StatusCreated
	}
	writeGH(w, status, map[string]any{"content": m.fileObject(f, false)})
}

func (m *MockGitHub) handleGet(w http.ResponseWriter, r *http.Request) {
	if !m.checkAuth(w, r) {
		return
	}
	objPath := r.PathValue("path")
	m.mu.Lock()
	defer m.mu.Unlock()
	if f, ok := m.files[objPath]; ok {
		writeGH(w, http.StatusOK, m.fileObject(f, true))
		return
	}
	if kids := m.children(objPath); len(kids) > 0 || objPath == "" {
		writeGH(w, http.StatusOK, kids)
		return
	}
	writeGH(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
}

func (m *MockGitHub) handleDelete(w http.ResponseWriter, r *http.Request) {
	if !m.checkAuth(w, r) {
		return
	}
	objPath := r.PathValue("path")
	var body struct {
		Message string `json:"message"`
		SHA     string `json:"sha"`
		Branch  string `json:"branch"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	m.mu.Lock()
	m.DelCalls++
	f, ok := m.files[objPath]
	m.mu.Unlock()
	if !ok {
		writeGH(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
		return
	}
	if body.SHA != f.SHA {
		writeGH(w, http.StatusUnprocessableEntity, map[string]any{"message": "sha does not match"})
		return
	}
	m.mu.Lock()
	delete(m.files, objPath)
	m.mu.Unlock()
	writeGH(w, http.StatusOK, map[string]any{"commit": map[string]any{"message": body.Message}})
}

func (m *MockGitHub) handleBlob(w http.ResponseWriter, r *http.Request) {
	if !m.checkAuth(w, r) {
		return
	}
	shaWant := r.PathValue("sha")
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range m.files {
		if f.SHA == shaWant {
			writeGH(w, http.StatusOK, map[string]any{
				"content": base64.StdEncoding.EncodeToString(f.Content),
			})
			return
		}
	}
	writeGH(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
}

func (m *MockGitHub) handleRaw(w http.ResponseWriter, r *http.Request) {
	objPath := r.PathValue("path")
	m.mu.Lock()
	f, ok := m.files[objPath]
	m.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	_, _ = io.Copy(w, strings.NewReader(string(f.Content)))
}

func writeGH(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
