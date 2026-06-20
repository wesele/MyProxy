# QwenPortal Development Rules

## 开发流程

接到任务后按以下顺序执行：

### 1. 理解项目
- 必读 `AGENTS.md`（本文档）和 `CONTENT_STUDIO.md`
- 涉及具体模块时，按需 `Read` 模块源码 / 测试，理清现有结构与约定
- 不要假设未读过的文件存在或不存在

### 2. 理解需求
- 用户输入清晰且无歧义 → 直接进入步骤 3
- 需求模糊、多种实现路径、影响范围不明确 → **先和用户确认修改计划**（影响点、方案、验证方式），再动手
- 涉及破坏性变更（删字段、改 API、改 schema）→ 必须确认

### 3. 拆分与执行
- 单文件小改 → 直接改
- 任务**适合分拆**（多模块、多文件、调研 + 实现并存）→ 派 subagent 并行/串行处理
- 任务**完全独立**（如纯调研、生成测试数据）→ 单独 subagent
- 派 subagent 时：在 prompt 中明确"研究还是改代码"、验证方式、要返回什么

### 4. 测试
- **所有修改完成后必须测试**：

### 5. 编译与重启
- 修改涉及 Go 后端代码 → 自动执行 `go build -o qwenportal.exe ./cmd/qwenportal` 编译。
- 如果需要编译且编译前该程序正在运行就先退出程序，编译完成后重启。
- 重启方式（避免阻塞当前 shell）：
  ```powershell
  # 1. 杀旧进程
  Get-Process -Name "qwenportal" -ErrorAction SilentlyContinue | Stop-Process -Force
  Get-Process -Name "python*" -ErrorAction SilentlyContinue | Where-Object { $_.CommandLine -match "app.py" } | Stop-Process -Force
  Start-Sleep -Seconds 1

  # 2. 清理旧日志文件（避免文件锁导致重定向失败）
  Remove-Item "data\server.log" -ErrorAction SilentlyContinue
  Remove-Item "data\server.err" -ErrorAction SilentlyContinue

  # 3. 后台启动（使用 cmd /c start /b 不会阻塞 Bash 工具）
  cmd /c start /b .\qwenportal.exe > data\server.log 2> data\server.err
  ```
- 如编译失败 → 修复后重试，不得跳过

### 6. 提交
- 任务收尾时自动 commit 到**本地 git**（用户不要求也要做）；push / 发版仍需用户明确要求
- 提交前确认 `git status` / `git diff` 干净、无敏感信息
- 提交信息用中文，简要说明本次改动范围（"feat: ..." / "fix: ..." 前缀可选）

## Testing Requirements
- All modifications MUST include corresponding automated tests.
- After any Go code changes, run `go build ./...` to verify compilation.
- Run `go test ./internal/...` to verify all Go unit tests pass before committing.
- After any backend changes, restart both the Go server and Flask server to ensure changes take effect.
- Verify database changes by checking if new tables/columns are properly created and data is correctly stored/retrieved.

## Running Tests

### Go Unit Tests
```bash
go test ./internal/... -v -count=1
```

### Flask Web UI Tests
```bash
pytest webui/test_app.py -v
```

### Full Test Suite
```bash
go test ./internal/... -count=1 && pytest webui/test_app.py -v
```

## Test Coverage by Package

| Package | Test File | Covers |
|---------|-----------|--------|
| `internal/db` | `db_test.go` | Provider CRUD, API Key CRUD, Request Logs, Training, edge cases |
| `internal/proxy` | `proxy_test.go` | Router matching, helper functions (truncate, token parsing, SSE writer) |
| `internal/api` | `handler_test.go` | OpenAI/Claude/Admin handlers, Gemini protocol translation |
| `internal/middleware` | `middleware_test.go` | Auth, CORS, Request logging |
| `internal/config` | `config_test.go` | YAML loading, defaults |
| `internal/models` | `models_test.go` | JSON marshaling, model ID generation |
| `webui` | `test_app.py` | Flask view functions, api_call helper |

## Architecture for Testability
- All database access goes through the `db.Store` interface, allowing mock implementations in tests.
- Handlers and middleware receive dependencies (Store, Router, Forwarder, Logger) via constructor injection.
- Pure functions (protocol translation, token parsing, content extraction) are testable without external dependencies.
- The `proxy.Router` accepts a `db.Store` for loading providers, enabling mock-based testing of routing logic.

## Code Change Checklist
1. Make the code change
2. Write/update tests for the changed code
3. Run `go build ./...` to verify compilation
4. Run `go test ./internal/... -count=1` to verify Go tests
5. If Flask UI changed, run `pytest webui/test_app.py -v`
6. Commit with a descriptive message
