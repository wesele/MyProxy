# QwenPortal Agent Rules

## 文档地图

| 目的 | 读什么 | 勿默认通读 |
|------|--------|------------|
| 构建/用法 | [README.md](README.md) §快速开始 | myproxy.md |
| 架构/协议/Admin API 细节 | [myproxy.md](myproxy.md) 对应章节 | 全文 |
| 部署到测试机 | [RELEASE.md](RELEASE.md)、[scripts/deploy.py](scripts/deploy.py) | — |

不要假设文件存在；用 Glob/Read 确认路径。

## 项目速览

单端口 LLM 网关（Go Gin + SQLite + Flask Web UI），默认 `:8080`。

- **入口**：[cmd/qwenportal/main.go](cmd/qwenportal/main.go)
- **请求链**：`middleware` → `internal/api/*Handler` → `proxy.Router` → `proxy.Forwarder` → 上游
- **路由/转发**：[internal/proxy/](internal/proxy/)
- **协议转换**：[internal/api/openai.go](internal/api/openai.go)、[gemini.go](internal/api/gemini.go)、[responses.go](internal/api/responses.go)、[claude.go](internal/api/claude.go)
- **持久化**：[internal/db/store.go](internal/db/store.go) 接口 + [internal/models/](internal/models/)
- **管理 UI**：[webui/app.py](webui/app.py) + [webui/templates/](webui/templates/)

代理接口需 `Authorization: Bearer <api-key>`；管理 API 支持 Session 或 Bearer，局域网 IP 自动放行。

## 开发流程

接到任务后按顺序执行：

### 1. 理解项目
- 读本文档 §文档地图、§项目速览
- 涉及具体模块时，按需 Read 源码 / `*_test.go`

### 2. 理解需求
- 输入清晰无歧义 → 直接进入步骤 3
- 需求模糊、多种实现路径、影响范围不明确 → **先确认修改计划**（影响点、方案、验证方式）
- 破坏性变更（删字段、改 API、改 schema）→ 必须确认

### 3. 拆分与执行
- 单文件小改 → 直接改
- 多模块/多文件、调研与实现并存、测试量大 → 可用 subagent 并行/串行

### 4. 收尾验证
所有修改须附带对应自动测试。完成后运行：

```bash
go build ./... && go test ./internal/... -count=1
```

仅当改动 `webui/` 时追加：

```bash
pytest webui/test_app.py -v
```

- 改 schema → 验证新表/字段创建及读写正确
- 改 Go 后端 → 见步骤 5 编译重启（Go 进程会拉起 Flask，无需单独重启 Flask）

### 5. 编译与重启
- 修改涉及 Go 后端 → `go build -o qwenportal.exe ./cmd/qwenportal/`
- 检查进程：`Get-Process qwenportal -ErrorAction SilentlyContinue`
- 若存在则重启：
  1. `Stop-Process -Name qwenportal -Force`
  2. 等待 1-2 秒
  3. `Start-Process -NoNewWindow -FilePath ".\qwenportal.exe"`
- 若不存在 → 提示用户手动启动

### 6. 提交
- 任务收尾自动 commit 到本地 git（用户未要求也要做）；push/发版需用户明确要求
- 提交前确认 `git status` / `git diff` 干净、无敏感信息
- 勿提交 `data/admin_key.txt`、`data/password.txt`、证书等
- 提交信息用中文，简要说明改动范围（`feat:` / `fix:` 前缀可选）

## 测试约定

- 数据库访问经 `db.Store` 接口，测试可 mock
- Handler/Middleware 通过构造注入依赖（Store、Router、Forwarder、Logger）
- 协议转换、token 解析等纯函数独立单测；测试文件与源码同目录，用 Glob `*_test.go` 定位