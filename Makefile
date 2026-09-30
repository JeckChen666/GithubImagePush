BINARY := github-image-push
VERSION ?= $(shell grep -m1 'var version' main.go | grep -o '"[^"]*"')

.PHONY: build test vet fmt mock run clean

build: ## 编译主程序
	go build -o $(BINARY) .

mock: ## 启动本地 GitHub API Mock（配合 config.mock.yaml 体验）
	go run ./cmd/mockgh -addr 127.0.0.1:9999 -token goodtoken

run: build ## 以默认配置启动服务
	./$(BINARY)

test: ## 运行全部测试
	go test ./...

vet: ## 静态检查 + 格式检查
	go vet ./... && test -z "$$(gofmt -l .)"

fmt: ## 格式化代码
	gofmt -w .

clean: ## 清理构建产物
	rm -f $(BINARY)

help: ## 显示帮助
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-8s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
