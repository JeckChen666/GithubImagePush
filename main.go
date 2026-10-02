// GithubImagePush：把 GitHub 仓库当作免费图床的 Web 服务（API + 网页，单一二进制）。
// 上传方式参考 PicGo 的 GitHub 图床实现（GitHub Contents API）。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"githubimagepush/internal/config"
	"githubimagepush/internal/server"
)

var version = "1.0.1"

func main() {
	configPath := flag.String("config", "config.yaml", "配置文件路径（YAML，兼容 JSON）")
	addrOverride := flag.String("addr", "", "覆盖配置文件中的监听地址，如 0.0.0.0:8080")
	showVersion := flag.Bool("version", false, "输出版本号")
	flag.Parse()

	if *showVersion {
		fmt.Println("GithubImagePush", version)
		return
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	path := *configPath
	if path == "config.yaml" {
		// 默认路径不存在时尝试 config.json，方便偏好 JSON 的用户
		if _, err := os.Stat(path); err != nil && !os.IsNotExist(err) {
			fatal(logger, err)
		} else if _, err := os.Stat(path); os.IsNotExist(err) {
			if _, err2 := os.Stat("config.json"); err2 == nil {
				path = "config.json"
			}
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		fatal(logger, fmt.Errorf("%s: %w", filepath.Base(path), err))
	}
	if *addrOverride != "" {
		cfg.Server.Addr = *addrOverride
	}

	if !cfg.Configured() {
		logger.Warn("GitHub token/repo 未配置：网页与鉴权可用，但上传/图库接口将返回 503，请补全配置后重启")
	}
	logger.Info("GithubImagePush 启动中",
		"version", version,
		"addr", cfg.Server.Addr,
		"repo", cfg.GitHub.Repo,
		"branch", cfg.GitHub.Branch,
		"urlFormat", cfg.GitHub.URLFormat,
		"keys", len(cfg.Auth.Keys),
	)

	srv := server.New(cfg, logger)
	httpSrv := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       10 * time.Minute, // 大文件慢速上传
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		fatal(logger, err)
	case <-ctx.Done():
	}
	logger.Info("收到退出信号，正在关闭…")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("优雅关闭失败", "err", err)
	}
	logger.Info("已退出")
}

func fatal(logger *slog.Logger, err error) {
	logger.Error("启动失败", "err", err)
	os.Exit(1)
}
