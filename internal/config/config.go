// Package config 负责加载与校验 GithubImagePush 的配置文件。
// 配置文件为 YAML（兼容 JSON），结构见 config.example.yaml。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	FormatRaw         = "raw"       // https://raw.githubusercontent.com/{repo}/{branch}/{path}
	FormatJsdelivr    = "jsdelivr"  // https://cdn.jsdelivr.net/gh/{repo}@{branch}/{path}
	FormatGithub      = "github"    // https://github.com/{repo}/raw/{branch}/{path}
	FormatCustom      = "custom"    // customUrl 拼接
	NameTimestamp     = "timestamp" // 时间戳-随机串 命名（默认，避免同名冲突）
	NameOriginal      = "original"  // 保留原始文件名
	ConflictError     = "error"     // 同名已存在时报错
	ConflictOverwrite = "overwrite" // 同名已存在时覆盖（多一次 GET 拿 sha）
)

type Config struct {
	Server Server `yaml:"server"`
	Auth   Auth   `yaml:"auth"`
	GitHub GitHub `yaml:"github"`
}

type Server struct {
	Addr          string `yaml:"addr"`
	MaxUploadSize int64  `yaml:"maxUploadSize"` // 单文件大小上限（字节）
}

type Auth struct {
	Keys []string `yaml:"keys"`
}

type GitHub struct {
	Token            string   `yaml:"token"`
	Repo             string   `yaml:"repo"` // owner/repo
	Branch           string   `yaml:"branch"`
	Path             string   `yaml:"path"`         // 仓库内根目录前缀，如 "img"
	PathTemplate     string   `yaml:"pathTemplate"` // 根目录下的子目录模板，支持 {year} {month} {day} {timestamp} {rand}
	FilenameStrategy string   `yaml:"filenameStrategy"`
	OnConflict       string   `yaml:"onConflict"`
	CommitMessage    string   `yaml:"commitMessage"`
	URLFormat        string   `yaml:"urlFormat"` // raw | jsdelivr | github | custom
	CustomURL        string   `yaml:"customUrl"`
	APIURL           string   `yaml:"apiURL"` // GitHub API 地址，可换成代理或 GitHub Enterprise
	AllowExtensions  []string `yaml:"allowExtensions"`

	// 运行时派生（非配置项）
	extSet map[string]struct{}
}

// Default 返回一份带默认值（且已完成派生字段构建）的配置。
func Default() *Config {
	c := &Config{
		Server: Server{
			Addr:          "127.0.0.1:8080",
			MaxUploadSize: 25 << 20,
		},
		Auth: Auth{Keys: []string{}},
		GitHub: GitHub{
			Branch:           "main",
			PathTemplate:     "{year}/{month}/{day}",
			FilenameStrategy: NameTimestamp,
			OnConflict:       ConflictError,
			CommitMessage:    "Upload by GithubImagePush",
			URLFormat:        FormatRaw,
			APIURL:           "https://api.github.com",
			AllowExtensions: []string{
				"png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif",
			},
		},
	}
	c.normalize()
	return c
}

// Load 读取配置文件（YAML 或 JSON），叠加默认值并校验。
func Load(path string) (*Config, error) {
	cfg := Default()
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}
	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}
	cfg.normalize()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) normalize() {
	s := &c.Server
	s.Addr = strings.TrimSpace(s.Addr)
	if s.Addr == "" {
		s.Addr = "127.0.0.1:8080"
	}
	if s.MaxUploadSize <= 0 {
		s.MaxUploadSize = 25 << 20
	}

	c.Auth.Keys = cleanList(c.Auth.Keys)

	g := &c.GitHub
	g.Repo = strings.TrimSpace(g.Repo)
	g.Branch = strings.TrimSpace(g.Branch)
	if g.Branch == "" {
		g.Branch = "main"
	}
	g.Path = strings.Trim(filepath.ToSlash(strings.TrimSpace(g.Path)), "/")
	g.PathTemplate = strings.Trim(filepath.ToSlash(strings.TrimSpace(g.PathTemplate)), "/")
	g.FilenameStrategy = strings.ToLower(strings.TrimSpace(g.FilenameStrategy))
	if g.FilenameStrategy == "" {
		g.FilenameStrategy = NameTimestamp
	}
	g.OnConflict = strings.ToLower(strings.TrimSpace(g.OnConflict))
	if g.OnConflict == "" {
		g.OnConflict = ConflictError
	}
	g.CommitMessage = strings.TrimSpace(g.CommitMessage)
	if g.CommitMessage == "" {
		g.CommitMessage = "Upload by GithubImagePush"
	}
	g.URLFormat = strings.ToLower(strings.TrimSpace(g.URLFormat))
	if g.URLFormat == "" {
		g.URLFormat = FormatRaw
	}
	g.CustomURL = strings.TrimRight(strings.TrimSpace(g.CustomURL), "/")
	g.APIURL = strings.TrimSpace(g.APIURL)
	if g.APIURL == "" {
		g.APIURL = "https://api.github.com"
	}
	g.APIURL = strings.TrimRight(g.APIURL, "/")

	g.AllowExtensions = cleanList(g.AllowExtensions)
	if len(g.AllowExtensions) == 0 {
		g.AllowExtensions = []string{"png", "jpg", "jpeg", "gif", "webp", "svg", "bmp", "ico", "avif"}
	}
	g.extSet = map[string]struct{}{}
	for _, e := range g.AllowExtensions {
		g.extSet[e] = struct{}{}
	}
}

func (c *Config) validate() error {
	if len(c.Auth.Keys) == 0 {
		return errors.New("auth.keys 不能为空：请至少配置一个访问 Key，否则服务将拒绝所有请求")
	}
	g := &c.GitHub
	switch g.FilenameStrategy {
	case NameTimestamp, NameOriginal:
	default:
		return fmt.Errorf("github.filenameStrategy 取值无效: %q（可选 timestamp / original）", g.FilenameStrategy)
	}
	switch g.OnConflict {
	case ConflictError, ConflictOverwrite:
	default:
		return fmt.Errorf("github.onConflict 取值无效: %q（可选 error / overwrite）", g.OnConflict)
	}
	switch g.URLFormat {
	case FormatRaw, FormatJsdelivr, FormatGithub, FormatCustom:
	default:
		return fmt.Errorf("github.urlFormat 取值无效: %q（可选 raw / jsdelivr / github / custom）", g.URLFormat)
	}
	if g.URLFormat == FormatCustom && g.CustomURL == "" {
		return errors.New("github.urlFormat 为 custom 时必须配置 github.customUrl")
	}
	if g.Repo != "" && !strings.Contains(g.Repo, "/") {
		return fmt.Errorf("github.repo 格式应为 owner/repo，当前为 %q", g.Repo)
	}
	if g.Token == "" || g.Repo == "" {
		// 允许启动（页面与鉴权可用），但上传/图库相关接口会返回明确的未配置错误。
		return nil
	}
	return nil
}

// Configured 报告 GitHub 图床是否已配置完整。
func (c *Config) Configured() bool {
	return c.GitHub.Token != "" && c.GitHub.Repo != ""
}

// ExtAllowed 判断扩展名（不含点、小写）是否允许上传。
func (c *Config) ExtAllowed(ext string) bool {
	_, ok := c.GitHub.extSet[strings.ToLower(ext)]
	return ok
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
