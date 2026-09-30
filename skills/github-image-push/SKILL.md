---
name: github-image-push
description: "上传本地图片到 GitHub 图床并返回直链/Markdown。当用户需要把图片文件变成 URL、在 Markdown/HTML 文档中嵌入图片、发布文章需要插图链接、或提到图床/传图/图片外链时使用。"
---

# GithubImagePush 图床上传

把本地图片文件上传到 GitHub 图床服务（GithubImagePush），返回可直接使用的图片外链。

## 用法

```bash
~/.agents/skills/github-image-push/upload.sh <图片文件> [图片文件2 ...]
```

- 支持一次上传多个文件，每行输出一个链接
- 成功输出 URL 并 exit 0；失败信息在 stderr 并 exit 非 0
- 允许的扩展名：png jpg jpeg gif webp svg bmp ico avif（单文件默认上限 25MB）

## 选项

| 选项 | 说明 |
|---|---|
| `-f, --format FMT` | 输出格式：`url`（默认）/ `markdown` / `html` / `bbcode` / `json` |
| `-u, --url URL` | 服务地址，默认 `$GIP_URL` 或 `http://127.0.0.1:8080` |
| `-k, --key KEY` | 访问 Key，默认 `$GIP_KEY` |
| `--start` | 服务未运行时自动后台启动（需配置 `GIP_BIN`） |
| `-h, --help` | 帮助 |

## 配置

脚本依次读取：命令行参数 → 环境变量 → `~/.config/github-image-push/env`（键值对，可设置 `GIP_URL`、`GIP_KEY`、`GIP_BIN`（服务二进制路径，供 `--start` 使用）、`GIP_CONFIG`（服务配置文件路径））。

## 典型场景

```bash
# 上传一张图，拿到直链
upload.sh ./screenshot.png

# 上传并直接得到 Markdown 嵌入语法
upload.sh -f markdown ./diagram.png    # ![diagram](https://raw.githubusercontent.com/...)

# 服务没启动时自动拉起
upload.sh --start ./a.png ./b.jpg

# 在文档写作流程中：先截图/生成图片 → 调本 skill 上传 → 把返回的 Markdown 插入文档
```

## 故障排查

- `Key 无效/未提供`：检查 `GIP_KEY` 或 `~/.config/github-image-push/env`
- `无法连接服务`：服务未运行；确认后重试加 `--start`，或手动启动 `github-image-push`
- `同名文件已存在`：服务端配置为保留原名时才会出现，改用时间戳命名或看返回中的已有链接
