# GithubImagePush 图床技能 · AI Agent 安装配置指南

> 本文档由 GithubImagePush 服务自动生成，面向 AI Agent。
> 请严格按以下步骤执行，全部完成后做"第 4 步验证"，并向用户汇报结果与测试上传返回的链接。

## 0. 服务信息

| 项目 | 值 |
|---|---|
| 服务地址 | {{BASE_URL}} |
| 访问 Key | {{KEY}} |
| 图床仓库 | {{REPO}} |
| 上传目录 | {{PATH}} |
| 允许扩展名 | {{EXTS}} |
| 单文件上限 | {{MAX_SIZE}} |
| 开源仓库 | {{REPO_URL}} |

## 1. 写入本机配置

创建配置文件 `~/.config/github-image-push/env`（目录不存在请先创建）：

```bash
mkdir -p ~/.config/github-image-push
cat > ~/.config/github-image-push/env <<'EOF'
GIP_URL={{BASE_URL}}
GIP_KEY={{KEY}}
EOF
```

说明：

- `GIP_KEY` 即本页面展示的 Key，是该服务的访问凭证，仅写入用户本地配置，不要提交到任何仓库或告知第三方
- 可选追加 `GIP_BIN`（服务二进制的绝对路径）与 `GIP_CONFIG`（服务配置文件路径），配置后上传脚本可用 `--start` 在服务未运行时自动拉起服务

## 2. 安装 skill 文件

创建技能目录：

```bash
mkdir -p ~/.agents/skills/github-image-push
```

### 2.1 写入 `~/.agents/skills/github-image-push/SKILL.md`

优先从开源仓库下载（与线上版本一致）：

```bash
curl -fsSL {{REPO_URL}}/raw/main/skills/github-image-push/SKILL.md \
  -o ~/.agents/skills/github-image-push/SKILL.md
```

无法访问仓库时，写入以下内容（与仓库版本一致）：

````markdown
{{SKILL_MD}}
````

### 2.2 写入 `~/.agents/skills/github-image-push/upload.sh` 并加执行权限

优先从开源仓库下载：

```bash
curl -fsSL {{REPO_URL}}/raw/main/skills/github-image-push/upload.sh \
  -o ~/.agents/skills/github-image-push/upload.sh
chmod +x ~/.agents/skills/github-image-push/upload.sh
```

无法访问仓库时，写入以下内容：

```bash
{{UPLOAD_SH}}
```

## 3. 依赖检查

- `curl`：必需（上传走 HTTP 请求）
- `python3`：可选（缺失时脚本会退化为正则提取链接，仅支持 `url` 格式）

## 4. 验证

```bash
# 1) 服务可达性
curl -sf {{BASE_URL}}/healthz

# 2) 用任意一张本机图片测试上传（预期输出一行 Markdown 链接）
~/.agents/skills/github-image-push/upload.sh -f markdown /path/to/any/image.png
```

预期：`healthz` 返回 `{"success":true,...}`；上传输出 `![文件名](https://...)`。
失败时查看 stderr 提示，对照 SKILL.md 的"故障排查"一节处理（多为 Key 无效、服务未运行、文件类型受限三类）。

## 5. 完成标准

- [ ] `~/.config/github-image-push/env` 存在，且包含 `GIP_URL` 与 `GIP_KEY`
- [ ] `~/.agents/skills/github-image-push/SKILL.md` 与 `upload.sh` 存在，且 `upload.sh` 具有可执行权限
- [ ] 测试图片上传成功，返回了可访问的图片链接

完成后请向用户报告：安装的文件路径、验证上传返回的图片链接。
