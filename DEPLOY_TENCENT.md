# QwenPortal 腾讯云部署方案

## 架构总览

```
┌─────────────────────────────────────────────────────────┐
│                     开发者本地                            │
│  deployTencent.bat ──→ deployTencent.py ──→ SSH 远程部署  │
└──────────────────────────┬──────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                    GitHub (CI/CD)                        │
│  push master ──→ GitHub Actions ──→ build qwenportal_linux
│                                      └─→ Release deploy-latest
└──────────────────────────┬──────────────────────────────┘
                           │
                           ▼
┌─────────────────────────────────────────────────────────┐
│                 Cloudflare Pages (CDN)                   │
│  functions/download.js ←── 代理下载 GitHub Release        │
│  site/ (index.html)     ←── 静态下载页                    │
│  加速国内下载 ≈ 1.14 MB/s                                 │
└──────────────────────────┬──────────────────────────────┘
                           │ curl https://my-proxy-5pb.pages.dev/download
                           ▼
┌─────────────────────────────────────────────────────────┐
│                  腾讯云服务器 (49.235.121.91)              │
│  /home/ubuntu/qwenportal/                                │
│  ├── qwenportal_linux    (Go 二进制)                      │
│  ├── config.yaml         (服务配置)                       │
│  ├── test_agent_tools.py (Agent 工具集)                   │
│  ├── webui/              (Flask 管理后台)                  │
│  └── data/               (数据库 + admin key)             │
└─────────────────────────────────────────────────────────┘
```

## 问题背景

腾讯云服务器访问 GitHub 极慢（~11 B/s），直接 SSH 上传 38MB 二进制频繁 `Connection reset`。  
解决方案：**Cloudflare Pages CDN 中转**——Go 二进制通过 GitHub Actions 编译后上传 Release，Cloudflare Pages Function 代理下载并提供 CDN 加速，服务器从 Cloudflare 下载可达 1.14 MB/s。

## 核心组件

### 1. GitHub Actions — 自动编译

**文件**: `.github/workflows/build.yml`

每次 push 到 master 分支时触发：
- 拉取代码 → 编译 `GOOS=linux GOARCH=amd64` → 更新 Release `deploy-latest`

```yaml
# 关键配置
on:
  push:
    branches: [master]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'

      - run: GOOS=linux GOARCH=amd64 go build -o qwenportal_linux ./cmd/qwenportal/

      - uses: softprops/action-gh-release@v2
        with:
          tag_name: deploy-latest      # 固定 tag，每次覆盖
          files: qwenportal_linux
          make_latest: true
```

**要点**：
- 使用固定 tag `deploy-latest`，每次 CI 运行覆盖该 Release，下载 URL 永远不变
- `make_latest: true` 确保 Release 出现在 Latest 位置

### 2. Cloudflare Pages Function — 下载代理

**文件**: `functions/download.js`

部署在 Cloudflare Pages 项目 `my-proxy`，提供一个 `/download` 端点代理 GitHub Release 二进制，利用 Cloudflare 全球 CDN 加速国内下载。

```js
export async function onRequest(context) {
  const LATEST_URL = 'https://github.com/wesele/MyProxy/releases/latest/download/qwenportal_linux';

  const resp = await fetch(LATEST_URL);

  const headers = new Headers(resp.headers);
  headers.set('Content-Disposition', 'attachment; filename="qwenportal_linux"');
  headers.set('Cache-Control', 'public, max-age=300');

  return new Response(resp.body, {
    status: resp.status,
    statusText: resp.statusText,
    headers,
  });
}
```

**要点**：
- `Cache-Control: max-age=300` 允许 CDN 缓存 5 分钟，重复下载命中缓存
- 部署方式：`npx wrangler pages deploy . --project-name=my-proxy`

### 3. Cloudflare Pages 静态站

**文件**: `site/index.html`, `site/style.css`, `site/script.js`

托管在 `https://my-proxy-5pb.pages.dev`，提供下载入口和产品介绍。

### 4. 部署脚本

**入口**: `deployTencent.bat` (Windows 批处理)

```
@echo off
chcp 65001 >nul
python scripts/deployTencent.py %*
if errorlevel 1 (
    echo.
    echo Deploy failed, check logs above
    pause
)
```

**核心**: `scripts/deployTencent.py`

一键完成以下 6 步：

