package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimalYAML = `
auth:
  keys:
    - "test-key"
github:
  token: "tok"
  repo: "someone/images"
`

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(writeTemp(t, "config.yaml", minimalYAML))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Addr != "127.0.0.1:8080" {
		t.Errorf("默认监听地址 = %q", cfg.Server.Addr)
	}
	if cfg.GitHub.Branch != "main" {
		t.Errorf("默认分支 = %q", cfg.GitHub.Branch)
	}
	if cfg.GitHub.URLFormat != FormatRaw {
		t.Errorf("默认 urlFormat = %q", cfg.GitHub.URLFormat)
	}
	if cfg.GitHub.PathTemplate != "{year}/{month}/{day}" {
		t.Errorf("默认 pathTemplate = %q", cfg.GitHub.PathTemplate)
	}
	if !cfg.ExtAllowed("PNG") || !cfg.ExtAllowed("png") {
		t.Errorf("扩展名白名单应大小写不敏感")
	}
	if cfg.ExtAllowed("exe") {
		t.Errorf("exe 不应在白名单内")
	}
	if !cfg.Configured() {
		t.Errorf("token+repo 均配置时 Configured 应为 true")
	}
}

func TestLoadJSONCompatible(t *testing.T) {
	path := writeTemp(t, "config.json", `{"auth":{"keys":["k"]},"github":{"token":"t","repo":"a/b"}}`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Auth.Keys) != 1 || cfg.Auth.Keys[0] != "k" {
		t.Errorf("JSON 配置解析失败: %v", cfg.Auth.Keys)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []struct {
		name, yaml, wantErr string
	}{
		{"空 keys", `github: {token: t, repo: a/b}`, "auth.keys"},
		{"custom 无 customUrl", `
auth: {keys: [k]}
github: {token: t, repo: a/b, urlFormat: custom}`, "customUrl"},
		{"非法 filenameStrategy", `
auth: {keys: [k]}
github: {token: t, repo: a/b, filenameStrategy: random}`, "filenameStrategy"},
		{"非法 onConflict", `
auth: {keys: [k]}
github: {token: t, repo: a/b, onConflict: skip}`, "onConflict"},
		{"repo 缺少斜杠", `
auth: {keys: [k]}
github: {token: t, repo: onlyname}`, "owner/repo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Load(writeTemp(t, "config.yaml", c.yaml))
			if err == nil {
				t.Fatalf("期望报错，实际通过")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("错误信息 %q 未包含 %q", err.Error(), c.wantErr)
			}
		})
	}
}

func TestAllowEmptyGitHubConfigButNeverEmptyKeys(t *testing.T) {
	// token/repo 可以为空（服务可启动，仅功能受限），keys 不行
	cfg, err := Load(writeTemp(t, "config.yaml", "auth:\n  keys: ['k']\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Configured() {
		t.Errorf("未配置 GitHub 时 Configured 应为 false")
	}
}
