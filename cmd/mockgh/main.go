// mockgh 启动一个内存态的 GitHub API Mock（基于 testutil），
// 用于本地联调与演示 GithubImagePush，无需真实 token。
//
// 用法：
//
//	go run ./cmd/mockgh -addr 127.0.0.1:9999 -token goodtoken
//
// 然后在 config.yaml 中设置 github.apiURL: http://127.0.0.1:9999 即可完整体验上传/图库。
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"githubimagepush/internal/testutil"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9999", "监听地址")
	token := flag.String("token", "goodtoken", "要求的 Bearer token（空则不校验）")
	flag.Parse()

	mock := testutil.NewMockGitHub(*token)
	samples := sampleFiles()
	for _, s := range samples {
		mock.Seed(s.path, s.content)
	}

	srv := &http.Server{Addr: *addr, Handler: mock.Handler()}
	fmt.Printf("GitHub API Mock 监听于 http://%s\n", *addr)
	fmt.Printf("  token: %q\n  预置文件: %d 个\n", *token, len(samples))
	log.Fatal(srv.ListenAndServe())
}

type sample struct {
	path    string
	content []byte
}

func sampleFiles() []sample {
	mk := func(bg, fg, label string) []byte {
		return []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="480" height="320">
<rect width="480" height="320" rx="24" fill="` + bg + `"/>
<circle cx="90" cy="80" r="42" fill="` + fg + `" opacity=".85"/>
<text x="240" y="180" font-family="sans-serif" font-size="40" fill="` + fg + `" text-anchor="middle">` + label + `</text>
</svg>`)
	}
	return []sample{
		{path: "img/2026/09/28/sunrise.svg", content: mk("#f59e0b", "#fff7ed", "Sunrise")},
		{path: "img/2026/09/28/ocean.svg", content: mk("#0ea5e9", "#e0f2fe", "Ocean")},
		{path: "img/2026/09/29/forest.svg", content: mk("#16a34a", "#f0fdf4", "Forest")},
		{path: "img/2026/09/29/sakura.svg", content: mk("#f472b6", "#fdf2f8", "Sakura")},
		{path: "img/2026/09/30/night.svg", content: mk("#312e81", "#e0e7ff", "Night")},
	}
}
