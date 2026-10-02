package server

import (
	_ "embed"
	"net/http"
	"strings"
)

// repoURL 项目开源仓库地址（页脚展示、技能指南内引用）。
const repoURL = "https://github.com/JeckChen666/GithubImagePush"

//go:embed skilldata/guide.md
var skillGuideTmpl string

//go:embed skilldata/SKILL.md
var skillMD string

//go:embed skilldata/upload.sh
var skillUploadSh string

// handleSkill 返回面向 AI Agent 的技能安装指南。
// 接口受鉴权保护，调用方已持有有效 Key，因此回显其自身 Key 便于直接完成配置；
// 不产生任何越权信息泄露。
func (s *Server) handleSkill(w http.ResponseWriter, r *http.Request) {
	g := s.cfg.GitHub
	guide := strings.NewReplacer(
		"{{BASE_URL}}", baseURL(r),
		"{{KEY}}", extractKey(r),
		"{{REPO}}", g.Repo+" @ "+g.Branch,
		"{{PATH}}", g.Path,
		"{{EXTS}}", strings.Join(g.AllowExtensions, " "),
		"{{MAX_SIZE}}", humanSize(s.cfg.Server.MaxUploadSize),
		"{{REPO_URL}}", repoURL,
		"{{SKILL_MD}}", strings.TrimSpace(skillMD),
		"{{UPLOAD_SH}}", strings.TrimSpace(skillUploadSh),
	).Replace(skillGuideTmpl)
	writeJSON(w, http.StatusOK, envelope{Success: true, Data: map[string]any{
		"baseUrl": baseURL(r),
		"key":     extractKey(r),
		"repoURL": repoURL,
		"guide":   guide,
	}})
}

// baseURL 从请求推导对外服务地址（支持反代的 X-Forwarded-Proto）。
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