| 步骤 | 操作 | 说明 |
|------|------|------|
| 0 | 交叉编译 | `GOOS=linux go build` 编译 Linux 二进制 |
| 1 | 上传配置 | SFTP 上传 `config.yaml`、`test_agent_tools.py`、`webui/` |
| 2 | 下载二进制 | SSH 远程执行 curl 从 Cloudflare Pages 下载 |
| 3 | 停止旧服务 | 多方式 kill 旧进程 + 等待端口释放 |
| 4 | 启动新服务 | nohup 后台启动，等待 PID 出现 |
| 5 | 等待就绪 | 循环 curl `/v1/models` 直到返回模型列表 |
| 6 | 配置 Provider | (可选) 通过 API 创建/更新 Provider |

脚本支持三个参数：

| 参数 | 作用 |
|------|------|
| `--skip-build` | 跳过本地编译，使用已有二进制 |
| `--skip-provider` | 跳过 Provider 配置 |
| `--skip-test` | 跳过 API 验证 |

## 部署流程

### 首次部署

```bash
# 1. 创建 Cloudflare Pages 项目 (仅首次)
npx wrangler pages deploy . --project-name=my-proxy

# 2. 确认 GitHub 仓库已配置
git remote -v
# → origin  https://github.com/wesele/MyProxy.git

# 3. 推送到 master 触发 CI/CD 编译 (等待 ~1 分钟)
git push origin master

# 4. 一键部署到腾讯云
deployTencent.bat

# 或分步执行（先跳过 Provider 配置验证核心流程）
deployTencent.bat --skip-provider --skip-test
```

### 日常更新

```bash
# 修改代码后
git push origin master

# 等待 GitHub Actions 完成
# → 可用 https://github.com/wesele/MyProxy/actions 查看状态

# 部署到腾讯云
deployTencent.bat --skip-build --skip-provider
```

## 关键配置

### 服务器信息

| 配置项 | 值 |
|--------|------|
| 主机 | `49.235.121.91` |
| 用户 | `ubuntu` |
| 密钥 | `c:\path\to\ssh.pem` |
| 远程目录 | `/home/ubuntu/qwenportal` |

### Cloudflare Pages

| 配置项 | 值 |
|--------|------|
| 项目名 | `my-proxy` |
| 自动后缀 | `-5pb` |
| 访问地址 | `https://my-proxy-5pb.pages.dev` |
| 下载地址 | `https://my-proxy-5pb.pages.dev/download` |
| API Token | `cfat_...` (设置为 GitHub Actions secret `CLOUDFLARE_API_TOKEN`) |

### Provider 配置

| 配置项 | 值 |
|--------|------|
| 名称 | `QwenTest` |
| 类型 | `openai` |
| 地址 | `https://qwen.aikit.club/v1` |
| 模型 | `qwen3.6-plus` |

## 涉及文件清单

| 文件 | 作用 |
|------|------|
| `deployTencent.bat` | Windows 部署入口 |
| `scripts/deployTencent.py` | 部署主脚本 |
| `.github/workflows/build.yml` | GitHub Actions CI/CD |
| `functions/download.js` | Cloudflare Pages Function 下载代理 |
| `site/index.html` | Cloudflare Pages 静态站首页 |
| `site/style.css` | 静态站样式 |
| `site/script.js` | 静态站版本信息获取 |
| `config.yaml` | 服务端配置文件 |

## 常见问题

### Q: 如何确认 GitHub Actions 编译成功？
打开 https://github.com/wesele/MyProxy/actions 查看最新 workflow 状态。

### Q: 如何确认 Cloudflare Pages 部署成功？
访问 https://my-proxy-5pb.pages.dev 确认静态页正常，访问 https://my-proxy-5pb.pages.dev/download 确认二进制可下载。

### Q: 二进制下载慢怎么办？
Cloudflare CDN 在境内通常可达 1 MB/s 以上。如果首次下载慢，CDN 缓存后（5 分钟内）会更快。也可以手动在服务器上用 `wget` 或 `curl` 重试。

### Q: 部署脚本报 SSH 连接失败？
确认：
1. 腾讯云安全组已开放 22 端口
2. 密钥文件存在且权限正确
3. 服务器 `sshd` 服务运行中

### Q: 服务启动后 curl 返回 401？
TLS 模式下本地请求也会要求认证。脚本使用自动读取的 `admin_key` 注入 `Authorization` 头。如果手动调试，通过 `ss -tlnp` 确认 8080 端口监听正常。
