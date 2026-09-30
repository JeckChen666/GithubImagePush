package naming

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestSanitizeBaseName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"..hidden", "hidden"},
		{"../etc/passwd", "passwd"},
		{"a\\b\\c.png", "c.png"},
		{`con:t*r?ol"<x>|y`, "con_t_r_ol__x__y"},
		{"图片 名称", "图片 名称"},
		{"", "image"},
		{".", "image"},
	}
	for _, c := range cases {
		if got := SanitizeBaseName(c.in); got != c.want {
			t.Errorf("SanitizeBaseName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTimestampName(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 23, 45, 0, time.Local)
	name := TimestampName(now, ".png")
	if !regexp.MustCompile(`^20260930152345-[0-9a-f]{8}\.png$`).MatchString(name) {
		t.Errorf("TimestampName 生成结果不符合预期: %s", name)
	}
}

func TestBuildObjectPath(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 23, 45, 0, time.Local)
	got := BuildObjectPath("img", "{year}/{month}/{day}", "a.png", now)
	if got != "img/2026/09/30/a.png" {
		t.Errorf("BuildObjectPath = %q", got)
	}
	if got := BuildObjectPath("", "", "a.png", now); got != "a.png" {
		t.Errorf("BuildObjectPath(空) = %q", got)
	}
}

func TestSanitizeRequestPath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"/img/a.png", "img/a.png"},
		{"../../etc/passwd", "etc/passwd"},
		{"a/..//b/./c.png", "a/b/c.png"},
		{"\\win\\path", "win/path"},
		{"img/ok.png", "img/ok.png"},
	}
	for _, c := range cases {
		if got := SanitizeRequestPath(c.in); got != c.want {
			t.Errorf("SanitizeRequestPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestEscapePathSegments(t *testing.T) {
	cases := []struct{ in, want string }{
		{"img/2026/a b.png", "img/2026/a%20b.png"},
		{"img/a+b.png", "img/a%2Bb.png"},
		{"图/x.png", "%E5%9B%BE/x.png"},
		{"img/#hash.png", "img/%23hash.png"},
		{"img/a?b.png", "img/a%3Fb.png"},
	}
	for _, c := range cases {
		got := EscapePathSegments(c.in)
		if got != c.want {
			t.Errorf("EscapePathSegments(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.Contains(got, " ") {
			t.Errorf("EscapePathSegments(%q) 含未编码空格: %q", c.in, got)
		}
	}
}

func TestBuildURLs(t *testing.T) {
	urls := BuildURLs("user/repo", "main", "img/2026/a b.png", "https://cdn.example.com/base")
	if want := "https://raw.githubusercontent.com/user/repo/main/img/2026/a%20b.png"; urls["raw"] != want {
		t.Errorf("raw = %q, want %q", urls["raw"], want)
	}
	if want := "https://cdn.jsdelivr.net/gh/user/repo@main/img/2026/a%20b.png"; urls["jsdelivr"] != want {
		t.Errorf("jsdelivr = %q, want %q", urls["jsdelivr"], want)
	}
	if want := "https://github.com/user/repo/raw/main/img/2026/a%20b.png"; urls["github"] != want {
		t.Errorf("github = %q, want %q", urls["github"], want)
	}
	if want := "https://cdn.example.com/base/img/2026/a%20b.png"; urls["custom"] != want {
		t.Errorf("custom = %q, want %q", urls["custom"], want)
	}

	// 模板式 customUrl
	urls = BuildURLs("u/r", "main", "x.png", "https://cdn.example.com/{path}")
	if want := "https://cdn.example.com/x.png"; urls["custom"] != want {
		t.Errorf("custom(模板) = %q, want %q", urls["custom"], want)
	}

	// 无 customUrl 时不包含 custom
	urls = BuildURLs("u/r", "main", "x.png", "")
	if _, ok := urls["custom"]; ok {
		t.Errorf("无 customUrl 时不应包含 custom 项")
	}

	if got := BuildURL("jsdelivr", "u/r", "main", "x.png", ""); !strings.Contains(got, "cdn.jsdelivr.net") {
		t.Errorf("BuildURL(jsdelivr) = %q", got)
	}
}
